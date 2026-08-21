package masterbus

import (
	"log/slog"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/factsheet"
	vdatime "github.com/lionsbot-official/vda5050-adapter/foundation/time"
)

// publishFactsheet publishes the robot's factsheet (retained, QoS 0) on the
// factsheet topic. Called on every (re)connect and on factsheetRequest.
func (b *Bus) publishFactsheet() {
	if err := b.transport.Publish(b.factsheetTopic, b.buildFactsheet(), qosFactsheet, true); err != nil {
		slog.Error("bus: failed to publish factsheet", "err", err)
	}
}

// buildFactsheet assembles the pinned factsheet. Header fields come from config
// like every other message; the capability body is fixed for the robot series.
// maximumAcceleration/maximumDeceleration are estimates pending motion-controller
// confirmation.
func (b *Bus) buildFactsheet() factsheet.Factsheet {
	return factsheet.Factsheet{
		Header: common.Header{
			HeaderID:     b.headers.next(b.factsheetTopic),
			Timestamp:    vdatime.Now(),
			Version:      b.version,
			Manufacturer: b.manufacturer,
			SerialNumber: b.serialNumber,
		},
		TypeSpecification: factsheet.TypeSpecification{
			SeriesName:            "R3",
			MobileRobotKinematics: factsheet.KinematicsDifferential,
			MobileRobotClass:      factsheet.ClassCleaning,
			MaximumLoadMass:       0,
			LocalizationTypes:     []factsheet.LocalizationType{factsheet.LocalizationNatural},
			NavigationTypes:       []factsheet.NavigationType{factsheet.NavigationFreelyNavigating},
			SupportedZones:        []factsheet.ZoneType{},
		},
		PhysicalParameters: factsheet.PhysicalParameters{
			MinimumSpeed:        0.1,
			MaximumSpeed:        1.0,
			MaximumAcceleration: 0.8,
			MaximumDeceleration: 0.8,
			MinimumHeight:       0.81,
			MaximumHeight:       0.81,
			Width:               0.45,
			Length:              0.60,
		},
		ProtocolLimits: factsheet.ProtocolLimits{
			Timing: factsheet.Timing{
				MinimumOrderInterval: 0,
				MinimumStateInterval: 0,
				DefaultStateInterval: 1.0,
			},
		},
		ProtocolFeatures: factsheet.ProtocolFeatures{
			OptionalParameters: []factsheet.OptionalParameter{},
			MobileRobotActions: mobileRobotActions(),
		},
	}
}

// mobileRobotActions is the advertised action vocabulary; a test guards it
// against drifting from the supported-action registry.
func mobileRobotActions() []factsheet.MobileRobotAction {
	instant := func(t string) factsheet.MobileRobotAction {
		return factsheet.MobileRobotAction{
			ActionType:    t,
			ActionScopes:  []factsheet.ActionScope{factsheet.ScopeInstant},
			PauseAllowed:  false,
			CancelAllowed: false,
		}
	}
	return []factsheet.MobileRobotAction{
		{
			ActionType:    "startCleaning",
			ActionScopes:  []factsheet.ActionScope{factsheet.ScopeNode},
			PauseAllowed:  true,
			CancelAllowed: true,
			BlockingTypes: []factsheet.BlockingType{factsheet.BlockingHard},
			ActionParameters: []factsheet.ActionParameter{
				{Key: "zones", ValueDataType: factsheet.DataTypeArray},
				{Key: "bypassBlackout", ValueDataType: factsheet.DataTypeBool},
			},
		},
		// Dock: instant + node-scoped, HARD-blocking. pause/cancel land in a
		// later slice as dock_pause/dock_stop are wired.
		{
			ActionType:    "startCharging",
			ActionScopes:  []factsheet.ActionScope{factsheet.ScopeInstant, factsheet.ScopeNode},
			PauseAllowed:  true,
			CancelAllowed: true,
			BlockingTypes: []factsheet.BlockingType{factsheet.BlockingHard},
		},
		// Undock: instant + node-scoped; not controllable once started (the robot
		// has no undock-pause/stop).
		{
			ActionType:    "stopCharging",
			ActionScopes:  []factsheet.ActionScope{factsheet.ScopeInstant, factsheet.ScopeNode},
			PauseAllowed:  false,
			CancelAllowed: false,
		},
		instant("cancelOrder"),
		instant("startPause"),
		instant("stopPause"),
		instant("factsheetRequest"),
		instant("initializePosition"),
	}
}
