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
// accelerationMax/decelerationMax are estimates pending motion-controller
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
			SeriesName:        "R3",
			AGVKinematic:      factsheet.KinematicsDifferential,
			AGVClass:          factsheet.ClassCarrier,
			MaxLoadMass:       0,
			LocalizationTypes: []factsheet.LocalizationType{factsheet.LocalizationNatural},
			NavigationTypes:   []factsheet.NavigationType{factsheet.NavigationFreelyNavigating},
		},
		PhysicalParameters: factsheet.PhysicalParameters{
			SpeedMin:        0.1,
			SpeedMax:        1.0,
			AccelerationMax: 0.8,
			DecelerationMax: 0.8,
			HeightMin:       0.81,
			HeightMax:       0.81,
			Width:           0.45,
			Length:          0.60,
		},
		ProtocolLimits: factsheet.ProtocolLimits{
			Timing: factsheet.Timing{
				MinOrderInterval:     0,
				MinStateInterval:     0,
				DefaultStateInterval: 1.0,
			},
		},
		ProtocolFeatures: factsheet.ProtocolFeatures{
			OptionalParameters: []factsheet.OptionalParameter{
				{Parameter: "order.nodes.nodePosition", Support: factsheet.SupportRequired, Description: "Required for navigation nodes (sequenceId > 0); optional at the initial node"},
				{Parameter: "order.nodes.nodePosition.theta", Support: factsheet.SupportSupported},
				{Parameter: "order.nodes.nodePosition.allowedDeviationXY", Support: factsheet.SupportSupported},
				{Parameter: "order.nodes.nodePosition.mapDescription", Support: factsheet.SupportSupported},
				{Parameter: "order.nodes.nodeDescription", Support: factsheet.SupportSupported},
				{Parameter: "order.edges.edgeDescription", Support: factsheet.SupportSupported},
				{Parameter: "order.nodes.actions.actionDescription", Support: factsheet.SupportSupported},
				{Parameter: "order.nodes.actions.actionParameters", Support: factsheet.SupportSupported},
				{Parameter: "instantActions.actions.actionDescription", Support: factsheet.SupportSupported},
				{Parameter: "instantActions.actions.actionParameters", Support: factsheet.SupportSupported},
			},
			AGVActions: agvActions(),
		},
	}
}

// agvActions is the advertised action vocabulary; a test guards it
// against drifting from the supported-action registry.
func agvActions() []factsheet.AGVAction {
	instant := func(t string) factsheet.AGVAction {
		return factsheet.AGVAction{
			ActionType:   t,
			ActionScopes: []factsheet.ActionScope{factsheet.ScopeInstant},
		}
	}
	return []factsheet.AGVAction{
		{
			ActionType:        "startCleaning",
			ActionDescription: "Single-node, single-action order",
			ActionScopes:      []factsheet.ActionScope{factsheet.ScopeNode},
			ActionParameters: []factsheet.ActionParameter{
				{Key: "zones", ValueDataType: factsheet.DataTypeArray},
				{Key: "homePoi", ValueDataType: factsheet.DataTypeObject, IsOptional: new(true), Description: "Legacy compatibility only; ignored, robot remains in place after cleaning"},
				{Key: "bypassBlackout", ValueDataType: factsheet.DataTypeBool, IsOptional: new(true)},
			},
		},
		// Dock: instant + node-scoped, HARD-blocking. pause/cancel land in a
		// later slice as dock_pause/dock_stop are wired.
		{
			ActionType:        "startCharging",
			ActionDescription: "Instant action or single-node, single-action order",
			ActionScopes:      []factsheet.ActionScope{factsheet.ScopeInstant, factsheet.ScopeNode},
		},
		// Undock: instant + node-scoped; not controllable once started (the robot
		// has no undock-pause/stop).
		{
			ActionType:   "stopCharging",
			ActionScopes: []factsheet.ActionScope{factsheet.ScopeInstant, factsheet.ScopeNode},
		},
		instant("cancelOrder"),
		instant("startPause"),
		instant("stopPause"),
		instant("factsheetRequest"),
		{
			ActionType:   "initPosition",
			ActionScopes: []factsheet.ActionScope{factsheet.ScopeInstant},
			ActionParameters: []factsheet.ActionParameter{
				{Key: "x", ValueDataType: factsheet.DataTypeNumber},
				{Key: "y", ValueDataType: factsheet.DataTypeNumber},
				{Key: "theta", ValueDataType: factsheet.DataTypeNumber},
				{Key: "mapId", ValueDataType: factsheet.DataTypeString},
				{Key: "lastNodeId", ValueDataType: factsheet.DataTypeString},
			},
		},
	}
}
