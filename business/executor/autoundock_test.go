package executor

import (
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func setCharging(s *vehiclestate.Store, charging bool) {
	s.SetTelemetry(vehiclestate.Telemetry{
		PowerSupply:         &state.PowerSupply{StateOfCharge: 80, Charging: charging},
		MobileRobotPosition: s.Snapshot().MobileRobotPosition,
	})
}

func hasStoreError(s *vehiclestate.Store, et state.ErrorType) bool {
	for _, e := range s.Snapshot().Errors {
		if e.ErrorType == et {
			return true
		}
	}
	return false
}

func TestExecutor_AutoUndocksBeforeFirstMove(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	setCharging(store, true)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())

	select {
	case <-cmd.chargeStops:
	case <-time.After(2 * time.Second):
		t.Fatal("no undock dispatched for an order accepted while charging")
	}
	select {
	case m := <-cmd.moves:
		t.Fatalf("move %+v dispatched before undock completed", m)
	case <-time.After(50 * time.Millisecond):
	}

	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: true}}

	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("move never dispatched after undock completed")
	}
}

func TestExecutor_UndockFailureHaltsOrder(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	setCharging(store, true)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.chargeStops:
	case <-time.After(2 * time.Second):
		t.Fatal("no undock dispatched")
	}

	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: false, Reason: "jam"}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hasStoreError(store, state.ErrorTypeNodeUnreachable) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !hasStoreError(store, state.ErrorTypeNodeUnreachable) {
		t.Fatalf("errors = %+v, want NODE_UNREACHABLE after undock failure", store.Snapshot().Errors)
	}
	select {
	case m := <-cmd.moves:
		t.Fatalf("move %+v dispatched after undock failure", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestExecutor_UndockWriteErrorHaltsOrder(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	setCharging(store, true)
	cmd := newFakeCommander()
	cmd.chargeErr = errChargeDown
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hasStoreError(store, state.ErrorTypeNodeUnreachable) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !hasStoreError(store, state.ErrorTypeNodeUnreachable) {
		t.Fatalf("errors = %+v, want NODE_UNREACHABLE after undock write error", store.Snapshot().Errors)
	}
	select {
	case m := <-cmd.moves:
		t.Fatalf("move %+v dispatched after undock write error", m)
	case <-time.After(50 * time.Millisecond):
	}
}
