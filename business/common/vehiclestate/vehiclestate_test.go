package vehiclestate

import (
	"reflect"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestSetTelemetry_SnapshotSequences(t *testing.T) {
	score, dev, dist, driving := 0.95, 0.1, 3.25, true
	mode := state.OperatingModeManual
	full := Telemetry{
		BatteryState:          &state.BatteryState{BatteryCharge: 55, Charging: true},
		SafetyState:           &state.SafetyState{EStop: state.EStopManual, FieldViolation: true},
		OperatingMode:         &mode,
		Driving:               &driving,
		AGVPosition:           &state.AGVPosition{X: 3.5, Y: -1.25, Theta: 1.57, MapID: "floor-2", PositionInitialized: true, LocalizationScore: &score, DeviationRange: &dev},
		Velocity:              &state.Velocity{Vx: 0.8, Vy: 0, Omega: 0.1},
		Loads:                 []state.Load{{LoadID: "cart-1", LoadType: "CART"}},
		DistanceSinceLastNode: &dist,
	}
	type want struct {
		power   state.BatteryState
		safety  state.SafetyState
		mode    state.OperatingMode
		driving bool
		pos     *state.AGVPosition
		vel     *state.Velocity
		loads   []state.Load
		dist    *float64
	}
	cases := []struct {
		name      string
		snapshots []Telemetry
		want      want
	}{
		{
			name:      "full snapshot populates all fields",
			snapshots: []Telemetry{full},
			want: want{
				power:   state.BatteryState{BatteryCharge: 55, Charging: true},
				safety:  state.SafetyState{EStop: state.EStopManual, FieldViolation: true},
				mode:    state.OperatingModeManual,
				driving: true,
				pos:     &state.AGVPosition{X: 3.5, Y: -1.25, Theta: 1.57, MapID: "floor-2", PositionInitialized: true, LocalizationScore: &score, DeviationRange: &dev},
				vel:     &state.Velocity{Vx: 0.8, Vy: 0, Omega: 0.1},
				loads:   []state.Load{{LoadID: "cart-1", LoadType: "CART"}},
				dist:    &dist,
			},
		},
		{
			name:      "empty snapshot retains required fields and omits optional one",
			snapshots: []Telemetry{full, {}},
			want: want{
				power:   state.BatteryState{BatteryCharge: 55, Charging: true},
				safety:  state.SafetyState{EStop: state.EStopManual, FieldViolation: true},
				mode:    state.OperatingModeManual,
				driving: true,
			},
		},
		{
			name: "later snapshot overwrites earlier one",
			snapshots: []Telemetry{full, {
				BatteryState:  &state.BatteryState{BatteryCharge: 54, Charging: false},
				SafetyState:   &state.SafetyState{EStop: state.EStopNone, FieldViolation: false},
				OperatingMode: func() *state.OperatingMode { m := state.OperatingModeAutomatic; return &m }(),
				Driving:       func() *bool { d := false; return &d }(),
				AGVPosition:   &state.AGVPosition{X: 4.0, Y: -1.0, Theta: 1.6, MapID: "floor-2", PositionInitialized: true},
			}},
			want: want{
				power:   state.BatteryState{BatteryCharge: 54, Charging: false},
				safety:  state.SafetyState{EStop: state.EStopNone, FieldViolation: false},
				mode:    state.OperatingModeAutomatic,
				driving: false,
				pos:     &state.AGVPosition{X: 4.0, Y: -1.0, Theta: 1.6, MapID: "floor-2", PositionInitialized: true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			for _, snap := range tc.snapshots {
				s.SetTelemetry(snap)
			}
			got := s.Snapshot()
			if got.BatteryState != tc.want.power {
				t.Errorf("batteryState = %+v, want %+v", got.BatteryState, tc.want.power)
			}
			if got.SafetyState != tc.want.safety {
				t.Errorf("safetyState = %+v, want %+v", got.SafetyState, tc.want.safety)
			}
			if got.OperatingMode != tc.want.mode {
				t.Errorf("operatingMode = %q, want %q", got.OperatingMode, tc.want.mode)
			}
			if got.Driving != tc.want.driving {
				t.Errorf("driving = %v, want %v", got.Driving, tc.want.driving)
			}
			if !reflect.DeepEqual(got.AGVPosition, tc.want.pos) {
				t.Errorf("agvPosition = %+v, want %+v", got.AGVPosition, tc.want.pos)
			}
			if !reflect.DeepEqual(got.Velocity, tc.want.vel) {
				t.Errorf("velocity = %+v, want %+v", got.Velocity, tc.want.vel)
			}
			if !reflect.DeepEqual(got.Loads, tc.want.loads) {
				t.Errorf("loads = %+v, want %+v", got.Loads, tc.want.loads)
			}
			if !reflect.DeepEqual(got.DistanceSinceLastNode, tc.want.dist) {
				t.Errorf("distanceSinceLastNode = %v, want %v", got.DistanceSinceLastNode, tc.want.dist)
			}
		})
	}
}

func TestSetTelemetry_LeavesOrderProjectionUntouched(t *testing.T) {
	s := New()

	p := OrderProjection{
		OrderID:            "order-1",
		OrderUpdateID:      2,
		LastNodeID:         "n0",
		LastNodeSequenceID: 0,
		NodeStates:         []state.NodeState{{NodeID: "n2", SequenceID: 2, Released: true}},
		EdgeStates:         []state.EdgeState{{EdgeID: "e1", SequenceID: 1, Released: true}},
		ActionStates:       []state.ActionState{{ActionID: "a1", ActionStatus: state.ActionStatusWaiting}},
	}
	s.CommitOrderProjection(p)
	robotErr := state.Error{ErrorType: state.ErrorTypeMobileRobotNotAvailable, ErrorLevel: state.ErrorLevelFatal}
	s.AddError(robotErr)

	driving := false
	mode := state.OperatingModeAutomatic
	s.SetTelemetry(Telemetry{
		BatteryState:  &state.BatteryState{BatteryCharge: 12, Charging: false},
		AGVPosition:   &state.AGVPosition{X: 1, Y: 2, MapID: "floor-2", PositionInitialized: true},
		SafetyState:   &state.SafetyState{EStop: state.EStopNone, FieldViolation: false},
		Driving:       &driving,
		OperatingMode: &mode,
	})

	snap := s.Snapshot()
	if snap.OrderID != p.OrderID || snap.OrderUpdateID != p.OrderUpdateID {
		t.Errorf("order identity = %q/%d, want %q/%d", snap.OrderID, snap.OrderUpdateID, p.OrderID, p.OrderUpdateID)
	}
	if snap.LastNodeID != p.LastNodeID || snap.LastNodeSequenceID != p.LastNodeSequenceID {
		t.Errorf("lastNode = %q/%d, want %q/%d", snap.LastNodeID, snap.LastNodeSequenceID, p.LastNodeID, p.LastNodeSequenceID)
	}
	if !reflect.DeepEqual(snap.NodeStates, p.NodeStates) {
		t.Errorf("nodeStates = %+v, want %+v", snap.NodeStates, p.NodeStates)
	}
	if !reflect.DeepEqual(snap.EdgeStates, p.EdgeStates) {
		t.Errorf("edgeStates = %+v, want %+v", snap.EdgeStates, p.EdgeStates)
	}
	if !reflect.DeepEqual(snap.ActionStates, p.ActionStates) {
		t.Errorf("actionStates = %+v, want %+v", snap.ActionStates, p.ActionStates)
	}
	if !reflect.DeepEqual(snap.Errors, []state.Error{robotErr}) {
		t.Errorf("errors = %+v, want %+v", snap.Errors, []state.Error{robotErr})
	}
}

func TestLocalizationErrorClearsWithConfirmedLocalization(t *testing.T) {
	s := New()
	s.SetTelemetry(Telemetry{AGVPosition: &state.AGVPosition{MapID: "m", PositionInitialized: false}})
	got := s.Snapshot()
	if len(got.Errors) != 1 || got.Errors[0].ErrorType != state.ErrorTypeLocalizationError || got.Errors[0].ErrorLevel != state.ErrorLevelFatal {
		t.Fatalf("missing localization failure: %+v", got.Errors)
	}
	s.AddInstantActionState(state.ActionState{ActionID: "loc", ActionStatus: state.ActionStatusRunning})
	s.CompleteRelocalization("loc", state.AGVPosition{MapID: "m", PositionInitialized: true}, "n0")
	got = s.Snapshot()
	if len(got.Errors) != 0 || !got.AGVPosition.PositionInitialized || got.LastNodeID != "n0" || got.InstantActionStates[0].ActionStatus != state.ActionStatusFinished {
		t.Fatalf("localization completion not coherent: %+v", got)
	}
}
