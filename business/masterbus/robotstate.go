package masterbus

import (
	"math"
	"reflect"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

type orderDecision int

const (
	decisionReject orderDecision = iota
	decisionIgnore
	decisionAcceptNew
	decisionAcceptUpdate
)

type robotState struct {
	store *vehiclestate.Store

	// these two are touched only by order-consumer go routine so no lock
	current order.Order // raw last msg: duplicate detection + decision point
	merged  order.Order // accumuated base+update: rebuild source
}

func newRobotState(store *vehiclestate.Store) *robotState {
	return &robotState{store: store}
}

func (r *robotState) snapshot() state.State {
	return r.store.Snapshot()
}

func (r *robotState) addError(e state.Error) {
	r.store.AddError(e)
}

func (r *robotState) applyOrder(o order.Order) {
	op := buildProjection(o)
	r.store.CommitOrderProjection(op)
	o.Header = common.Header{}
	r.current = o
	r.merged = o
}

func (r *robotState) applyOrderUpdate(o order.Order) {
	merged := mergeOrder(r.merged, o)
	op := buildProjection(merged)
	op.KeepLastNode = true // traversal owns lastNodeId; the rebuild must not regress it
	r.store.CommitOrderProjection(op)
	o.Header = common.Header{}
	r.current = o // raw msg for duplicate detection, merged drives the rebuild
	r.merged = merged
}

func buildProjection(o order.Order) vehiclestate.OrderProjection {
	op := vehiclestate.OrderProjection{}

	op.OrderID = o.OrderID
	op.OrderUpdateID = o.OrderUpdateID

	// business requirement
	// declared as empty slice so that marshalling empty gives [] instead of null
	newActionStates := []state.ActionState{}

	// seq 0 node will not be added to the nodeStates
	newNodeStates := make([]state.NodeState, 0, max(0, len(o.Nodes)-1))

	for _, node := range o.Nodes {
		for _, a := range node.Actions {
			newActionStates = append(newActionStates, toActionState(a))
		}
		// if node sequenceId is 0, this is the trivially reachable node
		// so "robot" has arrived and thus will not be appended to nodeStates.
		// actions of the node are appended to actionStates first so that the
		// status of these actions can still be tracked even if the trivial node

		if node.SequenceID == 0 {
			op.LastNodeID = node.NodeID
			op.LastNodeSequenceID = int(node.SequenceID)
			continue
		}
		newNodeStates = append(newNodeStates, toNodeState(node))
	}
	op.NodeStates = newNodeStates

	newEdgeStates := make([]state.EdgeState, 0, len(o.Edges))
	for _, e := range o.Edges {
		for _, a := range e.Actions {
			newActionStates = append(newActionStates, toActionState(a))
		}
		newEdgeStates = append(newEdgeStates, toEdgeState(e))
	}
	op.EdgeStates = newEdgeStates

	op.ActionStates = newActionStates

	return op
}

func (r *robotState) decideOrder(o order.Order) (orderDecision, state.ErrorType) {
	st := r.store.Snapshot()
	cur := r.current

	if st.OrderID != "" && o.OrderID == st.OrderID {
		if r.store.OrderCancelled() {
			return decisionReject, state.ErrorTypeOrderUpdateFollowingCancel
		}
		if o.OrderUpdateID < st.OrderUpdateID {
			return decisionReject, state.ErrorTypeOutdatedOrderUpdate
		}
		if o.OrderUpdateID == st.OrderUpdateID {
			o.Header = common.Header{}
			if reflect.DeepEqual(o, cur) {
				return decisionIgnore, ""
			}
			return decisionReject, state.ErrorTypeSameOrderUpdateID
		}
		if !isValidStitch(cur, o) {
			return decisionReject, state.ErrorTypeStartNodeOutOfRange
		}
		return decisionAcceptUpdate, ""
	}

	if !isIdle(st) {
		return decisionReject, state.ErrorTypeOtherOrderActive
	}
	if o.OrderUpdateID != 0 {
		return decisionReject, state.ErrorTypeValidationFailure
	}
	if fn, ok := firstNode(o); ok && !r.startNodeInRange(fn) {
		return decisionReject, state.ErrorTypeStartNodeOutOfRange
	}
	return decisionAcceptNew, ""
}

// defaultStartDeviationXY bounds "standing on the start node" when the order
// specifies no allowed deviation.
const defaultStartDeviationXY = 0.5 // metres

// startNodeInRange: a new order is only acceptable if the robot is within the
// first node's deviation range. Unknown robot position, an unlocalized pose
// (x/y/theta not trustworthy, VDA5050_EN.md:1815), or a node without a position
// cannot be checked and passes.
func (r *robotState) startNodeInRange(n order.Node) bool {
	if n.NodePosition == nil {
		return true
	}
	pos := r.store.Snapshot().MobileRobotPosition
	if pos == nil || !pos.Localized {
		return true
	}
	radius := defaultStartDeviationXY
	if dev := n.NodePosition.AllowedDeviationXY; dev != nil && dev.A > 0 {
		radius = dev.A
	}
	return math.Hypot(pos.X-n.NodePosition.X, pos.Y-n.NodePosition.Y) <= radius
}

// decisionPoint: released node with the highest sequenceId in the held order,
// the sttiching node the valid update must restate
func decisionPoint(held order.Order) (order.Node, bool) {
	var dp order.Node
	found := false
	for _, n := range held.Nodes {
		if !n.Released {
			continue
		}
		//  Without !found, decisionPoint would wrongly report "no decision point" for any order whose
		//  only released node is seq 0.
		if !found || n.SequenceID > dp.SequenceID {
			dp, found = n, true
		}
	}
	return dp, found
}

// firstNode: node with the lowest sequenceId aka stitching node of an update
// array order is not trusted thats why need to find
func firstNode(o order.Order) (order.Node, bool) {
	var first order.Node
	found := false
	for _, n := range o.Nodes {
		// Without !found, decisionPoint would wrongly report "no decision point" for any order whose
		// only released node is seq 0.
		if !found || n.SequenceID < first.SequenceID {
			first, found = n, true
		}
	}
	return first, found
}

func isValidStitch(held, o order.Order) bool {
	dp, ok := decisionPoint(held)
	if !ok {
		return false
	}
	stitch, ok := firstNode(o)
	if !ok {
		return false
	}
	return dp.NodeID == stitch.NodeID && dp.SequenceID == stitch.SequenceID
}

func mergeOrder(base, update order.Order) order.Order {
	stitch, ok := firstNode(update)
	if !ok {
		return update
	}
	dp := stitch.SequenceID

	nodes := make([]order.Node, 0, len(base.Nodes)+len(update.Nodes))
	for _, n := range base.Nodes {
		if n.SequenceID < dp {
			nodes = append(nodes, n)
		}
	}
	nodes = append(nodes, update.Nodes...)

	edges := make([]order.Edge, 0, len(base.Edges)+len(update.Edges))
	for _, e := range base.Edges {
		if e.SequenceID < dp {
			edges = append(edges, e)
		}
	}
	edges = append(edges, update.Edges...)

	return order.Order{
		OrderID:       update.OrderID,
		OrderUpdateID: update.OrderUpdateID,
		Nodes:         nodes,
		Edges:         edges,
	}
}

func toActionState(a order.Action) state.ActionState {
	// actionResult is not set in actionState as
	// it is only written after completion
	return state.ActionState{
		ActionID:         a.ActionID,
		ActionType:       a.ActionType,
		ActionDescriptor: a.ActionDescriptor,
		ActionStatus:     state.ActionStatusWaiting,
	}
}

func toNodeState(n order.Node) state.NodeState {
	ns := state.NodeState{
		NodeID:         n.NodeID,
		SequenceID:     int(n.SequenceID),
		Released:       n.Released,
		NodeDescriptor: n.NodeDescriptor,
	}
	if n.NodePosition != nil {
		pos := &state.NodePosition{
			X:     n.NodePosition.X,
			Y:     n.NodePosition.Y,
			MapID: n.NodePosition.MapID,
			Theta: n.NodePosition.Theta,
		}
		ns.NodePosition = pos
	}
	return ns
}

func toEdgeState(e order.Edge) state.EdgeState {
	es := state.EdgeState{
		EdgeID:         e.EdgeID,
		SequenceID:     int(e.SequenceID),
		Released:       e.Released,
		EdgeDescriptor: e.EdgeDescriptor,
		// aliases the order's trajectory; deep-copy if state ever mutates it
		Trajectory: e.Trajectory,
	}
	return es
}

func isIdle(st state.State) bool {
	return vehiclestate.IsIdle(st)
}
