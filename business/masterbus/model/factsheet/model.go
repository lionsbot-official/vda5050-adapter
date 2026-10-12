// Package factsheet is the pure-data VDA5050 factsheet message (spec §6.15):
// machine-readable capabilities of the robot type series. Structs + enums only.
package factsheet

import "github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"

type Factsheet struct {
	common.Header
	TypeSpecification  TypeSpecification  `json:"typeSpecification"`
	PhysicalParameters PhysicalParameters `json:"physicalParameters"`
	ProtocolLimits     ProtocolLimits     `json:"protocolLimits"`
	ProtocolFeatures   ProtocolFeatures   `json:"protocolFeatures"`
	AGVGeometry        AGVGeometry        `json:"agvGeometry"`
	LoadSpecification  LoadSpecification  `json:"loadSpecification"`
}

type TypeSpecification struct {
	SeriesName        string             `json:"seriesName"`
	SeriesDescription string             `json:"seriesDescription,omitempty"`
	AGVKinematic      Kinematics         `json:"agvKinematic"`
	AGVClass          Class              `json:"agvClass"`
	MaxLoadMass       float64            `json:"maxLoadMass"`
	LocalizationTypes []LocalizationType `json:"localizationTypes"`
	NavigationTypes   []NavigationType   `json:"navigationTypes"`
}

type PhysicalParameters struct {
	SpeedMin        float64  `json:"speedMin"`
	SpeedMax        float64  `json:"speedMax"`
	AngularSpeedMin *float64 `json:"angularSpeedMin,omitempty"`
	AngularSpeedMax *float64 `json:"angularSpeedMax,omitempty"`
	AccelerationMax float64  `json:"accelerationMax"`
	DecelerationMax float64  `json:"decelerationMax"`
	HeightMin       float64  `json:"heightMin"`
	HeightMax       float64  `json:"heightMax"`
	Width           float64  `json:"width"`
	Length          float64  `json:"length"`
}

type ProtocolLimits struct {
	MaxStringLens MaxStringLens `json:"maxStringLens"`
	MaxArrayLens  MaxArrayLens  `json:"maxArrayLens"`
	Timing        Timing        `json:"timing"`
}

// MaxStringLens / MaxArrayLens are required objects; the adapter
// enforces no caps, so both marshal as {} (schema: absent/zero = no limit).
type MaxStringLens struct{}

type MaxArrayLens struct{}

type Timing struct {
	MinOrderInterval      float64 `json:"minOrderInterval"`
	MinStateInterval      float64 `json:"minStateInterval"`
	DefaultStateInterval  float64 `json:"defaultStateInterval,omitempty"`
	VisualizationInterval float64 `json:"visualizationInterval,omitempty"`
}

type ProtocolFeatures struct {
	OptionalParameters []OptionalParameter `json:"optionalParameters"`
	AGVActions         []AGVAction         `json:"agvActions"`
}

type OptionalParameter struct {
	Parameter   string  `json:"parameter"`
	Support     Support `json:"support"`
	Description string  `json:"description,omitempty"`
}

type AGVAction struct {
	ActionType        string            `json:"actionType"`
	ActionDescription string            `json:"actionDescription,omitempty"`
	ActionScopes      []ActionScope     `json:"actionScopes"`
	ActionParameters  []ActionParameter `json:"actionParameters,omitempty"`
	ResultDescription string            `json:"resultDescription,omitempty"`
}

type ActionParameter struct {
	Key           string   `json:"key"`
	ValueDataType DataType `json:"valueDataType"`
	Description   string   `json:"description,omitempty"`
	IsOptional    *bool    `json:"isOptional,omitempty"`
}

// AGVGeometry / LoadSpecification are required objects with no required
// sub-fields; v1 publishes them empty ({}).
type AGVGeometry struct{}

type LoadSpecification struct{}
