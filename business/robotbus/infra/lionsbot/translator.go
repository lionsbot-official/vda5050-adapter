package lionsbot

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

type translator struct {
	version    codeVersion
	frame      MapFrame
	lastStatus *statusFrame
	lastPose   *poseFrame
	lastPath   []float64 // latest robot_path pixel pairs; the robot's live re-plan
}

type statusFrame struct {
	Content struct {
		Charging           bool     `json:"charging"`
		State              *int     `json:"state"`
		Localized          bool     `json:"localised"`
		HandlebarStatus    bool     `json:"handlebarStatus"`
		Status             string   `json:"status"`
		CurrentZone        string   `json:"currentZone"`
		WaterLevel         *float64 `json:"water_level"`
		RecoveryWaterLevel *float64 `json:"recovery_water_level"`
		MissionProgress    *float64 `json:"missionProgress"`
		TimeToComplete     *float64 `json:"timeToComplete"`
		BatteryInfo        struct {
			SOC     float64  `json:"soc"`
			Voltage *float64 `json:"voltage"`
		} `json:"battery_info"`
	} `json:"content"`
}

var drivingStates = map[canonicalState]bool{
	stateMoving:            true,
	stateCleaning:          true,
	stateTeleop:            true,
	stateTeleopClean:       true,
	stateDocking:           true,
	stateDockingMove:       true,
	stateDockingInProgress: true,
	stateUndocking:         true,
	stateExplore:           true,
	stateGantryTraverse:    true,
	stateEnteringLift:      true,
	stateExitingLift:       true,
}

const (
	feedTouchscreenStatus = "touchscreen_robot_status"
	feedPose              = "robot_pose"
	feedPath              = "robot_path"
)

func (tr *translator) translate(frame []byte) (vehiclestate.Telemetry, error) {
	var env struct {
		OperationFb string `json:"operation_fb"`
	}
	if err := json.Unmarshal(frame, &env); err != nil {
		return vehiclestate.Telemetry{}, err
	}

	switch env.OperationFb {
	case feedTouchscreenStatus:
		var f statusFrame
		if err := json.Unmarshal(frame, &f); err != nil {
			return vehiclestate.Telemetry{}, err
		}
		tr.lastStatus = &f
	case feedPose:
		var f poseFrame
		if err := json.Unmarshal(frame, &f); err != nil {
			return vehiclestate.Telemetry{}, err
		}
		tr.lastPose = &f
	case feedPath:
		var f pathFrame
		if err := json.Unmarshal(frame, &f); err != nil {
			return vehiclestate.Telemetry{}, err
		}
		tr.lastPath = f.Content.Path
	default:
		// ignored feed (robot_status, docking_station_status, sensor_status, …):
		// no state change, but re-emit the held snapshot so it doesn't clobber last-known.
	}

	return tr.snapshot(), nil
}

func (tr *translator) snapshot() vehiclestate.Telemetry {
	var t vehiclestate.Telemetry

	if s := tr.lastStatus; s != nil {
		t.PowerSupply = &state.PowerSupply{
			StateOfCharge:  s.Content.BatteryInfo.SOC,
			Charging:       s.Content.Charging,
			BatteryVoltage: s.Content.BatteryInfo.Voltage,
		}
		if s.Content.State != nil {
			if canonical, ok := decodeState(tr.version, *s.Content.State); ok {
				driving := drivingStates[canonical]
				t.Driving = &driving
				mode := operatingMode(canonical)
				t.OperatingMode = &mode
				safety := safetyState(canonical)
				t.SafetyState = &safety
			}
		}
	}

	// No usable frame -> omit position rather than emit untransformed coordinates.
	if p := tr.lastPose; p != nil && tr.frame.Resolution > 0 {
		localized := false
		if tr.lastStatus != nil {
			localized = tr.lastStatus.Content.Localized
		}
		xm := tr.frame.OriginX + p.Content.X*tr.frame.Resolution
		ym := tr.frame.OriginY + (tr.frame.Height-p.Content.Y)*tr.frame.Resolution
		theta := normalizeAngle((p.Content.Heading + tr.frame.ThetaOffsetDeg) * math.Pi / 180)
		t.MobileRobotPosition = &state.MobileRobotPosition{
			X:         xm,
			Y:         ym,
			Theta:     theta,
			MapID:     tr.frame.MapID,
			Localized: localized,
		}
	}

	t.PlannedPath = tr.plannedPath()

	if s := tr.lastStatus; s != nil {
		c := s.Content

		telemetry := []state.InfoReference{}
		if c.WaterLevel != nil {
			telemetry = append(telemetry, state.InfoReference{ReferenceKey: "waterLevel", ReferenceValue: numStr(*c.WaterLevel)})
		}
		if c.RecoveryWaterLevel != nil {
			telemetry = append(telemetry, state.InfoReference{ReferenceKey: "recoveryWaterLevel", ReferenceValue: numStr(*c.RecoveryWaterLevel)})
		}
		if c.CurrentZone != "" {
			telemetry = append(telemetry, state.InfoReference{ReferenceKey: "currentZone", ReferenceValue: c.CurrentZone})
		}

		diagnostics := []state.InfoReference{
			{ReferenceKey: "handlebarStatus", ReferenceValue: boolStr(c.HandlebarStatus)},
			{ReferenceKey: "localised", ReferenceValue: boolStr(c.Localized)},
		}
		if c.State != nil {
			diagnostics = append(diagnostics, state.InfoReference{ReferenceKey: "state", ReferenceValue: strconv.Itoa(*c.State)})
		}
		if c.Status != "" {
			diagnostics = append(diagnostics, state.InfoReference{ReferenceKey: "status", ReferenceValue: c.Status})
		}

		mission := []state.InfoReference{}
		if c.MissionProgress != nil {
			mission = append(mission, state.InfoReference{ReferenceKey: "missionProgress", ReferenceValue: numStr(*c.MissionProgress)})
		}
		if c.TimeToComplete != nil {
			mission = append(mission, state.InfoReference{ReferenceKey: "timeToComplete", ReferenceValue: numStr(*c.TimeToComplete)})
		}

		for _, g := range []struct {
			infoType string
			refs     []state.InfoReference
		}{
			{"lionsbot.telemetry", telemetry},
			{"lionsbot.diagnostics", diagnostics},
			{"lionsbot.mission", mission},
		} {
			if len(g.refs) > 0 {
				t.Information = append(t.Information, state.Info{
					InfoType:       g.infoType,
					InfoLevel:      state.InfoLevelInfo,
					InfoReferences: g.refs,
				})
			}
		}
	}

	return t
}

func numStr(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func operatingMode(s canonicalState) state.OperatingMode {
	switch {
	case s == stateEstop:
		return state.OperatingModeManual
	case s == stateTeleop || s == stateTeleopClean || s == stateManualDock:
		return state.OperatingModeManual
	case s == stateMapping || s == stateExpandMap || s == stateExplore ||
		s == stateMaintenance || s == stateCalibration || s == stateUpdating:
		return state.OperatingModeService
	default:
		return state.OperatingModeAutomatic
	}
}

func safetyState(s canonicalState) state.SafetyState {
	if s == stateEstop {
		return state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual, FieldViolation: false}
	}
	return state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false}
}

type MapFrame struct {
	MapID          string
	Resolution     float64 // m/px
	OriginX        float64 // metres
	OriginY        float64 // metres
	Height         float64 // image height, px (for the y-flip)
	ThetaOffsetDeg float64 // heading->theta offset; unvalidated
}

type poseFrame struct {
	Content struct {
		X       float64 `json:"x"`
		Y       float64 `json:"y"`
		Heading float64 `json:"heading"`
	} `json:"content"`
}

type pathFrame struct {
	Content struct {
		Path []float64 `json:"path"` // flat pixel pairs: x1 y1 x2 y2 …
	} `json:"content"`
}

// plannedPath converts the held robot_path pixels to a world-metre degree-1
// clamped NURBS (the VDA plannedPath encoding). Malformed paths or a missing
// map frame yield nil — never untransformed pixels.
func (tr *translator) plannedPath() *state.PlannedPath {
	px := tr.lastPath
	if len(px) < 4 || len(px)%2 != 0 || tr.frame.Resolution <= 0 {
		return nil
	}
	n := len(px) / 2
	cps := make([]common.ControlPoint, 0, n)
	for i := 0; i < n; i++ {
		cps = append(cps, common.ControlPoint{
			X: tr.frame.OriginX + px[2*i]*tr.frame.Resolution,
			Y: tr.frame.OriginY + (tr.frame.Height-px[2*i+1])*tr.frame.Resolution,
		})
	}
	degree := 1
	// clamped uniform knots: ends at multiplicity degree+1, interior equidistant
	knots := make([]float64, 0, n+2)
	knots = append(knots, 0, 0)
	for i := 1; i < n-1; i++ {
		knots = append(knots, float64(i)/float64(n-1))
	}
	knots = append(knots, 1, 1)
	return &state.PlannedPath{Trajectory: common.Trajectory{
		ControlPoints: cps,
		Degree:        &degree,
		KnotVector:    knots,
	}}
}

func normalizeAngle(theta float64) float64 {
	const twoPi = 2 * math.Pi
	w := math.Mod(theta+math.Pi, twoPi)
	if w < 0 {
		w += twoPi // Go's math.Mod keeps the dividend's sign, unlike Python's %; correct it
	}
	return w - math.Pi
}
