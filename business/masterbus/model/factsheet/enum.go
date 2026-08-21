package factsheet

// Kinematics, Class, LocalizationType, NavigationType are extensible enums in
// the schema (free strings); the consts document the spec-listed values.

type Kinematics string

const (
	KinematicsDifferential    Kinematics = "DIFFERENTIAL"
	KinematicsOmnidirectional Kinematics = "OMNIDIRECTIONAL"
	KinematicsThreeWheel      Kinematics = "THREE_WHEEL"
)

type Class string

const (
	ClassForklift Class = "FORKLIFT"
	ClassConveyor Class = "CONVEYOR"
	ClassTugger   Class = "TUGGER"
	ClassCarrier  Class = "CARRIER"
	ClassCleaning Class = "CLEANING"
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
	NavigationFreelyNavigating   NavigationType = "FREELY_NAVIGATING"
)

// ZoneType is a closed enum in the schema.
type ZoneType string

const (
	ZoneBlocked               ZoneType = "BLOCKED"
	ZoneLineGuided            ZoneType = "LINE_GUIDED"
	ZoneRelease               ZoneType = "RELEASE"
	ZoneCoordinatedReplanning ZoneType = "COORDINATED_REPLANNING"
	ZoneSpeedLimit            ZoneType = "SPEED_LIMIT"
	ZoneAction                ZoneType = "ACTION"
	ZonePriority              ZoneType = "PRIORITY"
	ZonePenalty               ZoneType = "PENALTY"
	ZoneDirected              ZoneType = "DIRECTED"
	ZoneBidirected            ZoneType = "BIDIRECTED"
)

// ActionScope is a closed enum in the schema.
type ActionScope string

const (
	ScopeInstant ActionScope = "INSTANT"
	ScopeNode    ActionScope = "NODE"
	ScopeEdge    ActionScope = "EDGE"
	ScopeZone    ActionScope = "ZONE"
)

// BlockingType is a closed enum in the schema.
type BlockingType string

const (
	BlockingNone   BlockingType = "NONE"
	BlockingSoft   BlockingType = "SOFT"
	BlockingSingle BlockingType = "SINGLE"
	BlockingHard   BlockingType = "HARD"
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
