package vehiclestate

import (
	"reflect"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestSetTelemetry_SnapshotSequences(t *testing.T) {
	score, dev, dist, driving := 0.95, 0.1, 3.25, true
	mode := state.OperatingModeManual
	full := Telemetry{
		PowerSupply:           &state.PowerSupply{StateOfCharge: 55, Charging: true},
		SafetyState:           &state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual, FieldViolation: true},
		OperatingMode:         &mode,
		Driving:               &driving,
		MobileRobotPosition:   &state.MobileRobotPosition{X: 3.5, Y: -1.25, Theta: 1.57, MapID: "floor-2", Localized: true, LocalizationScore: &score, DeviationRange: &dev},
		Velocity:              &state.Velocity{Vx: 0.8, Vy: 0, Omega: 0.1},
		Loads:                 []state.Load{{LoadID: "cart-1", LoadType: "CART"}},
		DistanceSinceLastNode: &dist,
	}
	type want struct {
		power   state.PowerSupply
		safety  state.SafetyState
		mode    state.OperatingMode
		driving bool
		pos     *state.MobileRobotPosition
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
				power:   state.PowerSupply{StateOfCharge: 55, Charging: true},
				safety:  state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual, FieldViolation: true},
				mode:    state.OperatingModeManual,
				driving: true,
				pos:     &state.MobileRobotPosition{X: 3.5, Y: -1.25, Theta: 1.57, MapID: "floor-2", Localized: true, LocalizationScore: &score, DeviationRange: &dev},
				vel:     &state.Velocity{Vx: 0.8, Vy: 0, Omega: 0.1},
				loads:   []state.Load{{LoadID: "cart-1", LoadType: "CART"}},
				dist:    &dist,
			},
		},
		{
			name:      "empty snapshot retains required fields and omits optional one",
			snapshots: []Telemetry{full, {}},
			want: want{
				power:   state.PowerSupply{StateOfCharge: 55, Charging: true},
				safety:  state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual, FieldViolation: true},
				mode:    state.OperatingModeManual,
				driving: true,
			},
		},
		{
			name: "later snapshot overwrites earlier one",
			snapshots: []Telemetry{full, {
				PowerSupply:         &state.PowerSupply{StateOfCharge: 54, Charging: false},
				SafetyState:         &state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false},
				OperatingMode:       func() *state.OperatingMode { m := state.OperatingModeAutomatic; return &m }(),
				Driving:             func() *bool { d := false; return &d }(),
				MobileRobotPosition: &state.MobileRobotPosition{X: 4.0, Y: -1.0, Theta: 1.6, MapID: "floor-2", Localized: true},
			}},
			want: want{
				power:   state.PowerSupply{StateOfCharge: 54, Charging: false},
				safety:  state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false},
				mode:    state.OperatingModeAutomatic,
				driving: false,
				pos:     &state.MobileRobotPosition{X: 4.0, Y: -1.0, Theta: 1.6, MapID: "floor-2", Localized: true},
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
			if got.PowerSupply != tc.want.power {
				t.Errorf("powerSupply = %+v, want %+v", got.PowerSupply, tc.want.power)
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
			if !reflect.DeepEqual(got.MobileRobotPosition, tc.want.pos) {
				t.Errorf("mobileRobotPosition = %+v, want %+v", got.MobileRobotPosition, tc.want.pos)
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

func TestSetTelemetry_PlannedPathSetAndCleared(t *testing.T) {
	s := New()
	path := &state.PlannedPath{Trajectory: common.Trajectory{
		ControlPoints: []common.ControlPoint{{X: 1, Y: 2}, {X: 3, Y: 4}},
	}}

	s.SetTelemetry(Telemetry{PlannedPath: path})
	got := s.Snapshot().PlannedPath
	if got == nil || !reflect.DeepEqual(got.Trajectory.ControlPoints, path.Trajectory.ControlPoints) {
		t.Fatalf("PlannedPath = %+v, want the supplied path", got)
	}

	// absent in the next snapshot: cleared, never stale
	s.SetTelemetry(Telemetry{})
	if got := s.Snapshot().PlannedPath; got != nil {
		t.Fatalf("PlannedPath = %+v, want nil after a snapshot without one", got)
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
	robotErr := state.Error{ErrorType: state.ErrorTypeReleaseLost, ErrorLevel: state.ErrorLevelFatal}
	s.AddError(robotErr)

	driving := false
	mode := state.OperatingModeAutomatic
	s.SetTelemetry(Telemetry{
		PowerSupply:         &state.PowerSupply{StateOfCharge: 12, Charging: false},
		MobileRobotPosition: &state.MobileRobotPosition{X: 1, Y: 2, MapID: "floor-2", Localized: true},
		SafetyState:         &state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false},
		Driving:             &driving,
		OperatingMode:       &mode,
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

func TestOrderUpdatePreservesActionProgressAndInstantHistory(t *testing.T) {
	s := New()
	projection := OrderProjection{OrderID: "order-1", ActionStates: []state.ActionState{{ActionID: "clean-1", ActionStatus: state.ActionStatusWaiting}}}
	s.CommitOrderProjection(projection)
	s.SetActionStatus("clean-1", state.ActionStatusRunning, "cleaning")
	s.AddInstantActionState(state.ActionState{ActionID: "instant-1", ActionStatus: state.ActionStatusFinished})
	projection.KeepLastNode = true
	projection.OrderUpdateID = 1
	projection.ActionStates = []state.ActionState{{ActionID: "clean-1", ActionStatus: state.ActionStatusWaiting}, {ActionID: "clean-2", ActionStatus: state.ActionStatusWaiting}}
	s.CommitOrderProjection(projection)
	got := s.Snapshot()
	if got.ActionStates[0].ActionStatus != state.ActionStatusRunning || got.ActionStates[0].ActionResult != "cleaning" || got.ActionStates[1].ActionStatus != state.ActionStatusWaiting {
		t.Fatalf("update changed action progress: %+v", got.ActionStates)
	}
	projection.KeepLastNode = false
	projection.OrderID = "order-2"
	s.CommitOrderProjection(projection)
	if got := s.Snapshot().InstantActionStates; len(got) != 1 || got[0].ActionID != "instant-1" {
		t.Fatalf("V3 instant history must survive a new order: %+v", got)
	}
}

func TestReadinessAndPublisherNotificationsAreIndependent(t *testing.T) {
	s := New()
	mode := state.OperatingModeManual
	s.SetTelemetry(Telemetry{OperatingMode: &mode})
	select {
	case <-s.ReadinessChanges():
	default:
		t.Fatal("no executor readiness notification")
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("executor readiness consumed publisher notification")
	}
}

func TestLocalizationErrorClearsWhenPoseRecovers(t *testing.T) {
	s := New()
	s.SetTelemetry(Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1"}})
	got := s.Snapshot()
	if len(got.Errors) != 1 || got.Errors[0].ErrorType != state.ErrorTypeLocalizationError || got.Errors[0].ErrorLevel != state.ErrorLevelFatal {
		t.Fatalf("unlocalized pose missing fatal error: %+v", got.Errors)
	}
	s.SetTelemetry(Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1"}})
	if len(s.Snapshot().Errors) != 1 {
		t.Fatal("repeated unlocalized telemetry duplicated error")
	}
	s.SetTelemetry(Telemetry{MobileRobotPosition: &state.MobileRobotPosition{MapID: "map-1", Localized: true}})
	if len(s.Snapshot().Errors) != 0 {
		t.Fatal("localization recovery left fatal error")
	}
}

func TestAvailableOperatingModeClearsAvailabilityError(t *testing.T) {
	s := New()
	s.AddError(state.Error{ErrorType: state.ErrorTypeMobileRobotNotAvailable, ErrorLevel: state.ErrorLevelWarning})
	mode := state.OperatingModeManual
	s.SetTelemetry(Telemetry{OperatingMode: &mode})
	if len(s.Snapshot().Errors) != 1 {
		t.Fatal("unavailable mode cleared its error")
	}
	mode = state.OperatingModeIntervened
	s.SetTelemetry(Telemetry{OperatingMode: &mode})
	if len(s.Snapshot().Errors) != 0 {
		t.Fatal("order-accepting mode retained availability error")
	}
}

func TestClearingModesPreserveOrderHistoryAndResetLastNodeOnlyWhenRequired(t *testing.T) {
	for _, mode := range []state.OperatingMode{state.OperatingModeManual, state.OperatingModeService, state.OperatingModeTeachIn, state.OperatingModeStartup} {
		t.Run(string(mode), func(t *testing.T) {
			s := New()
			s.CommitOrderProjection(OrderProjection{OrderID: "old", OrderUpdateID: 3, LastNodeID: "n7", LastNodeSequenceID: 6,
				ActionStates: []state.ActionState{{ActionID: "finished", ActionStatus: state.ActionStatusFinished}}})
			s.SetTelemetry(Telemetry{OperatingMode: &mode})
			got := s.Snapshot()
			wantNode := ""
			if mode == state.OperatingModeManual {
				wantNode = "n7"
			}
			if got.OperatingMode != mode || got.OrderID != "old" || got.OrderUpdateID != 3 || got.LastNodeID != wantNode || got.LastNodeSequenceID != 6 || len(got.ActionStates) != 1 || !s.OrderCleared() || s.OrderCancelled() {
				t.Fatalf("incorrect mode clear: %+v", got)
			}
		})
	}
}

func TestPendingModeClearRetainsAvailabilityErrorUntilCompletion(t *testing.T) {
	s := New()
	s.CommitOrderProjection(OrderProjection{OrderID: "old", NodeStates: []state.NodeState{{NodeID: "target"}}})
	mode := state.OperatingModeManual
	s.SetTelemetry(Telemetry{OperatingMode: &mode})
	s.AddError(state.Error{ErrorType: state.ErrorTypeMobileRobotNotAvailable, ErrorLevel: state.ErrorLevelWarning})
	mode = state.OperatingModeAutomatic
	s.SetTelemetry(Telemetry{OperatingMode: &mode})
	if !s.ModeChangePending() || len(s.Snapshot().Errors) != 1 {
		t.Fatal("pending mode clear must retain unavailability")
	}
	s.CompleteModeTransition()
	if s.ModeChangePending() || len(s.Snapshot().Errors) != 0 {
		t.Fatal("completed transition to automatic must clear unavailability")
	}
}
