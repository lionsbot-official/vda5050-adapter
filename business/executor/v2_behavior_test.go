package executor

import (
	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
	"testing"
	"time"
)

func TestCancelWaitsForNoninterruptibleUndock(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "stopCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(dockNodeOrder("stopCharging"))
	<-cmd.chargeStops
	a := order.Action{ActionID: "cancel", ActionType: "cancelOrder"}
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "cancel", state.ActionStatusRunning)
	if vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("order became idle during undock")
	}
	select {
	case <-cmd.dockStops:
		t.Fatal("undock must not send dock_stop")
	default:
	}
	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitInstantStatus(t, store, "cancel", state.ActionStatusFinished)
	waitActionStatus(t, store, "d1", state.ActionStatusFinished)
}

func TestCancelWaitsForAutoUndockWithoutDispatchingMove(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	setCharging(store, true)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	<-cmd.chargeStops
	a := order.Action{ActionID: "cancel", ActionType: "cancelOrder"}
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "cancel", state.ActionStatusRunning)
	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitInstantStatus(t, store, "cancel", state.ActionStatusFinished)
	select {
	case <-cmd.moves:
		t.Fatal("cancelled auto-undock dispatched move")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestManualTransitionDiscardsBufferedOrderAndInstant(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	cmd := newFakeCommander()
	e := New(store, cmd)
	e.OrderAccepted(singleTargetOrder())
	a := order.Action{ActionID: "dock", ActionType: "startCharging"}
	acceptInstant(store, a)
	e.InstantAction(a)
	manual, automatic := state.OperatingModeManual, state.OperatingModeAutomatic
	store.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &manual})
	store.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &automatic})
	startExecutor(t, e)
	if got := store.Snapshot(); got.OrderID != "" || len(got.NodeStates) != 0 || len(got.ActionStates) != 0 {
		t.Fatalf("manual transition retained order: %+v", got)
	}
	waitInstantStatus(t, store, "dock", state.ActionStatusFailed)
	select {
	case <-cmd.moves:
		t.Fatal("stale order dispatched after manual control")
	case <-cmd.chargeStarts:
		t.Fatal("stale instant dispatched after manual control")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestManualTransitionDiscardsInFlightTraversal(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	<-cmd.moves
	manual, automatic := state.OperatingModeManual, state.OperatingModeAutomatic
	store.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &manual})
	store.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &automatic})
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	time.Sleep(25 * time.Millisecond)
	if got := store.Snapshot(); got.OrderID != "" || got.LastNodeID != "n0" {
		t.Fatalf("stale completion restored manual-cleared order: %+v", got)
	}
}

func TestRepeatedOrderNotificationDoesNotRepeatFinishedNodeAction(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(dockNodeOrder("startCharging"))
	<-cmd.chargeStarts
	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	waitActionStatus(t, store, "d1", state.ActionStatusFinished)
	e.OrderAccepted(dockNodeOrder("startCharging"))
	select {
	case <-cmd.chargeStarts:
		t.Fatal("finished node action executed again")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestNavigationRechecksReadinessAfterAcceptance(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		store := vehiclestate.New()
		seedStore(store, 1, 2)
		pose := store.Snapshot().AGVPosition
		pose.PositionInitialized = initialized
		if initialized {
			pose.MapID = "different-map"
		}
		store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: pose})
		cmd := newFakeCommander()
		e := New(store, cmd)
		startExecutor(t, e)
		e.OrderAccepted(singleTargetOrder())
		select {
		case <-cmd.moves:
			t.Fatal("dispatched without initialized matching map")
		case <-time.After(25 * time.Millisecond):
		}
		if e.withinDeviation(singleTargetOrder().Nodes[1]) {
			t.Fatal("uninitialized/cross-map pose counted as reached")
		}
	}
}

func TestLocalizationLossStopsAndResumesInterruptedTarget(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(time.Second):
		t.Fatal("initial move missing")
	}
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: false}})
	select {
	case <-cmd.stops:
	case <-time.After(time.Second):
		t.Fatal("localization loss did not stop move")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	select {
	case <-cmd.moves:
		t.Fatal("redispatched while unlocalized")
	case <-time.After(25 * time.Millisecond):
	}
	if got := store.Snapshot(); got.LastNodeID != "n0" || got.OrderID != "o1" {
		t.Fatalf("interrupted target counted as traversed: %+v", got)
	}
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	select {
	case m := <-cmd.moves:
		if m.x != 1 || m.y != 2 {
			t.Fatalf("wrong resumed target: %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("did not resume interrupted target")
	}
}

func TestLocalizationLossStopsCleaningAndWaitsForEnd(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(cleaningOrder())
	select {
	case <-cmd.cleanStarts:
	case <-time.After(time.Second):
		t.Fatal("clean did not start")
	}
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: false}})
	select {
	case <-cmd.cleanStops:
	case <-time.After(time.Second):
		t.Fatal("clean not stopped on readiness loss")
	}
	if store.Snapshot().ActionStates[0].ActionStatus != state.ActionStatusRunning {
		t.Fatal("stop write treated as terminal feedback")
	}
	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: true}}
	waitActionStatus(t, store, "a1", state.ActionStatusFailed)
}
