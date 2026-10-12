package executor

import (
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func setMode(s *vehiclestate.Store, mode state.OperatingMode) {
	s.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &mode, MobileRobotPosition: s.Snapshot().MobileRobotPosition})
}

func waitModeTransition(t *testing.T, s *vehiclestate.Store) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.ModeChangePending() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.ModeChangePending() {
		t.Fatalf("mode transition did not complete: %+v", s.Snapshot())
	}
}

func TestManualMoveClearWaitsForStopAndSurvivesRapidAutomaticReturn(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	<-cmd.moves
	setMode(store, state.OperatingModeManual)
	select {
	case <-cmd.stops:
	case <-time.After(2 * time.Second):
		t.Fatal("manual transition did not stop move")
	}
	if got := store.Snapshot(); got.OperatingMode != state.OperatingModeAutomatic || len(got.NodeStates) == 0 {
		t.Fatalf("mode or route cleared before stop completed: %+v", got)
	}
	setMode(store, state.OperatingModeAutomatic)
	if store.RequestedOperatingMode() != state.OperatingModeManual {
		t.Fatal("rapid automatic return dropped the pending clear")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false, Reason: "stopped"}}
	waitModeTransition(t, store)
	got := store.Snapshot()
	if got.OrderID != "o1" || got.LastNodeID != "n0" || len(got.NodeStates) != 0 || len(got.EdgeStates) != 0 || store.OrderCancelled() {
		t.Fatalf("mode clear lost identity or masqueraded as fleet cancellation: %+v", got)
	}
	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
		t.Fatal("old order resumed after MANUAL→AUTO")
	case <-time.After(20 * time.Millisecond):
	}
	store.CommitOrderProjection(vehiclestate.OrderProjection{OrderID: "o2", LastNodeID: "n0", NodeStates: []state.NodeState{{NodeID: "n1", SequenceID: 2}}})
	next := singleTargetOrder()
	next.OrderID = "o2"
	e.OrderAccepted(next)
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("a new order did not execute after mode recovery")
	}
}

func TestManualTransitionWaitsForNoninterruptibleUndock(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "stopCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(dockNodeOrder("stopCharging"))
	<-cmd.chargeStops
	waitActionStatus(t, store, "d1", state.ActionStatusRunning)
	setMode(store, state.OperatingModeManual)
	if got := store.Snapshot(); got.OperatingMode != state.OperatingModeAutomatic || got.ActionStates[0].ActionStatus != state.ActionStatusRunning {
		t.Fatalf("noninterruptible action was cleared early: %+v", got)
	}
	select {
	case <-cmd.dockStops:
		t.Fatal("manual transition attempted to abort noninterruptible undock")
	case <-time.After(20 * time.Millisecond):
	}
	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitModeTransition(t, store)
	got := store.Snapshot()
	if got.OperatingMode != state.OperatingModeManual || got.OrderID != "dock-1" || len(got.ActionStates) != 1 || got.ActionStates[0].ActionStatus != state.ActionStatusFinished {
		t.Fatalf("manual transition lost terminal history: %+v", got)
	}
	setMode(store, state.OperatingModeAutomatic)
	e.OrderAccepted(dockNodeOrder("stopCharging"))
	select {
	case <-cmd.chargeStops:
		t.Fatal("completed undock replayed on automatic return")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestModeClearDiscardsBufferedOrdersAndInstants(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	e.OrderAccepted(dockNodeOrder("startCharging"))
	a := order.Action{ActionID: "instant-dock", ActionType: "startCharging"}
	acceptInstant(store, a)
	e.InstantAction(a)
	setMode(store, state.OperatingModeManual)
	setMode(store, state.OperatingModeAutomatic)
	startExecutor(t, e)
	waitModeTransition(t, store)
	waitActionStatus(t, store, "d1", state.ActionStatusFailed)
	waitInstantStatus(t, store, "instant-dock", state.ActionStatusFailed)
	select {
	case <-cmd.chargeStarts:
		t.Fatal("buffered old work ran after mode clearing")
	case <-time.After(20 * time.Millisecond):
	}
}
