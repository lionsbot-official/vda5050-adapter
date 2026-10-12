package executor

import (
	"context"
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

func TestFailedCancelStopKeepsMoveTracked(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	cmd := newFakeCommander()
	cmd.stopErr = context.DeadlineExceeded
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	<-cmd.moves
	a := order.Action{ActionID: "cancel", ActionType: "cancelOrder"}
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "cancel", state.ActionStatusFailed)
	if store.OrderCancelled() {
		t.Fatal("failed stop marked order cancelled")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")
}

func TestCancelledOrderNotificationCannotRestartMovement(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	cmd := newFakeCommander()
	e := New(store, cmd)
	e.OrderAccepted(singleTargetOrder())
	store.CancelOrder()
	startExecutor(t, e)
	select {
	case <-cmd.moves:
		t.Fatal("buffered cancelled order dispatched")
	case <-time.After(20 * time.Millisecond):
	}
}
