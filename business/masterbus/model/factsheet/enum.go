package factsheet

// VDA 2.0 factsheet values.

type Kinematics string

const (
	KinematicsDifferential    Kinematics = "DIFF"
	KinematicsOmnidirectional Kinematics = "OMNI"
	KinematicsThreeWheel      Kinematics = "THREEWHEEL"
)

type Class string

const (
	ClassForklift Class = "FORKLIFT"
	ClassConveyor Class = "CONVEYOR"
	ClassTugger   Class = "TUGGER"
	ClassCarrier  Class = "CARRIER"
)

type LocalizationType string

const (
	LocalizationNatural   LocalizationType = "NATURAL"
	LocalizationReflector LocalizationType = "REFLECTOR"
	LocalizationRFID      LocalizationType = "RFID"
	LocalizationDMC       LocalizationType = "DMC"
	LocalizationSpot      LocalizationType = "SPOT"
	LocalizationGrid      LocalizationType = "GRID"
)

type NavigationType string

const (
	NavigationPhysicalLineGuided NavigationType = "PHYSICAL_LINE_GUIDED"
	NavigationVirtualLineGuided  NavigationType = "VIRTUAL_LINE_GUIDED"
	NavigationFreelyNavigating   NavigationType = "AUTONOMOUS"
)

// ActionScope is a closed enum in the schema.
type ActionScope string

const (
	ScopeInstant ActionScope = "INSTANT"
	ScopeNode    ActionScope = "NODE"
	ScopeEdge    ActionScope = "EDGE"
)

// DataType is the closed enum for an action parameter's valueDataType.
type DataType string

const (
	DataTypeBool    DataType = "BOOL"
	DataTypeNumber  DataType = "NUMBER"
	DataTypeInteger DataType = "INTEGER"
	DataTypeString  DataType = "STRING"
	DataTypeObject  DataType = "OBJECT"
	DataTypeArray   DataType = "ARRAY"
)

// Support is the closed enum for an optional parameter's support level.
type Support string

const (
	SupportSupported Support = "SUPPORTED"
	SupportRequired  Support = "REQUIRED"
)
