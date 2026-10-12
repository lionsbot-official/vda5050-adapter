package order

import (
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
)

type Order struct {
	common.Header
	OrderID       string `json:"orderId"`
	OrderUpdateID int    `json:"orderUpdateId"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`

	ZoneSetID string `json:"zoneSetId,omitempty"`
}

type Node struct {
	NodeID     string   `json:"nodeId"`
	SequenceID uint32   `json:"sequenceId"`
	Released   bool     `json:"released"`
	Actions    []Action `json:"actions"`

	NodeDescription string        `json:"nodeDescription,omitempty"`
	NodePosition    *NodePosition `json:"nodePosition,omitempty"`
}
type Edge struct {
	EdgeID      string `json:"edgeId"`
	SequenceID  uint32 `json:"sequenceId"`
	Released    bool   `json:"released"`
	StartNodeID string `json:"startNodeId"`
	EndNodeID   string `json:"endNodeId"`

	Actions []Action `json:"actions"`

	EdgeDescription  string             `json:"edgeDescription,omitempty"`
	MaxSpeed         *float64           `json:"maxSpeed,omitempty"`
	MaxHeight        *float64           `json:"maxHeight,omitempty"`
	MinHeight        *float64           `json:"minHeight,omitempty"`
	Orientation      *float64           `json:"orientation,omitempty"`
	OrientationType  OrientationType    `json:"orientationType,omitempty"`
	Direction        string             `json:"direction,omitempty"`
	RotationAllowed  *bool              `json:"rotationAllowed,omitempty"`
	MaxRotationSpeed *float64           `json:"maxRotationSpeed,omitempty"`
	Trajectory       *common.Trajectory `json:"trajectory,omitempty"`
	Length           *float64           `json:"length,omitempty"`
	Corridor         *Corridor          `json:"corridor,omitempty"`
}

type Corridor struct {
	LeftWidth  float64 `json:"leftWidth"`
	RightWidth float64 `json:"rightWidth"`

	CorridorReferencePoint *CorridorReferencePoint `json:"corridorRefPoint,omitempty"`
}

type Action struct {
	ActionID     string       `json:"actionId"`
	ActionType   string       `json:"actionType"`
	BlockingType BlockingType `json:"blockingType"`

	ActionDescription string            `json:"actionDescription,omitempty"`
	ActionParameters  []ActionParameter `json:"actionParameters,omitempty"`
}
type NodePosition struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	MapID string  `json:"mapId"`

	MapDescription        string   `json:"mapDescription,omitempty"`
	Theta                 *float64 `json:"theta,omitempty"`
	AllowedDeviationXY    *float64 `json:"allowedDeviationXY,omitempty"`
	AllowedDeviationTheta *float64 `json:"allowedDeviationTheta,omitempty"`
}

type ActionParameter struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}
