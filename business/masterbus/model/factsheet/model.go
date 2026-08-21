// Package factsheet is the pure-data VDA5050 factsheet message (spec §7.10):
// machine-readable capabilities of the robot type series. Structs + enums only.
package factsheet

import "github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"

type Factsheet struct {
	common.Header
	TypeSpecification        TypeSpecification         `json:"typeSpecification"`
	PhysicalParameters       PhysicalParameters        `json:"physicalParameters"`
	ProtocolLimits           ProtocolLimits            `json:"protocolLimits"`
	ProtocolFeatures         ProtocolFeatures          `json:"protocolFeatures"`
	MobileRobotGeometry      MobileRobotGeometry       `json:"mobileRobotGeometry"`
	LoadSpecification        LoadSpecification         `json:"loadSpecification"`
	MobileRobotConfiguration *MobileRobotConfiguration `json:"mobileRobotConfiguration,omitempty"`
}

type TypeSpecification struct {
	SeriesName            string             `json:"seriesName"`
	SeriesDescription     string             `json:"seriesDescription,omitempty"`
	MobileRobotKinematics Kinematics         `json:"mobileRobotKinematics"`
	MobileRobotClass      Class              `json:"mobileRobotClass"`
	MaximumLoadMass       float64            `json:"maximumLoadMass"`
	LocalizationTypes     []LocalizationType `json:"localizationTypes"`
	NavigationTypes       []NavigationType   `json:"navigationTypes"`
	// SupportedZones is optional in the schema but emitted always (as [] when
	// none) so a master reads an explicit "no zone types understood".
	SupportedZones []ZoneType `json:"supportedZones"`
}

type PhysicalParameters struct {
	MinimumSpeed        float64  `json:"minimumSpeed"`
	MaximumSpeed        float64  `json:"maximumSpeed"`
	MinimumAngularSpeed *float64 `json:"minimumAngularSpeed,omitempty"`
	MaximumAngularSpeed *float64 `json:"maximumAngularSpeed,omitempty"`
	MaximumAcceleration float64  `json:"maximumAcceleration"`
	MaximumDeceleration float64  `json:"maximumDeceleration"`
	MinimumHeight       float64  `json:"minimumHeight"`
	MaximumHeight       float64  `json:"maximumHeight"`
	Width               float64  `json:"width"`
	Length              float64  `json:"length"`
}

type ProtocolLimits struct {
	MaximumStringLengths MaximumStringLengths `json:"maximumStringLengths"`
	MaximumArrayLengths  MaximumArrayLengths  `json:"maximumArrayLengths"`
	Timing               Timing               `json:"timing"`
}

// MaximumStringLengths / MaximumArrayLengths are required objects; the adapter
// enforces no caps, so both marshal as {} (schema: absent/zero = no limit).
type MaximumStringLengths struct{}

type MaximumArrayLengths struct{}

type Timing struct {
	MinimumOrderInterval  float64 `json:"minimumOrderInterval"`
	MinimumStateInterval  float64 `json:"minimumStateInterval"`
	DefaultStateInterval  float64 `json:"defaultStateInterval,omitempty"`
	VisualizationInterval float64 `json:"visualizationInterval,omitempty"`
}

type ProtocolFeatures struct {
	OptionalParameters []OptionalParameter `json:"optionalParameters"`
	MobileRobotActions []MobileRobotAction `json:"mobileRobotActions"`
}

type OptionalParameter struct {
	Parameter   string  `json:"parameter"`
	Support     Support `json:"support"`
	Description string  `json:"description,omitempty"`
}

type MobileRobotAction struct {
	ActionType        string            `json:"actionType"`
	ActionDescription string            `json:"actionDescription,omitempty"`
	ActionScopes      []ActionScope     `json:"actionScopes"`
	ActionParameters  []ActionParameter `json:"actionParameters,omitempty"`
	ActionResult      string            `json:"actionResult,omitempty"`
	BlockingTypes     []BlockingType    `json:"blockingTypes,omitempty"`
	PauseAllowed      bool              `json:"pauseAllowed"`
	CancelAllowed     bool              `json:"cancelAllowed"`
}

type ActionParameter struct {
	Key           string   `json:"key"`
	ValueDataType DataType `json:"valueDataType"`
	Description   string   `json:"description,omitempty"`
	IsOptional    *bool    `json:"isOptional,omitempty"`
}

// MobileRobotGeometry / LoadSpecification are required objects with no required
// sub-fields; v1 publishes them empty ({}).
type MobileRobotGeometry struct{}

type LoadSpecification struct{}

// MobileRobotConfiguration is optional and omitted in v1.
type MobileRobotConfiguration struct{}
