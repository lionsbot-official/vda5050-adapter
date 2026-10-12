package lionsbot

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

type translator struct {
	version    codeVersion
	frame      MapFrame
	lastStatus *statusFrame
	lastPose   *poseFrame
}

type statusFrame struct {
	Content struct {
		Charging            bool     `json:"charging"`
		State               *int     `json:"state"`
		PositionInitialized bool     `json:"localised"`
		HandlebarStatus     bool     `json:"handlebarStatus"`
		Status              string   `json:"status"`
		CurrentZone         string   `json:"currentZone"`
		WaterLevel          *float64 `json:"water_level"`
		RecoveryWaterLevel  *float64 `json:"recovery_water_level"`
		MissionProgress     *float64 `json:"missionProgress"`
		TimeToComplete      *float64 `json:"timeToComplete"`
		BatteryInfo         struct {
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
	default:
		// ignored feed (robot_status, docking_station_status, sensor_status, …):
		// no state change, but re-emit the held snapshot so it doesn't clobber last-known.
	}

	return tr.snapshot(), nil
}

func (tr *translator) snapshot() vehiclestate.Telemetry {
	var t vehiclestate.Telemetry

	if s := tr.lastStatus; s != nil {
		t.BatteryState = &state.BatteryState{
			BatteryCharge:  s.Content.BatteryInfo.SOC,
			Charging:       s.Content.Charging,
			BatteryVoltage: s.Content.BatteryInfo.Voltage,
		}
		if s.Content.State != nil {
			if canonical, ok := decodeState(tr.version, *s.Content.State); ok {
				driving := drivingStates[canonical]
				t.Driving = &driving
				mode := operatingMode(canonical, s.Content.PositionInitialized)
				t.OperatingMode = &mode
				safety := safetyState(canonical)
				t.SafetyState = &safety
			}
		}
	}

	// No usable frame -> omit position rather than emit untransformed coordinates.
	if p := tr.lastPose; p != nil && tr.frame.Resolution > 0 {
		positionInitialized := false
		if tr.lastStatus != nil {
			positionInitialized = tr.lastStatus.Content.PositionInitialized
		}
		xm := tr.frame.OriginX + p.Content.X*tr.frame.Resolution
		ym := tr.frame.OriginY + (tr.frame.Height-p.Content.Y)*tr.frame.Resolution
		theta := normalizeAngle((p.Content.Heading + tr.frame.ThetaOffsetDeg) * math.Pi / 180)
		t.AGVPosition = &state.AGVPosition{
			X:                   xm,
			Y:                   ym,
			Theta:               theta,
			MapID:               tr.frame.MapID,
			PositionInitialized: positionInitialized,
		}
	}

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
			{ReferenceKey: "localised", ReferenceValue: boolStr(c.PositionInitialized)},
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

func operatingMode(s canonicalState, positionInitialized bool) state.OperatingMode {
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
		return state.SafetyState{EStop: state.EStopManual, FieldViolation: false}
	}
	return state.SafetyState{EStop: state.EStopNone, FieldViolation: false}
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

func normalizeAngle(theta float64) float64 {
	const twoPi = 2 * math.Pi
	w := math.Mod(theta+math.Pi, twoPi)
	if w < 0 {
		w += twoPi // Go's math.Mod keeps the dividend's sign, unlike Python's %; correct it
	}
	return w - math.Pi
}
