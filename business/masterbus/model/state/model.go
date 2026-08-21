package state

import (
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
)

type State struct {
	common.Header
	OrderID             string        `json:"orderId"`
	OrderUpdateID       int           `json:"orderUpdateId"`
	LastNodeID          string        `json:"lastNodeId"`
	LastNodeSequenceID  int           `json:"lastNodeSequenceId"`
	NodeStates          []NodeState   `json:"nodeStates"`
	EdgeStates          []EdgeState   `json:"edgeStates"`
	Driving             bool          `json:"driving"`
	ActionStates        []ActionState `json:"actionStates"`
	InstantActionStates []ActionState `json:"instantActionStates"`
	PowerSupply         PowerSupply   `json:"powerSupply"`
	OperatingMode       OperatingMode `json:"operatingMode"`
	Errors              []Error       `json:"errors"`
	SafetyState         SafetyState   `json:"safetyState"`

	Maps                  []Map                `json:"maps,omitempty"`
	ZoneSets              []ZoneSet            `json:"zoneSets,omitempty"`
	PlannedPath           *PlannedPath         `json:"plannedPath,omitempty"`
	IntermediatePath      *IntermediatePath    `json:"intermediatePath,omitempty"`
	MobileRobotPosition   *MobileRobotPosition `json:"mobileRobotPosition,omitempty"`
	Velocity              *Velocity            `json:"velocity,omitempty"`
	Loads                 []Load               `json:"loads,omitempty"`
	Paused                *bool                `json:"paused,omitempty"`
	NewBaseRequest        *bool                `json:"newBaseRequest,omitempty"`
	ZoneRequests          []ZoneRequest        `json:"zoneRequests,omitempty"`
	EdgeRequests          []EdgeRequest        `json:"edgeRequests,omitempty"`
	DistanceSinceLastNode *float64             `json:"distanceSinceLastNode,omitempty"`
	ZoneActionStates      []ActionState        `json:"zoneActionStates,omitempty"`
	Information           []Info               `json:"information,omitempty"` // only for visualisation/debug
}

type Velocity struct {
	Vx    float64 `json:"vx"`
	Vy    float64 `json:"vy"`
	Omega float64 `json:"omega"`
}

type Map struct {
	MapID      string    `json:"mapId"`
	MapVersion string    `json:"mapVersion"`
	MapStatus  MapStatus `json:"mapStatus"`

	MapDescriptor string `json:"mapDescriptor,omitempty"`
}

type ZoneSet struct {
	ZoneSetID     string        `json:"zoneSetId"`
	MapID         string        `json:"mapId"`
	ZoneSetStatus ZoneSetStatus `json:"zoneSetStatus"`
}

type NodeState struct {
	NodeID     string `json:"nodeId"`
	SequenceID int    `json:"sequenceId"`
	Released   bool   `json:"released"`

	NodeDescriptor string        `json:"nodeDescriptor,omitempty"`
	NodePosition   *NodePosition `json:"nodePosition,omitempty"`
}

type NodePosition struct {
	X     float64  `json:"x"`
	Y     float64  `json:"y"`
	Theta *float64 `json:"theta,omitempty"`
	MapID string   `json:"mapId"`
}

type EdgeState struct {
	EdgeID     string `json:"edgeId"`
	SequenceID int    `json:"sequenceId"`
	Released   bool   `json:"released"`

	EdgeDescriptor string             `json:"edgeDescriptor,omitempty"`
	Trajectory     *common.Trajectory `json:"trajectory,omitempty"`
}

type PlannedPath struct {
	Trajectory     common.Trajectory `json:"trajectory"`
	TraversedNodes []string          `json:"traversedNodes,omitempty"`
}

type IntermediatePath struct {
	Polyline []Polyline `json:"polyline"`
}
type Polyline struct {
	X     float64  `json:"x"`
	Y     float64  `json:"y"`
	Eta   string   `json:"eta"`
	Theta *float64 `json:"theta,omitempty"`
}

type MobileRobotPosition struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Theta     float64 `json:"theta"`
	MapID     string  `json:"mapId"`
	Localized bool    `json:"localized"`

	LocalizationScore *float64 `json:"localizationScore,omitempty"`
	DeviationRange    *float64 `json:"deviationRange,omitempty"`
}

type Load struct {
	// if robot cannot identify load, drop LoadID
	LoadID               string                `json:"loadId,omitempty"`
	LoadType             string                `json:"loadType,omitempty"`
	LoadPosition         string                `json:"loadPosition,omitempty"`
	BoundingBoxReference *BoundingBoxReference `json:"boundingBoxReference,omitempty"`
	LoadDimensions       *LoadDimensions       `json:"loadDimensions,omitempty"`
	Weight               *float64              `json:"weight,omitempty"`
}

type LoadDimensions struct {
	Length float64  `json:"length"`
	Width  float64  `json:"width"`
	Height *float64 `json:"height,omitempty"`
}

type BoundingBoxReference struct {
	X     float64  `json:"x"`
	Y     float64  `json:"y"`
	Z     float64  `json:"z"`
	Theta *float64 `json:"theta,omitempty"`
}

type ZoneRequest struct {
	RequestID     string          `json:"requestId"`
	RequestType   ZoneRequestType `json:"requestType"`
	ZoneId        string          `json:"zoneId"`
	ZoneSetId     string          `json:"zoneSetId"`
	RequestStatus RequestStatus   `json:"requestStatus"`

	Trajectory *common.Trajectory `json:"trajectory,omitempty"`
}

type EdgeRequest struct {
	RequestID     string          `json:"requestId"`
	RequestType   EdgeRequestType `json:"requestType"`
	EdgeID        string          `json:"edgeId"`
	SequenceID    int             `json:"sequenceId"`
	RequestStatus RequestStatus   `json:"requestStatus"`
}

type ActionState struct {
	ActionID     string       `json:"actionId"`
	ActionStatus ActionStatus `json:"actionStatus"`

	ActionType       string `json:"actionType,omitempty"`
	ActionDescriptor string `json:"actionDescriptor,omitempty"`
	ActionResult     string `json:"actionResult,omitempty"`
}

type PowerSupply struct {
	StateOfCharge float64 `json:"stateOfCharge"`
	Charging      bool    `json:"charging"`

	BatteryVoltage *float64 `json:"batteryVoltage,omitempty"`
	BatteryCurrent *float64 `json:"batteryCurrent,omitempty"`
	BatteryHealth  *float64 `json:"batteryHealth,omitempty"`
	Range          *float64 `json:"range,omitempty"`
}

type Error struct {
	ErrorType  ErrorType  `json:"errorType"`
	ErrorLevel ErrorLevel `json:"errorLevel"`

	ErrorReferences              []ErrorReference `json:"errorReferences,omitempty"`
	ErrorDescription             string           `json:"errorDescription,omitempty"`
	ErrorDescriptionTranslations []Translation    `json:"errorDescriptionTranslations,omitempty"`
	ErrorHint                    string           `json:"errorHint,omitempty"`
	ErrorHintTranslations        []Translation    `json:"errorHintTranslations,omitempty"`
}

type ErrorReference struct {
	ReferenceKey   string `json:"referenceKey"`
	ReferenceValue string `json:"referenceValue"`
}

type Translation struct {
	TranslationKey   string `json:"translationKey"`
	TranslationValue string `json:"translationValue"`
}

type Info struct {
	InfoType  string    `json:"infoType"`
	InfoLevel InfoLevel `json:"infoLevel"`

	InfoReferences []InfoReference `json:"infoReferences,omitempty"`
	InfoDescriptor string          `json:"infoDescriptor,omitempty"`
}

type InfoReference struct {
	ReferenceKey   string `json:"referenceKey"`
	ReferenceValue string `json:"referenceValue"`
}

type SafetyState struct {
	ActiveEmergencyStop ActiveEmergencyStop `json:"activeEmergencyStop"`
	FieldViolation      bool                `json:"fieldViolation"`
}
