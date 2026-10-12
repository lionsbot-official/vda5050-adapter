package executor

import (
	"context"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func TestHeldPlanDispatchesOnlyWhenReady(t *testing.T) {
	for _, tc := range []struct {
		name string
		pose *state.MobileRobotPosition
		mode state.OperatingMode
	}{
		{"unknown pose", nil, state.OperatingModeAutomatic},
		{"unlocalized", &state.MobileRobotPosition{MapID: "map-1"}, state.OperatingModeAutomatic},
		{"intervened", &state.MobileRobotPosition{MapID: "map-1", Localized: true}, state.OperatingModeIntervened},
		{"different map", &state.MobileRobotPosition{MapID: "map-2", Localized: true}, state.OperatingModeAutomatic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := vehiclestate.New()
			seedStore(store, 0, 0)
			store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: tc.pose, OperatingMode: &tc.mode})
			cmd := newFakeCommander()
			e := New(store, cmd)
			startExecutor(t, e)
			e.OrderAccepted(singleTargetOrder())
			select {
			case <-cmd.moves:
				t.Fatal("dispatched without localization/map/mode readiness")
			case <-time.After(20 * time.Millisecond):
			}
			automatic := state.OperatingModeAutomatic
			store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1", Localized: true}, OperatingMode: &automatic})
			select {
			case <-cmd.moves:
			case <-time.After(2 * time.Second):
				t.Fatal("held plan did not dispatch when ready")
			}
		})
	}
}

func TestRelocalizationWakesHeldNodeAction(t *testing.T) {
	store := vehiclestate.New()
	seedDockStore(store, "startCharging")
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1"}})
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(dockNodeOrder("startCharging"))
	select {
	case <-cmd.chargeStarts:
		t.Fatal("node action dispatched before localization")
	case <-time.After(20 * time.Millisecond):
	}
	acceptInstant(store, order.Action{ActionID: "loc", ActionType: "initializePosition"})
	store.CompleteRelocalization("loc", state.MobileRobotPosition{Localized: true, MapID: "map-1"}, "n0")
	select {
	case <-cmd.chargeStarts:
	case <-time.After(2 * time.Second):
		t.Fatal("relocalization did not wake held node action")
	}
}

func TestLocalizationLossBlocksAutoUndockContinuation(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	setCharging(store, true)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.chargeStops:
	case <-time.After(2 * time.Second):
		t.Fatal("no auto-undock")
	}
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1"}, PowerSupply: &state.PowerSupply{Charging: false}})
	cmd.events <- robotbus.ExecutionEvent{StopChargingCompleted: &robotbus.ChargingCompleted{Success: true}}
	select {
	case <-cmd.moves:
		t.Fatal("undock completion dispatched while unlocalized")
	case <-time.After(20 * time.Millisecond):
	}
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1", Localized: true}})
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("held move did not resume after localization")
	}
}

func TestLocalizationLossStopsMoveAndWaitsForCompletionBeforeRecovery(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	<-cmd.moves
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1"}})
	select {
	case <-cmd.stops:
	case <-time.After(2 * time.Second):
		t.Fatal("localization loss did not stop active move")
	}
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1", Localized: true}})
	select {
	case <-cmd.moves:
		t.Fatal("restarted before stop completion")
	case <-time.After(20 * time.Millisecond):
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false, Reason: "stopped"}}
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("lost interrupted target after localization recovered")
	}
	if got := store.Snapshot().LastNodeID; got != "n0" {
		t.Fatalf("interrupted move committed traversal: %s", got)
	}
}

func TestMapLossStopFailureKeepsMoveTracked(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1, 2)
	cmd := newFakeCommander()
	cmd.stopErr = context.DeadlineExceeded
	e := New(store, cmd)
	startExecutor(t, e)
	e.OrderAccepted(singleTargetOrder())
	<-cmd.moves
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-2", Localized: true}})
	waitError(t, store, "n1")
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1", Localized: true, X: 1, Y: 2}})
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")
	select {
	case <-cmd.moves:
		t.Fatal("failed stop lost track of the still-running move")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestLocalizationLossStopsCleaningAndDockingBeforeFailing(t *testing.T) {
	for _, kind := range []string{"clean", "dock", "instant dock"} {
		t.Run(kind, func(t *testing.T) {
			store := vehiclestate.New()
			cmd := newFakeCommander()
			e := New(store, cmd)
			actionID := "d1"
			switch kind {
			case "clean":
				seedCleaningStore(store)
				actionID = "a1"
			case "dock":
				seedDockStore(store, "startCharging")
			default:
				store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{Localized: true, MapID: "map-1"}})
			}
			startExecutor(t, e)
			switch kind {
			case "clean":
				e.OrderAccepted(cleaningOrder())
				<-cmd.cleanStarts
			case "dock":
				e.OrderAccepted(dockNodeOrder("startCharging"))
				<-cmd.chargeStarts
			default:
				a := order.Action{ActionID: actionID, ActionType: "startCharging"}
				acceptInstant(store, a)
				e.InstantAction(a)
				<-cmd.chargeStarts
			}
			store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1"}})
			if kind == "clean" {
				select {
				case <-cmd.cleanStops:
				case <-time.After(2 * time.Second):
					t.Fatal("localization loss did not stop cleaning")
				}
			} else {
				select {
				case <-cmd.dockStops:
				case <-time.After(2 * time.Second):
					t.Fatal("localization loss did not abort docking")
				}
			}
			if kind == "instant dock" {
				waitInstantStatus(t, store, actionID, state.ActionStatusRunning)
			} else {
				waitActionStatus(t, store, actionID, state.ActionStatusRunning)
			}
			if kind == "clean" {
				cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: false}}
			} else {
				cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: false}}
			}
			if kind == "instant dock" {
				waitInstantStatus(t, store, actionID, state.ActionStatusFailed)
			} else {
				waitActionStatus(t, store, actionID, state.ActionStatusFailed)
			}
		})
	}
}

func TestSafetyInterlocksHoldDispatch(t *testing.T) {
	for _, kind := range []string{"emergency stop", "field violation", "fatal error"} {
		t.Run(kind, func(t *testing.T) {
			store := vehiclestate.New()
			seedStore(store, 0, 0)
			safety := state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone}
			switch kind {
			case "emergency stop":
				safety.ActiveEmergencyStop = state.ActiveEmergencyStopManual
			case "field violation":
				safety.FieldViolation = true
			case "fatal error":
				store.AddError(state.Error{ErrorType: state.ErrorTypeReleaseLost, ErrorLevel: state.ErrorLevelFatal})
			}
			store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: store.Snapshot().MobileRobotPosition, SafetyState: &safety})
			cmd := newFakeCommander()
			e := New(store, cmd)
			startExecutor(t, e)
			e.OrderAccepted(singleTargetOrder())
			select {
			case <-cmd.moves:
				t.Fatal("dispatched through safety interlock")
			case <-time.After(20 * time.Millisecond):
			}
			safety = state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone}
			if kind == "fatal error" {
				store.AddError(state.Error{ErrorType: state.ErrorTypeReleaseLost, ErrorLevel: state.ErrorLevelWarning})
			}
			store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: store.Snapshot().MobileRobotPosition, SafetyState: &safety})
			select {
			case <-cmd.moves:
			case <-time.After(2 * time.Second):
				t.Fatal("held plan did not resume after safety cleared")
			}
		})
	}
}
