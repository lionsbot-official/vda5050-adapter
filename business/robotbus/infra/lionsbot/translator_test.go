package lionsbot

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func ptr[T any](v T) *T { return &v }

func TestTranslate_PowerSupply(t *testing.T) {
	cases := []struct {
		name      string
		frame     string
		wantPower state.PowerSupply
	}{
		{
			name: "soc, charging, voltage; other battery fields omitted",
			frame: `{
							"operation_fb": "touchscreen_robot_status",
							"robotId": "R3-2300001-SCR",
							"content": {
											"charging": true,
											"battery_info": { "soc": 42.5, "voltage": 48.2 }
							}
			}`,
			wantPower: state.PowerSupply{
				StateOfCharge:  42.5,
				Charging:       true,
				BatteryVoltage: ptr(48.2),
			},
		},
		{
			name: "voltage absent -> BatteryVoltage omitted, not zero",
			frame: `{
							"operation_fb": "touchscreen_robot_status",
							"content": { "charging": false, "battery_info": { "soc": 88 } }
			}`,
			wantPower: state.PowerSupply{
				StateOfCharge: 88,
				Charging:      false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tr translator
			got, err := tr.translate([]byte(tc.frame))
			if err != nil {
				t.Fatalf("translate returned error: %v", err)
			}
			if got.PowerSupply == nil {
				t.Fatalf("PowerSupply is nil, want %+v", tc.wantPower)
			}
			if !reflect.DeepEqual(*got.PowerSupply, tc.wantPower) {
				t.Errorf("PowerSupply = %+v, want %+v", *got.PowerSupply, tc.wantPower)
			}
		})
	}
}

func TestTranslate_Driving(t *testing.T) {
	cases := []struct {
		name        string
		version     codeVersion
		code        int
		wantDriving bool
	}{
		{"v1 R3 code 2 = Moving -> driving true", codeV1, 2, true},
		{"v0 LeoBot code 2 = MovingPaused -> driving false", codeV0, 2, false},
		{"v2 R5 code 2 = Moving -> driving true", codeV2, 2, true},
		{"v1 R3 code 4 = Cleaning -> driving true", codeV1, 4, true},
		{"v1 R3 code 0 = Resting -> driving false", codeV1, 0, false},
		{"v0 LeoBot code 5 = Teleoping -> driving true", codeV0, 5, true},

		{"v1 R3 code 19 = Entering Lift -> driving true", codeV1, 19, true},
		{"v2 R5 code 11 = Explore -> driving true", codeV2, 11, true},
		{"v2 R5 code 18 = Manual Dock (human push) -> driving false", codeV2, 18, false},
		{"v0 LeoBot code 40 = Mapping (human push) -> driving false", codeV0, 40, false},
		{"v1 R3 code 23 = Inside Lift (lift movement) -> driving false", codeV1, 23, false},
		{"v1 R3 code 13 = Docked -> driving false", codeV1, 13, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: tc.version}
			frame := fmt.Sprintf(`{"operation_fb": "touchscreen_robot_status", "content": {"state": %d, "localised": true}}`, tc.code)
			got, err := tr.translate([]byte(frame))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if got.Driving == nil {
				t.Fatalf("Driving is nil, want %v", tc.wantDriving)
			}
			if *got.Driving != tc.wantDriving {
				t.Errorf("Driving = %v, want %v", *got.Driving, tc.wantDriving)
			}
		})
	}
}

func TestTranslate_OperatingMode(t *testing.T) {
	cases := []struct {
		name      string
		version   codeVersion
		code      int
		localized bool
		wantMode  state.OperatingMode
	}{
		{"Estop -> MANUAL", codeV1, 98, true, state.OperatingModeManual},
		{"Estop beats not-localized -> MANUAL", codeV1, 98, false, state.OperatingModeManual},
		{"Moving not localized -> SERVICE", codeV1, 2, false, state.OperatingModeService},
		{"Teleop not localized -> SERVICE (rule 2 before 3)", codeV1, 6, false, state.OperatingModeService},
		{"Teleoping localized -> MANUAL", codeV1, 6, true, state.OperatingModeManual},
		{"ManualDock -> MANUAL", codeV2, 18, true, state.OperatingModeManual},
		{"Mapping -> SERVICE", codeV1, 9, true, state.OperatingModeService},
		{"Updating -> SERVICE", codeV1, 1, true, state.OperatingModeService},
		{"Explore -> SERVICE", codeV2, 11, true, state.OperatingModeService},
		{"Moving localized -> AUTOMATIC", codeV1, 2, true, state.OperatingModeAutomatic},
		{"Docked localized -> AUTOMATIC", codeV2, 15, true, state.OperatingModeAutomatic},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: tc.version}
			frame := fmt.Sprintf(`{"operation_fb": "touchscreen_robot_status", "content": {"state": %d, "localised": %t}}`, tc.code, tc.localized)
			got, err := tr.translate([]byte(frame))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if got.OperatingMode == nil {
				t.Fatalf("OperatingMode is nil, want %v", tc.wantMode)
			}
			if *got.OperatingMode != tc.wantMode {
				t.Errorf("OperatingMode = %v, want %v", *got.OperatingMode, tc.wantMode)
			}
		})
	}
}

func TestTranslate_SafetyState(t *testing.T) {
	cases := []struct {
		name       string
		version    codeVersion
		code       int
		wantSafety state.SafetyState
	}{
		{"v1 R3 code 98 = Estop -> activeEmergencyStop MANUAL", codeV1, 98,
			state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual, FieldViolation: false}},
		{"v2 R5 code 98 = Estop -> activeEmergencyStop MANUAL", codeV2, 98,
			state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual, FieldViolation: false}},
		{"v0 LeoBot code 98 = Connecting -> NONE (no estop, documented gap)", codeV0, 98,
			state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false}},
		{"v1 R3 code 2 = Moving -> NONE", codeV1, 2,
			state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: tc.version}
			frame := fmt.Sprintf(`{"operation_fb": "touchscreen_robot_status", "content":  {"state": %d, "localised": true}}`, tc.code)
			got, err := tr.translate([]byte(frame))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if got.SafetyState == nil {
				t.Fatalf("SafetyState is nil, want %+v", tc.wantSafety)
			}
			if *got.SafetyState != tc.wantSafety {
				t.Errorf("SafetyState = %+v, want %+v", *got.SafetyState, tc.wantSafety)
			}
		})
	}
}

func TestTranslate_UnknownCodeLeavesStateFieldsNil(t *testing.T) {
	unknown := []struct {
		name    string
		version codeVersion
		code    int
	}{
		{"v1 unrecognized code", codeV1, 500},
		{"v0 gap code 14 (not in table)", codeV0, 14},
	}
	for _, tc := range unknown {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: tc.version}
			frame := fmt.Sprintf(`{"operation_fb": "touchscreen_robot_status", "content": {"state": %d, "localised": true, "battery_info": {"soc": 55}}}`, tc.code)
			got, err := tr.translate([]byte(frame))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if got.Driving != nil || got.OperatingMode != nil || got.SafetyState != nil {
				t.Errorf("unknown code must leave state fields nil (hold last known); got driving=%v mode=%v safety=%v",
					got.Driving, got.OperatingMode, got.SafetyState)
			}
			if got.PowerSupply == nil || got.PowerSupply.StateOfCharge != 55 {
				t.Errorf("powerSupply must still be reported for an unknown state code")
			}
		})
	}
}

func TestTranslate_UnknownStateHeldThroughStore(t *testing.T) {
	store := vehiclestate.New()
	tr := translator{version: codeV1}

	moving, _ := tr.translate([]byte(`{"operation_fb": "touchscreen_robot_status", "content":  {"state": 2, "localised": true}}`))
	store.SetTelemetry(moving)

	unknown, _ := tr.translate([]byte(`{"operation_fb": "touchscreen_robot_status", "content": {"state": 500, "localised": true}}`))
	store.SetTelemetry(unknown)

	got := store.Snapshot()
	if got.OperatingMode != state.OperatingModeAutomatic {
		t.Errorf("operatingMode = %v, want AUTOMATIC held (not fabricated)", got.OperatingMode)
	}
	if got.Driving != true {
		t.Errorf("driving = %v, want true held", got.Driving)
	}
}

func TestTranslate_TouchscreenStatusDecodesState(t *testing.T) {
	frame := `{
					"operation_fb": "touchscreen_robot_status",
					"robot_id": "R3-0001",
					"content": {
									"state": 2,
									"status": "Moving",
									"localised": true,
									"charging": false,
									"battery_info": { "soc": 77, "voltage": 28 }
					}
	}`
	tr := translator{version: codeV1}
	got, err := tr.translate([]byte(frame))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if got.Driving == nil || *got.Driving != true {
		t.Errorf("Driving = %v, want true (state 2 = Moving on v1)", got.Driving)
	}
	if got.OperatingMode == nil || *got.OperatingMode != state.OperatingModeAutomatic {
		t.Errorf("OperatingMode = %v, want AUTOMATIC (localised, Moving)", got.OperatingMode)
	}
	if got.PowerSupply == nil || got.PowerSupply.StateOfCharge != 77 {
		t.Errorf("powerSupply soc = %v, want 77", got.PowerSupply)
	}
}

func TestTranslate_PoseTransform(t *testing.T) {
	frame := `{
              "operation_fb": "robot_pose",
              "robot_id": "R3-0001",
              "content": { "x": 0, "y": 5402, "heading": 0 }
      }`
	tr := translator{
		version: codeV1,
		frame:   MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402},
	}
	got, err := tr.translate([]byte(frame))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if got.MobileRobotPosition == nil {
		t.Fatal("MobileRobotPosition = nil, want present")
	}
	want := state.MobileRobotPosition{X: -165.4, Y: -158.07, Theta: 0, MapID: "map-1", Localized: false}
	if *got.MobileRobotPosition != want {
		t.Errorf("MobileRobotPosition = %+v, want %+v", *got.MobileRobotPosition, want)
	}
}

func TestTranslate_RobotPath(t *testing.T) {
	const eps = 1e-9
	mapFrame := MapFrame{MapID: "map-1", Resolution: 0.1, OriginX: 10, OriginY: 20, Height: 1000}
	pathFrame := func(vals ...float64) string {
		b, _ := json.Marshal(map[string]any{
			"operation_fb": "robot_path", "robot_id": "robot-001",
			"content": map[string]any{"path": vals},
		})
		return string(b)
	}

	near := func(a, b float64) bool { return math.Abs(a-b) < eps }

	t.Run("two-point path transforms to metres as a clamped degree-1 NURBS", func(t *testing.T) {
		tr := translator{version: codeV1, frame: mapFrame}
		got, err := tr.translate([]byte(pathFrame(562, 842, 562, 843)))
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		p := got.PlannedPath
		if p == nil {
			t.Fatal("PlannedPath = nil, want present")
		}
		cps := p.Trajectory.ControlPoints
		if len(cps) != 2 ||
			!near(cps[0].X, 66.2) || !near(cps[0].Y, 35.8) ||
			!near(cps[1].X, 66.2) || !near(cps[1].Y, 35.7) {
			t.Errorf("controlPoints = %+v, want [(66.2,35.8) (66.2,35.7)]", cps)
		}
		if p.Trajectory.Degree == nil || *p.Trajectory.Degree != 1 {
			t.Errorf("degree = %v, want 1", p.Trajectory.Degree)
		}
		if !reflect.DeepEqual(p.Trajectory.KnotVector, []float64{0, 0, 1, 1}) {
			t.Errorf("knotVector = %v, want [0 0 1 1]", p.Trajectory.KnotVector)
		}
	})

	t.Run("three-point path gets a uniform interior knot", func(t *testing.T) {
		tr := translator{version: codeV1, frame: mapFrame}
		got, err := tr.translate([]byte(pathFrame(0, 1000, 100, 1000, 200, 1000)))
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		if got.PlannedPath == nil {
			t.Fatal("PlannedPath = nil, want present")
		}
		if kv := got.PlannedPath.Trajectory.KnotVector; !reflect.DeepEqual(kv, []float64{0, 0, 0.5, 1, 1}) {
			t.Errorf("knotVector = %v, want [0 0 0.5 1 1]", kv)
		}
	})

	t.Run("no usable map frame omits the path", func(t *testing.T) {
		tr := translator{version: codeV1}
		got, err := tr.translate([]byte(pathFrame(562, 842, 562, 843)))
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		if got.PlannedPath != nil {
			t.Errorf("PlannedPath = %+v, want nil without a frame (never emit pixels)", got.PlannedPath)
		}
	})

	t.Run("malformed paths are omitted", func(t *testing.T) {
		for _, vals := range [][]float64{{}, {562, 842}, {562, 842, 563}} {
			tr := translator{version: codeV1, frame: mapFrame}
			got, err := tr.translate([]byte(pathFrame(vals...)))
			if err != nil {
				t.Fatalf("translate(%v): %v", vals, err)
			}
			if got.PlannedPath != nil {
				t.Errorf("PlannedPath(%v) = %+v, want nil", vals, got.PlannedPath)
			}
		}
	})

	t.Run("path held across other frames, replaced by the next robot_path", func(t *testing.T) {
		tr := translator{version: codeV1, frame: mapFrame}
		if _, err := tr.translate([]byte(pathFrame(562, 842, 562, 843))); err != nil {
			t.Fatalf("translate path: %v", err)
		}
		got, err := tr.translate([]byte(`{"operation_fb":"robot_pose","robot_id":"robot-001","content":{"x":0,"y":1000,"heading":0}}`))
		if err != nil {
			t.Fatalf("translate pose: %v", err)
		}
		if got.PlannedPath == nil {
			t.Fatal("PlannedPath dropped by an unrelated frame; must be held")
		}

		got, err = tr.translate([]byte(pathFrame(0, 1000, 100, 1000)))
		if err != nil {
			t.Fatalf("translate second path: %v", err)
		}
		if cps := got.PlannedPath.Trajectory.ControlPoints; len(cps) != 2 || !near(cps[0].X, 10) {
			t.Errorf("controlPoints = %+v, want the replacement path [(10,20) (20,20)]", cps)
		}
	})
}

func TestTranslate_PoseTransformFrames(t *testing.T) {
	const eps = 1e-6
	mapFrame := MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402}
	cases := []struct {
		name         string
		x, y         float64
		wantX, wantY float64
	}{
		{"origin corner: pixel (0,5402) -> world origin metres", 0, 5402, -165.4, -158.07},
		{"world (0,0) is pixel (3308,2240.6)", 3308, 2240.6, 0, 0},
		{"captured real pose (px) -> plausible metres", 3136.5, 2311.9, -8.575, -3.565},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: codeV1, frame: mapFrame}
			body := fmt.Sprintf(`{"operation_fb":"robot_pose","content":{"x":%v,"y":%v,"heading":0}}`, tc.x, tc.y)
			got, err := tr.translate([]byte(body))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if got.MobileRobotPosition == nil {
				t.Fatal("MobileRobotPosition = nil")
			}
			p := got.MobileRobotPosition
			if math.Abs(p.X-tc.wantX) > eps || math.Abs(p.Y-tc.wantY) > eps {
				t.Errorf("(X,Y) = (%v,%v), want (%v,%v)", p.X, p.Y, tc.wantX, tc.wantY)
			}
		})
	}
}

func TestTranslate_NoMapFrameOmitsPosition(t *testing.T) {
	pose := `{"operation_fb":"robot_pose","content":{"x":3136.5,"y":2311.9,"heading":90}}`

	tr := translator{version: codeV1} // no usable frame yet
	got, err := tr.translate([]byte(pose))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if got.MobileRobotPosition != nil {
		t.Errorf("MobileRobotPosition = %+v, want nil (no frame -> omit, never 0,0)", *got.MobileRobotPosition)
	}

	// The held pose surfaces once a usable frame exists: the guard must read the
	// frame per emission, not discard the pose on arrival.
	tr.frame = MapFrame{MapID: "map-1", Resolution: 0.05, OriginX: -165.4, OriginY: -158.07, Height: 5402}
	got, err = tr.translate([]byte(`{"operation_fb":"docking_station_status","content":{}}`))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if got.MobileRobotPosition == nil {
		t.Fatal("MobileRobotPosition = nil after frame became usable, want held pose emitted")
	}
	const eps = 1e-6
	if x := got.MobileRobotPosition.X; math.Abs(x-(-8.575)) > eps {
		t.Errorf("X = %v, want -8.575", x)
	}
}

func TestTranslate_PoseHeadingToTheta(t *testing.T) {
	const eps = 1e-9
	// heading is already the world-frame CCW angle in degrees (validated against
	// a live robot's internal pose); theta converts units and adds the offset.
	cases := []struct {
		name      string
		heading   float64
		offsetDeg float64
		wantTheta float64
	}{
		{"heading 0 -> 0", 0, 0, 0},
		{"heading 90 -> +pi/2 (no negation)", 90, 0, math.Pi / 2},
		{"heading 270 wraps to -pi/2", 270, 0, -math.Pi / 2},
		{"offset applied after conversion: 90 with -90 -> 0", 90, -90, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: codeV1, frame: MapFrame{MapID: "m", Resolution: 0.05, ThetaOffsetDeg: tc.offsetDeg}}
			body := fmt.Sprintf(`{"operation_fb":"robot_pose","content":{"x":0,"y":0,"heading":%v}}`, tc.heading)
			got, err := tr.translate([]byte(body))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if got.MobileRobotPosition == nil {
				t.Fatal("MobileRobotPosition = nil")
			}
			if math.Abs(got.MobileRobotPosition.Theta-tc.wantTheta) > eps {
				t.Errorf("Theta = %v, want %v", got.MobileRobotPosition.Theta, tc.wantTheta)
			}
		})
	}
}

func TestTranslate_Aggregation(t *testing.T) {
	const soc = 77
	statusMoving := `{"operation_fb":"touchscreen_robot_status","content":{"state":2,"status":"Moving","localised":true,"charging":false,"battery_info":{"soc":77}}}`
	statusNotLoc := `{"operation_fb":"touchscreen_robot_status","content":{"state":2,"localised":false,"charging":false,"battery_info":{"soc":77}}}`
	pose := `{"operation_fb":"robot_pose","content":{"x":100,"y":200,"heading":0}}`
	docking := `{"operation_fb":"docking_station_status","content":{"docked":false}}`

	cases := []struct {
		name   string
		frames []string
		check  func(t *testing.T, got vehiclestate.Telemetry)
	}{
		{
			name:   "status then pose: pose frame keeps status-derived fields",
			frames: []string{statusMoving, pose},
			check: func(t *testing.T, got vehiclestate.Telemetry) {
				if got.MobileRobotPosition == nil {
					t.Fatal("position missing after pose")
				}
				if got.PowerSupply == nil || got.PowerSupply.StateOfCharge != soc {
					t.Errorf("powerSupply not retained on pose frame: %+v", got.PowerSupply)
				}
				if got.Driving == nil || *got.Driving != true {
					t.Errorf("driving not retained on pose frame: %v", got.Driving)
				}
			},
		},
		{
			name:   "pose then status: status frame keeps position",
			frames: []string{pose, statusMoving},
			check: func(t *testing.T, got vehiclestate.Telemetry) {
				if got.PowerSupply == nil || got.PowerSupply.StateOfCharge != soc {
					t.Fatal("powerSupply missing on status frame")
				}
				if got.MobileRobotPosition == nil {
					t.Errorf("position not retained on status frame")
				}
			},
		},
		{
			name:   "localised:false crosses from status into position",
			frames: []string{statusNotLoc, pose},
			check: func(t *testing.T, got vehiclestate.Telemetry) {
				if got.MobileRobotPosition == nil {
					t.Fatal("position missing")
				}
				if got.MobileRobotPosition.Localized != false {
					t.Errorf("position.Localized = true, want false (from held status)")
				}
			},
		},
		{
			name:   "localised:true crosses from status into position",
			frames: []string{statusMoving, pose},
			check: func(t *testing.T, got vehiclestate.Telemetry) {
				if got.MobileRobotPosition == nil {
					t.Fatal("position missing")
				}
				if got.MobileRobotPosition.Localized != true {
					t.Errorf("position.Localized = false, want true (from held status)")
				}
			},
		},
		{
			name:   "ignored frame re-emits held snapshot without clobbering",
			frames: []string{statusMoving, pose, docking},
			check: func(t *testing.T, got vehiclestate.Telemetry) {
				if got.MobileRobotPosition == nil {
					t.Errorf("ignored frame cleared position")
				}
				if got.PowerSupply == nil || got.PowerSupply.StateOfCharge != soc {
					t.Errorf("ignored frame cleared powerSupply")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := translator{version: codeV1, frame: MapFrame{MapID: "m", Resolution: 0.05}}
			var got vehiclestate.Telemetry
			for _, fr := range tc.frames {
				var err error
				got, err = tr.translate([]byte(fr))
				if err != nil {
					t.Fatalf("translate: %v", err)
				}
			}
			tc.check(t, got)
		})
	}
}

func hasInfoGroup(info []state.Info, infoType string) bool {
	for _, g := range info {
		if g.InfoType == infoType {
			return true
		}
	}
	return false
}

func findInfoRef(info []state.Info, infoType, key string) (string, bool) {
	for _, group := range info {
		if group.InfoType != infoType {
			continue
		}
		for _, ref := range group.InfoReferences {
			if ref.ReferenceKey == key {
				return ref.ReferenceValue, true
			}
		}
	}
	return "", false
}

func TestTranslate_InformationTelemetryGroup(t *testing.T) {
	frame := `{
		"operation_fb": "touchscreen_robot_status",
		"content": { "state": 13, "localised": true, "water_level": 50, "battery_info": { "soc": 100 } }
	}`
	tr := translator{version: codeV1}
	got, err := tr.translate([]byte(frame))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	ref, ok := findInfoRef(got.Information, "lionsbot.telemetry", "waterLevel")
	if !ok {
		t.Fatalf("no waterLevel ref in lionsbot.telemetry group; information=%+v", got.Information)
	}
	if ref != "50" {
		t.Errorf("waterLevel = %q, want \"50\"", ref)
	}
}

func TestTranslate_InformationAllGroups(t *testing.T) {
	frame := `{
		"operation_fb": "touchscreen_robot_status",
		"content": {
			"state": 13, "status": "Docked", "localised": true,
			"handlebarStatus": false, "currentZone": "zone-A",
			"water_level": 50, "recovery_water_level": 50,
			"missionProgress": 99, "timeToComplete": 0,
			"battery_info": { "soc": 100 }
		}
	}`
	tr := translator{version: codeV1}
	got, err := tr.translate([]byte(frame))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	want := []struct{ infoType, key, val string }{
		{"lionsbot.telemetry", "waterLevel", "50"},
		{"lionsbot.telemetry", "recoveryWaterLevel", "50"},
		{"lionsbot.telemetry", "currentZone", "zone-A"},
		{"lionsbot.diagnostics", "handlebarStatus", "false"},
		{"lionsbot.diagnostics", "localised", "true"},
		{"lionsbot.diagnostics", "state", "13"},
		{"lionsbot.diagnostics", "status", "Docked"},
		{"lionsbot.mission", "missionProgress", "99"},
		{"lionsbot.mission", "timeToComplete", "0"},
	}
	for _, w := range want {
		v, ok := findInfoRef(got.Information, w.infoType, w.key)
		if !ok {
			t.Errorf("%s/%s missing", w.infoType, w.key)
			continue
		}
		if v != w.val {
			t.Errorf("%s/%s = %q, want %q", w.infoType, w.key, v, w.val)
		}
	}
}

func TestTranslate_InformationOmitsAbsentAndEmptyGroups(t *testing.T) {
	// only state + battery present: no water/recovery/zone/mission/status/handlebar
	frame := `{"operation_fb":"touchscreen_robot_status","content":{"state":2,"localised":true,"battery_info":{"soc":50}}}`
	tr := translator{version: codeV1}
	got, err := tr.translate([]byte(frame))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if hasInfoGroup(got.Information, "lionsbot.telemetry") {
		t.Errorf("lionsbot.telemetry group present, want omitted (no telemetry extras)")
	}
	if hasInfoGroup(got.Information, "lionsbot.mission") {
		t.Errorf("lionsbot.mission group present, want omitted (no mission extras)")
	}
	if _, ok := findInfoRef(got.Information, "lionsbot.diagnostics", "status"); ok {
		t.Errorf("empty status must be omitted from diagnostics references")
	}
	if v, ok := findInfoRef(got.Information, "lionsbot.diagnostics", "localised"); !ok || v != "true" {
		t.Errorf("diagnostics.localised = %q ok=%v, want \"true\" (always emitted)", v, ok)
	}
}
