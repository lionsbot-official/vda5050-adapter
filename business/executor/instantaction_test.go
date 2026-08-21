package executor

import (
	"errors"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func waitInstantStatus(t *testing.T, s *vehiclestate.Store, actionID string, want state.ActionStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, as := range s.Snapshot().InstantActionStates {
			if as.ActionID == actionID && as.ActionStatus == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("instant action %q never reached %q; states = %+v", actionID, want, s.Snapshot().InstantActionStates)
}

func acceptInstant(s *vehiclestate.Store, a order.Action) {
	s.AddInstantActionState(state.ActionState{ActionID: a.ActionID, ActionType: a.ActionType, ActionStatus: state.ActionStatusWaiting})
}

func TestExecutor_StartChargingActionRunsToFinished(t *testing.T) {
	store := vehiclestate.New()
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := order.Action{ActionID: "ia-1", ActionType: "startCharging"}
	acceptInstant(store, a)
	e.InstantAction(a)

	select {
	case <-cmd.chargeStarts:
	case <-time.After(2 * time.Second):
		t.Fatal("no StartCharging command dispatched")
	}
	waitInstantStatus(t, store, "ia-1", state.ActionStatusRunning)

	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitInstantStatus(t, store, "ia-1", state.ActionStatusFinished)
}

func TestExecutor_StopChargingFailureReported(t *testing.T) {
	store := vehiclestate.New()
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := order.Action{ActionID: "ia-2", ActionType: "stopCharging"}
	acceptInstant(store, a)
	e.InstantAction(a)

	select {
	case <-cmd.chargeStops:
	case <-time.After(2 * time.Second):
		t.Fatal("no StopCharging command dispatched")
	}

	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: false, Reason: "stuck"}}
	waitInstantStatus(t, store, "ia-2", state.ActionStatusFailed)
	for _, as := range store.Snapshot().InstantActionStates {
		if as.ActionID == "ia-2" && as.ActionResult != "stuck" {
			t.Errorf("ActionResult = %q, want stuck", as.ActionResult)
		}
	}
}

func TestExecutor_InstantActionRejectedWhileOrderExecuting(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move dispatched")
	}

	a := order.Action{ActionID: "ia-3", ActionType: "startCharging"}
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "ia-3", state.ActionStatusFailed)
	select {
	case <-cmd.chargeStarts:
		t.Fatal("charging command sent while an order was executing")
	default:
	}

	// the running order is undisturbed: completion still commits traversal
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")
}

func TestExecutor_ChargingDispatchErrorFails(t *testing.T) {
	store := vehiclestate.New()
	cmd := newFakeCommander()
	cmd.chargeErr = errors.New("ws down")
	e := New(store, cmd)
	startExecutor(t, e)

	a := order.Action{ActionID: "ia-4", ActionType: "startCharging"}
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "ia-4", state.ActionStatusFailed)
}

var errChargeDown = errors.New("charge command channel down")

func hasStoreErrorRef(s *vehiclestate.Store, et state.ErrorType, key, value string) bool {
	for _, e := range s.Snapshot().Errors {
		if e.ErrorType != et {
			continue
		}
		for _, r := range e.ErrorReferences {
			if r.ReferenceKey == key && r.ReferenceValue == value {
				return true
			}
		}
	}
	return false
}

func TestExecutor_CancelMidMoveStopsAndClears(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move dispatched")
	}

	cancel := order.Action{ActionID: "c1", ActionType: "cancelOrder"}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)

	select {
	case <-cmd.stops:
	case <-time.After(2 * time.Second):
		t.Fatal("no stop dispatched for cancel mid-move")
	}

	// the interrupted move's end event must not commit traversal
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitInstantStatus(t, store, "c1", state.ActionStatusFinished)

	snap := store.Snapshot()
	if len(snap.NodeStates) != 0 || len(snap.EdgeStates) != 0 {
		t.Errorf("states = %+v/%+v, want emptied", snap.NodeStates, snap.EdgeStates)
	}
	if snap.OrderID != "o1" || snap.LastNodeID != "n0" {
		t.Errorf("identity = %s/%s, want kept o1/n0 (no commit of the stopped move)", snap.OrderID, snap.LastNodeID)
	}

	// vehicle is idle again: a new order dispatches
	o2 := singleTargetOrder()
	o2.OrderID = "o2"
	e.OrderAccepted(o2)
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch after cancel + new order")
	}
}

func TestExecutor_CancelFailedHaltedOrderClears(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move dispatched")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !hasStoreError(store, state.ErrorTypeNodeUnreachable) {
		time.Sleep(5 * time.Millisecond)
	}

	cancel := order.Action{ActionID: "c2", ActionType: "cancelOrder"}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)

	waitInstantStatus(t, store, "c2", state.ActionStatusFinished)
	select {
	case <-cmd.stops:
		t.Fatal("stop sent though nothing was in flight")
	default:
	}
	if snap := store.Snapshot(); len(snap.NodeStates) != 0 {
		t.Errorf("NodeStates = %+v, want emptied (the live wedge)", snap.NodeStates)
	}
}

func TestExecutor_CancelWhileIdleFails(t *testing.T) {
	store := vehiclestate.New()
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	cancel := order.Action{ActionID: "c3", ActionType: "cancelOrder"}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)

	waitInstantStatus(t, store, "c3", state.ActionStatusFailed)
	if !hasStoreErrorRef(store, state.ErrorTypeNoOrderToCancel, "actionId", "c3") {
		t.Fatalf("errors = %+v, want NO_ORDER_TO_CANCEL with actionId=c3", store.Snapshot().Errors)
	}
}

func TestExecutor_CancelWrongOrderIdParamFails(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move dispatched")
	}

	cancel := order.Action{
		ActionID: "c4", ActionType: "cancelOrder",
		ActionParameters: []order.ActionParameter{{Key: "orderId", Value: "some-other-order"}},
	}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)

	waitInstantStatus(t, store, "c4", state.ActionStatusFailed)
	if !hasStoreErrorRef(store, state.ErrorTypeNoOrderToCancel, "actionId", "c4") {
		t.Fatalf("errors = %+v, want NO_ORDER_TO_CANCEL with actionId=c4", store.Snapshot().Errors)
	}
	// the running order is undisturbed
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")
}
