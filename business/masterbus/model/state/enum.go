package state

type OperatingMode string

const (
	OperatingModeAutomatic     OperatingMode = "AUTOMATIC"
	OperatingModeSemiAutomatic OperatingMode = "SEMIAUTOMATIC"
	OperatingModeManual        OperatingMode = "MANUAL"
	OperatingModeService       OperatingMode = "SERVICE"
	OperatingModeTeachIn       OperatingMode = "TEACHIN"
)

type MapStatus string

const (
	MapStatusEnabled  MapStatus = "ENABLED"
	MapStatusDisabled MapStatus = "DISABLED"
)

type ActionStatus string

const (
	ActionStatusWaiting      ActionStatus = "WAITING"
	ActionStatusInitializing ActionStatus = "INITIALIZING"
	ActionStatusRunning      ActionStatus = "RUNNING"
	ActionStatusPaused       ActionStatus = "PAUSED"
	ActionStatusFinished     ActionStatus = "FINISHED"
	ActionStatusFailed       ActionStatus = "FAILED"
)

type ErrorLevel string

const (
	ErrorLevelWarning ErrorLevel = "WARNING"
	ErrorLevelFatal   ErrorLevel = "FATAL"
)

type InfoLevel string

const (
	InfoLevelInfo  InfoLevel = "INFO"
	InfoLevelDebug InfoLevel = "DEBUG"
)

type ErrorType string

const (
	ErrorTypeOrderError                 ErrorType = "orderError"
	ErrorTypeBlockedZoneViolation       ErrorType = "BLOCKED_ZONE_VIOLATION"
	ErrorTypeDuplicateMap               ErrorType = "DUPLICATE_MAP"
	ErrorTypeDuplicateZoneSet           ErrorType = "DUPLICATE_ZONE_SET"
	ErrorTypeInsufficientMemory         ErrorType = "INSUFFICIENT_MEMORY"
	ErrorTypeInvalidInstantAction       ErrorType = "INVALID_INSTANT_ACTION"
	ErrorTypeInvalidOrderAction         ErrorType = "INVALID_ORDER_ACTION"
	ErrorTypeLocalizationError          ErrorType = "localizationError"
	ErrorTypeMobileRobotNotAvailable    ErrorType = "MOBILE_ROBOT_NOT_AVAILABLE"
	ErrorTypeNodeUnreachable            ErrorType = "NODE_UNREACHABLE"
	ErrorTypeNoOrderToCancel            ErrorType = "noOrderToCancel"
	ErrorTypeNoRouteToTarget            ErrorType = "NO_ROUTE_TO_TARGET"
	ErrorTypeOrderUpdateFollowingCancel ErrorType = "ORDER_UPDATE_FOLLOWING_CANCEL"
	ErrorTypeOtherOrderActive           ErrorType = "OTHER_ORDER_ACTIVE"
	ErrorTypeOutdatedOrderUpdate        ErrorType = "orderUpdateError"
	ErrorTypeOutsideOfCorridor          ErrorType = "OUTSIDE_OF_CORRIDOR"
	ErrorTypeReleaseLost                ErrorType = "RELEASE_LOST"
	ErrorTypeSameOrderUpdateID          ErrorType = "SAME_ORDER_UPDATE_ID"
	ErrorTypeStartNodeOutOfRange        ErrorType = "START_NODE_OUT_OF_RANGE"
	ErrorTypeUnknownMapID               ErrorType = "UNKNOWN_MAP_ID"
	ErrorTypeUnsupportedParameter       ErrorType = "UNSUPPORTED_PARAMETER"
	ErrorTypeValidationFailure          ErrorType = "validationError"
	ErrorTypeZoneActionConflict         ErrorType = "ZONE_ACTION_CONFLICT"
)

type EStop string

const (
	EStopAutoAck EStop = "AUTOACK"
	EStopManual  EStop = "MANUAL"
	EStopRemote  EStop = "REMOTE"
	EStopNone    EStop = "NONE"
)
