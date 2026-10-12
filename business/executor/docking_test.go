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

// dockNodeOrder is a single positionless seq-0 released node carrying a docking
// action; dock_start/undock_start self-navigate, so no move node is needed.
func dockNodeOrder(actionType string) order.Order {
	return order.Order{
		OrderID: "dock-1",
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true, Actions: []order.Action{{
				ActionID: "d1", ActionType: actionType,
			}}},
		},
		Edges: []order.Edge{},
	}
}

func seedDockStore(s *vehiclestate.Store, actionType string) {
	s.CommitOrderProjection(vehiclestate.OrderProjection{
		OrderID: "dock-1", LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{}, EdgeStates: []state.EdgeState{},
		ActionStates: []state.ActionState{
			{ActionID: "d1", ActionType: actionType, ActionStatus: state.ActionStatusWaiting},
		},
	})
	s.SetTelemetry(vehiclestate.Telemetry{
		AGVPosition: &state.AGVPosition{X: 1, Y: 2, MapID: "map-1", PositionInitialized: true},
	})
}

func TestExecutor_DockNodeAction_DispatchesAndFinishes(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(dockNodeOrder("startCharging"))

	select {
	case <-cmd.chargeStarts:
	case <-time.After(2 * time.Second):
		t.Fatal("dock (StartCharging) not dispatched for a node action")
	}
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)

	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitActionStatus(t, store, "d1", state.ActionStatusFinished)
}

func TestExecutor_DockNodeAction_FailureReported(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(dockNodeOrder("startCharging"))
	select {
	case <-cmd.chargeStarts:
	case <-time.After(2 * time.Second):
		t.Fatal("dock not dispatched")
	}

	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: false, Reason: "stuck"}}
	waitActionStatus(t, store, "d1", state.ActionStatusFailed)
	for _, as := range store.Snapshot().ActionStates {
		if as.ActionID == "d1" && as.ResultDescription != "stuck" {
			t.Errorf("ResultDescription = %q, want stuck", as.ResultDescription)
		}
	}
}

func TestExecutor_PauseResumeMidDock_RoutesByOpKind(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(dockNodeOrder("startCharging"))
	<-cmd.chargeStarts
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)

	// startPause during a dock: dock_pause, never p2p_pause/clean_pause
	pause := order.Action{ActionID: "dp1", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.InstantAction(pause)
	select {
	case <-cmd.dockPauses:
	case <-time.After(2 * time.Second):
		t.Fatal("startPause mid-dock did not send dock_pause")
	}
	select {
	case <-cmd.pauses:
		t.Fatal("sent p2p_pause for a dock; pause must route by op kind")
	case <-cmd.cleanPauses:
		t.Fatal("sent clean_pause for a dock; pause must route by op kind")
	case <-time.After(50 * time.Millisecond):
	}
	waitPaused(t, store, true)
	waitInstantStatus(t, store, "dp1", state.ActionStatusFinished)

	// stopPause during a dock: dock_continue, never p2p_continue/clean_continue
	resume := order.Action{ActionID: "dp2", ActionType: "stopPause"}
	acceptInstant(store, resume)
	e.InstantAction(resume)
	select {
	case <-cmd.dockResumes:
	case <-time.After(2 * time.Second):
		t.Fatal("stopPause mid-dock did not send dock_continue")
	}
	select {
	case <-cmd.resumes:
		t.Fatal("sent p2p_continue for a dock; resume must route by op kind")
	case <-cmd.cleanResumes:
		t.Fatal("sent clean_continue for a dock; resume must route by op kind")
	case <-time.After(50 * time.Millisecond):
	}
	waitPaused(t, store, false)
	waitInstantStatus(t, store, "dp2", state.ActionStatusFinished)
}

func TestExecutor_CancelMidDock_StopsAndClears(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(dockNodeOrder("startCharging"))
	<-cmd.chargeStarts
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)

	cancel := order.Action{ActionID: "dc1", ActionType: "cancelOrder"}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)

	// a running dock must be aborted on the robot (dock_stop), not just dropped
	select {
	case <-cmd.dockStops:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelOrder mid-dock did not send dock_stop")
	}
	waitInstantStatus(t, store, "dc1", state.ActionStatusRunning)
	if vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("order became idle before robot completion")
	}
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)
	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitActionStatus(t, store, "d1", state.ActionStatusFailed)
	waitInstantStatus(t, store, "dc1", state.ActionStatusFinished)
	if !vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle not idle after dock cancel")
	}

	// the robot's own dock end status arrives after the cancel: stale, ignored
	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	time.Sleep(50 * time.Millisecond)
	if got := actionState(t, store, "d1"); got.ActionStatus != state.ActionStatusFailed {
		t.Errorf("action = %+v, late dock end must not resurrect a cancelled dock", got)
	}
}

func TestExecutor_CancelMidDock_StopErrorFailsCancel(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	store.AddInstantActionState(state.ActionState{ActionID: "dc2", ActionType: "cancelOrder", ActionStatus: state.ActionStatusWaiting})
	cmd := newFakeCommander()
	cmd.dockStopErr = errors.New("ws down")
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(dockNodeOrder("startCharging"))
	<-cmd.chargeStarts
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)

	e.InstantAction(order.Action{ActionID: "dc2", ActionType: "cancelOrder"})

	// the abort could not be dispatched: the cancel fails and the dock stays held
	// (RUNNING) so a real dock end still resolves it, never a false idle
	waitInstantStatus(t, store, "dc2", state.ActionStatusFailed)
	if got := actionState(t, store, "d1"); got.ActionStatus != state.ActionStatusRunning {
		t.Errorf("dock action = %+v, want still RUNNING after a failed cancel stop", got)
	}
	if vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle went idle despite a dock that could not be stopped")
	}
}

func TestExecutor_UndockNodeAction_DispatchesAndFinishes(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "stopCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(dockNodeOrder("stopCharging"))

	select {
	case <-cmd.chargeStops:
	case <-time.After(2 * time.Second):
		t.Fatal("undock (StopCharging) not dispatched for a node action")
	}
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)

	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitActionStatus(t, store, "d1", state.ActionStatusFinished)
}
