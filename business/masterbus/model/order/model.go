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

	OrderDescription string `json:"orderDescription,omitempty"`
}

type Node struct {
	NodeID     string   `json:"nodeId"`
	SequenceID uint32   `json:"sequenceId"`
	Released   bool     `json:"released"`
	Actions    []Action `json:"actions"`

	NodeDescriptor string        `json:"nodeDescriptor,omitempty"`
	NodePosition   *NodePosition `json:"nodePosition,omitempty"`
}
type Edge struct {
	EdgeID     string `json:"edgeId"`
	SequenceID uint32 `json:"sequenceId"`
	Released   bool   `json:"released"`

	Actions []Action `json:"actions"`

	EdgeDescriptor                  string             `json:"edgeDescriptor,omitempty"`
	MaximumSpeed                    *float64           `json:"maximumSpeed,omitempty"`
	MaximumMobileRobotHeight        *float64           `json:"maximumMobileRobotHeight,omitempty"`
	MinimumLoadHandlingDeviceHeight *float64           `json:"minimumLoadHandlingDeviceHeight,omitempty"`
	Orientation                     *float64           `json:"orientation,omitempty"`
	OrientationType                 OrientationType    `json:"orientationType,omitempty"`
	Direction                       string             `json:"direction,omitempty"`
	ReachOrientationBeforeEntering  bool               `json:"reachOrientationBeforeEntering,omitempty"`
	MaxRotationSpeed                *float64           `json:"maxRotationSpeed,omitempty"`
	Trajectory                      *common.Trajectory `json:"trajectory,omitempty"`
	Length                          *float64           `json:"length,omitempty"`
	Corridor                        *Corridor          `json:"corridor,omitempty"`
}

type Corridor struct {
	LeftWidth  float64 `json:"leftWidth"`
	RightWidth float64 `json:"rightWidth"`

	CorridorReferencePoint *CorridorReferencePoint `json:"corridorReferencePoint,omitempty"`
	ReleaseRequired        *bool                   `json:"releaseRequired,omitempty"`
	ReleaseLossBehavior    ReleaseLossBehavior     `json:"releaseLossBehavior,omitempty"`
}

type Action struct {
	ActionID     string       `json:"actionId"`
	ActionType   string       `json:"actionType"`
	BlockingType BlockingType `json:"blockingType"`

	ActionDescriptor string            `json:"actionDescriptor,omitempty"`
	ActionParameters []ActionParameter `json:"actionParameters,omitempty"`
	Retriable        *bool             `json:"retriable,omitempty"`
}
type NodePosition struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	MapID string  `json:"mapId"`

	Theta                 *float64            `json:"theta,omitempty"`
	AllowedDeviationXY    *AllowedDeviationXY `json:"allowedDeviationXY,omitempty"`
	AllowedDeviationTheta *float64            `json:"allowedDeviationTheta,omitempty"`
}

type AllowedDeviationXY struct {
	A     float64 `json:"a"`
	B     float64 `json:"b"`
	Theta float64 `json:"theta"`
}

type ActionParameter struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}
