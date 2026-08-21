package state

type OperatingMode string

const (
	OperatingModeStartup       OperatingMode = "STARTUP"
	OperatingModeAutomatic     OperatingMode = "AUTOMATIC"
	OperatingModeSemiAutomatic OperatingMode = "SEMIAUTOMATIC"
	OperatingModeIntervened    OperatingMode = "INTERVENED"
	OperatingModeManual        OperatingMode = "MANUAL"
	OperatingModeService       OperatingMode = "SERVICE"
	OperatingModeTeachIn       OperatingMode = "TEACH_IN"
)

type MapStatus string

const (
	MapStatusEnabled  MapStatus = "ENABLED"
	MapStatusDisabled MapStatus = "DISABLED"
)

type ZoneSetStatus string

const (
	ZoneSetStatusEnabled  ZoneSetStatus = "ENABLED"
	ZoneSetStatusDisabled ZoneSetStatus = "DISABLED"
)

type ZoneRequestType string

const (
	ZoneRequestTypeAccess     ZoneRequestType = "ACCESS"
	ZoneRequestTypeReplanning ZoneRequestType = "REPLANNING"
)

type RequestStatus string

const (
	RequestStatusRequested RequestStatus = "REQUESTED"
	RequestStatusGranted   RequestStatus = "GRANTED"
	RequestStatusRevoked   RequestStatus = "REVOKED"
	RequestStatusExpired   RequestStatus = "EXPIRED"
)

type EdgeRequestType string

const (
	EdgeRequestTypeCorridor EdgeRequestType = "CORRIDOR"
)

type ActionStatus string

const (
	ActionStatusWaiting      ActionStatus = "WAITING"
	ActionStatusInitializing ActionStatus = "INITIALIZING"
	ActionStatusRunning      ActionStatus = "RUNNING"
	ActionStatusPaused       ActionStatus = "PAUSED"
	ActionStatusRetriable    ActionStatus = "RETRIABLE"
	ActionStatusFinished     ActionStatus = "FINISHED"
	ActionStatusFailed       ActionStatus = "FAILED"
)

type ErrorLevel string

const (
	ErrorLevelWarning  ErrorLevel = "WARNING"
	ErrorLevelUrgent   ErrorLevel = "URGENT"
	ErrorLevelCritical ErrorLevel = "CRITICAL"
	ErrorLevelFatal    ErrorLevel = "FATAL"
)

type InfoLevel string

const (
	InfoLevelInfo  InfoLevel = "INFO"
	InfoLevelDebug InfoLevel = "DEBUG"
)

type ErrorType string

const (
	ErrorTypeBlockedZoneViolation       ErrorType = "BLOCKED_ZONE_VIOLATION"
	ErrorTypeDuplicateMap               ErrorType = "DUPLICATE_MAP"
	ErrorTypeDuplicateZoneSet           ErrorType = "DUPLICATE_ZONE_SET"
	ErrorTypeInsufficientMemory         ErrorType = "INSUFFICIENT_MEMORY"
	ErrorTypeInvalidInstantAction       ErrorType = "INVALID_INSTANT_ACTION"
	ErrorTypeInvalidOrderAction         ErrorType = "INVALID_ORDER_ACTION"
	ErrorTypeLocalizationError          ErrorType = "LOCALIZATION_ERROR"
	ErrorTypeMobileRobotNotAvailable    ErrorType = "MOBILE_ROBOT_NOT_AVAILABLE"
	ErrorTypeNodeUnreachable            ErrorType = "NODE_UNREACHABLE"
	ErrorTypeNoOrderToCancel            ErrorType = "NO_ORDER_TO_CANCEL"
	ErrorTypeNoRouteToTarget            ErrorType = "NO_ROUTE_TO_TARGET"
	ErrorTypeOrderUpdateFollowingCancel ErrorType = "ORDER_UPDATE_FOLLOWING_CANCEL"
	ErrorTypeOtherOrderActive           ErrorType = "OTHER_ORDER_ACTIVE"
	ErrorTypeOutdatedOrderUpdate        ErrorType = "OUTDATED_ORDER_UPDATE"
	ErrorTypeOutsideOfCorridor          ErrorType = "OUTSIDE_OF_CORRIDOR"
	ErrorTypeReleaseLost                ErrorType = "RELEASE_LOST"
	ErrorTypeSameOrderUpdateID          ErrorType = "SAME_ORDER_UPDATE_ID"
	ErrorTypeStartNodeOutOfRange        ErrorType = "START_NODE_OUT_OF_RANGE"
	ErrorTypeUnknownMapID               ErrorType = "UNKNOWN_MAP_ID"
	ErrorTypeUnsupportedParameter       ErrorType = "UNSUPPORTED_PARAMETER"
	ErrorTypeValidationFailure          ErrorType = "VALIDATION_FAILURE"
	ErrorTypeZoneActionConflict         ErrorType = "ZONE_ACTION_CONFLICT"
)

type ActiveEmergencyStop string

const (
	ActiveEmergencyStopManual ActiveEmergencyStop = "MANUAL"
	ActiveEmergencyStopRemote ActiveEmergencyStop = "REMOTE"
	ActiveEmergencyStopNone   ActiveEmergencyStop = "NONE"
)
