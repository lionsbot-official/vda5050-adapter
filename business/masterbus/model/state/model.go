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
	InstantActionStates []ActionState `json:"-"` // internal ownership; publication combines both action lists
	BatteryState        BatteryState  `json:"batteryState"`
	OperatingMode       OperatingMode `json:"operatingMode"`
	Errors              []Error       `json:"errors"`
	SafetyState         SafetyState   `json:"safetyState"`

	Maps                  []Map        `json:"maps,omitempty"`
	AGVPosition           *AGVPosition `json:"agvPosition,omitempty"`
	Velocity              *Velocity    `json:"velocity,omitempty"`
	Loads                 []Load       `json:"loads,omitempty"`
	Paused                *bool        `json:"paused,omitempty"`
	NewBaseRequest        *bool        `json:"newBaseRequest,omitempty"`
	DistanceSinceLastNode *float64     `json:"distanceSinceLastNode,omitempty"`
	Information           []Info       `json:"information,omitempty"` // only for visualisation/debug
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

	MapDescription string `json:"mapDescription,omitempty"`
}

type NodeState struct {
	NodeID     string `json:"nodeId"`
	SequenceID int    `json:"sequenceId"`
	Released   bool   `json:"released"`

	NodeDescription string        `json:"nodeDescription,omitempty"`
	NodePosition    *NodePosition `json:"nodePosition,omitempty"`
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

	EdgeDescription string             `json:"edgeDescription,omitempty"`
	Trajectory      *common.Trajectory `json:"trajectory,omitempty"`
}

type AGVPosition struct {
	X                   float64 `json:"x"`
	Y                   float64 `json:"y"`
	Theta               float64 `json:"theta"`
	MapID               string  `json:"mapId"`
	PositionInitialized bool    `json:"positionInitialized"`

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

type ActionState struct {
	ActionID     string       `json:"actionId"`
	ActionStatus ActionStatus `json:"actionStatus"`

	ActionType        string `json:"actionType,omitempty"`
	ActionDescription string `json:"actionDescription,omitempty"`
	ResultDescription string `json:"resultDescription,omitempty"`
}

type BatteryState struct {
	BatteryCharge float64 `json:"batteryCharge"`
	Charging      bool    `json:"charging"`

	BatteryVoltage *float64 `json:"batteryVoltage,omitempty"`
	BatteryHealth  *int8    `json:"batteryHealth,omitempty"`
	Reach          *uint32  `json:"reach,omitempty"`
}

type Error struct {
	ErrorType  ErrorType  `json:"errorType"`
	ErrorLevel ErrorLevel `json:"errorLevel"`

	ErrorReferences  []ErrorReference `json:"errorReferences,omitempty"`
	ErrorDescription string           `json:"errorDescription,omitempty"`
	ErrorHint        string           `json:"errorHint,omitempty"`
}

type ErrorReference struct {
	ReferenceKey   string `json:"referenceKey"`
	ReferenceValue string `json:"referenceValue"`
}

type Info struct {
	InfoType  string    `json:"infoType"`
	InfoLevel InfoLevel `json:"infoLevel"`

	InfoReferences  []InfoReference `json:"infoReferences,omitempty"`
	InfoDescription string          `json:"infoDescription,omitempty"`
}

type InfoReference struct {
	ReferenceKey   string `json:"referenceKey"`
	ReferenceValue string `json:"referenceValue"`
}

type SafetyState struct {
	EStop          EStop `json:"eStop"`
	FieldViolation bool  `json:"fieldViolation"`
}
