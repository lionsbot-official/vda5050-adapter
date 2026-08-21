// Package executor sequences an accepted order onto the robot: it dispatches
// one released node at a time, verifies completion, and commits traversal to
// the shared vehicle state.
package executor

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// defaultDeviationXY bounds "reached the node" when the order specifies none.
const defaultDeviationXY = 0.5 // metres

// settleAfterMove is the gap between a completed move and the next dispatch: the
// robot rejects a p2p_start sent while it is still settling out of a just-ended
// mission (observed on hardware). Tunable.
const settleAfterMove = 1500 * time.Millisecond

type Executor struct {
	store       *vehiclestate.Store
	cmd         robotbus.RobotCommander
	orders      chan order.Order
	instants    chan order.Action
	orderID     string        // current order, for error references (sequencer goroutine only)
	paused      bool          // pause mode; nothing dispatches while set (sequencer goroutine only)
	settling    bool          // settle gap after a move; nothing dispatches while set
	settleDelay time.Duration // gap before dispatching the node after a completed move; 0 in tests
}

func New(store *vehiclestate.Store, cmd robotbus.RobotCommander) *Executor {
	return &Executor{
		store:       store,
		cmd:         cmd,
		orders:      make(chan order.Order, 4),
		instants:    make(chan order.Action, 4),
		settleDelay: settleAfterMove,
	}
}

// OrderAccepted hands the executor the accepted (merged) order. Wired as the
// masterbus accept hook from main.
func (e *Executor) OrderAccepted(o order.Order) {
	e.orders <- o
}

// InstantAction hands the executor an accepted instant action. Wired as the
// masterbus instant-action hook from main.
func (e *Executor) InstantAction(a order.Action) {
	e.instants <- a
}

// Start runs the sequencer until the context is cancelled: one move in flight
// at a time, traversal committed on verified completion.
func (e *Executor) Start(ctx context.Context) error {
	events, err := e.cmd.Events()
	if err != nil {
		return fmt.Errorf("executor: failed to open execution events: %w", err)
	}

	var queue []order.Node
	var nodeActions []order.Action // actions to run at the current node, held with the plan
	var inFlight *order.Node
	var charge *order.Action     // in-flight charging instant action
	var reloc *order.Action      // in-flight initializePosition instant action
	var cleaning *order.Action   // in-flight startCleaning node action
	var docking *order.Action    // in-flight startCharging/stopCharging node action
	var cancelling *order.Action // cancelOrder awaiting the interrupted move's end event
	stopping := false            // a replan interrupted the in-flight move; await its end event
	undocking := false           // auto-undock in flight; first move held until it completes
	var settleC <-chan time.Time // fires when the post-move settle gap elapses; then dispatch

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-settleC:
			settleC = nil
			e.settling = false
			e.dispatchNext(&queue, &inFlight)
		case a := <-e.instants:
			switch a.ActionType {
			case "cancelOrder":
				e.handleCancel(a, &queue, &nodeActions, &inFlight, &cleaning, &docking, &cancelling)
			case "startPause":
				e.handlePause(a, inFlight, cleaning, docking)
			case "stopPause":
				e.handleResume(a, &queue, &inFlight, cleaning, docking)
			case "initializePosition":
				busy := inFlight != nil || len(queue) > 0 || stopping || cleaning != nil || docking != nil || charge != nil
				e.handleRelocalize(a, busy, &reloc)
			default:
				busy := inFlight != nil || len(queue) > 0 || stopping || cleaning != nil || docking != nil || reloc != nil
				e.handleInstantAction(a, busy, &charge)
			}
		case o := <-e.orders:
			newQueue := e.plan(o)
			// An update that only appended nodes beyond the one in flight leaves
			// the current move valid — keep the robot moving toward it, swap in
			// the new tail, and do NOT p2p_stop. Stopping on every append thrashes
			// the robot and strands it mid-route.
			if inFlight != nil && len(newQueue) > 0 &&
				newQueue[0].NodeID == inFlight.NodeID && newQueue[0].SequenceID == inFlight.SequenceID {
				queue = newQueue[1:]
				continue
			}
			queue = newQueue
			nodeActions = e.currentActions(o)
			if inFlight != nil {
				// the in-flight target is no longer next: never race the robot
				// toward a stale target — stop, then dispatch the new plan once
				// the interrupted move has ended.
				if err := e.cmd.StopMovement(); err != nil {
					e.reportUnreachable(*inFlight, "stop dispatch failed: "+err.Error())
					queue = nil // halt until fleet control re-plans
					nodeActions = nil
					inFlight = nil
					continue
				}
				inFlight = nil
				stopping = true
				continue
			}
			e.beginPlan(&queue, &inFlight, &undocking)
			e.beginNodeActions(&nodeActions, &cleaning, &docking)
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if undocking && ev.StopChargingCompleted != nil {
				undocking = false
				if ev.StopChargingCompleted.Success {
					e.dispatchNext(&queue, &inFlight)
				} else if len(queue) > 0 {
					e.reportUnreachable(queue[0], "undock failed: "+ev.StopChargingCompleted.Reason)
					queue = nil // halt until fleet control re-plans
				}
				continue
			}
			if ev.StartChargingCompleted != nil || ev.StopChargingCompleted != nil {
				// a node-scoped dock/undock tracks in actionStates; the instant
				// charge in instantActionStates. At most one is in flight.
				if docking != nil {
					e.completeDocking(ev, &docking)
				} else {
					e.completeCharging(ev, &charge)
				}
				continue
			}
			if ev.RelocalizeCompleted != nil {
				e.completeRelocalize(ev.RelocalizeCompleted, &reloc)
				continue
			}
			if ev.CleanCompleted != nil {
				e.completeCleaning(ev.CleanCompleted, &cleaning)
				continue
			}
			if ev.MoveCompleted == nil {
				continue
			}
			if cancelling != nil {
				// the cancelled move ended; never commit it — finish the cancel
				e.store.CancelOrder()
				e.store.SetInstantActionStatus(cancelling.ActionID, state.ActionStatusFinished, "")
				cancelling = nil
				continue
			}
			if stopping {
				stopping = false // the interrupted move ended; never commit it
				e.beginPlan(&queue, &inFlight, &undocking)
				e.beginNodeActions(&nodeActions, &cleaning, &docking)
				continue
			}
			if inFlight == nil {
				continue
			}
			if ev.MoveCompleted.Success && e.withinDeviation(*inFlight) {
				e.store.CommitTraversal(inFlight.NodeID, int(inFlight.SequenceID))
				inFlight = nil
				// The robot needs a gap after a completed move before it accepts
				// the next p2p_start — dispatch after a settle rather than at once.
				if e.settleDelay > 0 && len(queue) > 0 {
					e.settling = true
					settleC = time.After(e.settleDelay)
				} else {
					e.dispatchNext(&queue, &inFlight)
				}
			} else {
				reason := ev.MoveCompleted.Reason
				if ev.MoveCompleted.Success {
					reason = "move completed outside the allowed node deviation"
				}
				e.reportUnreachable(*inFlight, reason)
				queue = nil // halt until fleet control re-plans
				inFlight = nil
			}
		}
	}
}

// plan lists the released, position-bearing nodes still ahead of the robot,
// in traversal order.
func (e *Executor) plan(o order.Order) []order.Node {
	e.orderID = o.OrderID
	last := e.store.Snapshot().LastNodeSequenceID
	var queue []order.Node
	for _, n := range o.Nodes {
		if n.Released && int(n.SequenceID) > last && n.NodePosition != nil {
			queue = append(queue, n)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].SequenceID < queue[j].SequenceID })
	return queue
}

// currentActions lists the actions of already-reached released nodes — for a
// fresh order the trivially reachable seq-0 node; they execute at the current
// node, alongside any movement the plan dispatches.
func (e *Executor) currentActions(o order.Order) []order.Action {
	last := e.store.Snapshot().LastNodeSequenceID
	var acts []order.Action
	for _, n := range o.Nodes {
		if n.Released && int(n.SequenceID) <= last {
			acts = append(acts, n.Actions...)
		}
	}
	return acts
}

// beginNodeActions runs the held current-node actions off a single-node order:
// startCleaning, or startCharging/stopCharging as a self-navigating dock/undock.
func (e *Executor) beginNodeActions(acts *[]order.Action, cleaning **order.Action, docking **order.Action) {
	pending := *acts
	*acts = nil
	for _, a := range pending {
		switch a.ActionType {
		case "startCleaning":
			e.startCleaning(a, cleaning)
		case "startCharging", "stopCharging":
			e.startDocking(a, docking)
		default:
			e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, fmt.Sprintf("unsupported node action %q", a.ActionType))
		}
	}
}

// beginPlan starts dispatching a fresh plan, undocking first when the robot is
// charging: the first move is held until the undock's end event arrives.
func (e *Executor) beginPlan(queue *[]order.Node, inFlight **order.Node, undocking *bool) {
	if len(*queue) == 0 {
		return
	}
	if *undocking {
		return // plan replaced mid-undock; the pending completion dispatches it
	}
	if e.store.Snapshot().PowerSupply.Charging {
		if err := e.cmd.StopCharging(); err != nil {
			e.reportUnreachable((*queue)[0], "undock dispatch failed: "+err.Error())
			*queue = nil // halt until fleet control re-plans
			return
		}
		*undocking = true
		return
	}
	e.dispatchNext(queue, inFlight)
}

// handlePause activates pause mode (VDA :707, valid at any time): pause the
// in-flight operation by kind (clean_pause for a clean, else p2p_pause), gate
// further dispatching, report the action FINISHED.
func (e *Executor) handlePause(a order.Action, inFlight *order.Node, cleaning *order.Action, docking *order.Action) {
	var err error
	switch {
	case cleaning != nil:
		err = e.cmd.PauseCleaning()
	case docking != nil:
		err = e.cmd.PauseDocking()
	case inFlight != nil:
		err = e.cmd.PauseMovement()
	}
	if err != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "pause dispatch failed: "+err.Error())
		return
	}
	e.paused = true
	e.store.SetPaused(true)
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFinished, "")
}

// handleResume deactivates pause mode: resume the in-flight operation by kind
// (clean_continue with the order's bypassBlackout, else p2p_continue), then
// dispatch whatever was held. Resuming a non-paused vehicle is a no-op FINISHED.
func (e *Executor) handleResume(a order.Action, queue *[]order.Node, inFlight **order.Node, cleaning *order.Action, docking *order.Action) {
	if e.paused {
		var err error
		switch {
		case cleaning != nil:
			_, bypass, perr := cleaningParams(*cleaning)
			if perr != nil {
				e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "resume dispatch failed: "+perr.Error())
				return
			}
			err = e.cmd.ResumeCleaning(bypass)
		case docking != nil:
			err = e.cmd.ResumeDocking()
		case *inFlight != nil:
			err = e.cmd.ResumeMovement()
		}
		if err != nil {
			e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "resume dispatch failed: "+err.Error())
			return
		}
	}
	e.paused = false
	e.store.SetPaused(false)
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFinished, "")
	e.dispatchNext(queue, inFlight)
}

func (e *Executor) dispatchNext(queue *[]order.Node, inFlight **order.Node) {
	if e.paused || e.settling || *inFlight != nil || len(*queue) == 0 {
		return
	}
	n := (*queue)[0]
	*queue = (*queue)[1:]

	theta := e.currentHeading()
	if n.NodePosition.Theta != nil {
		theta = *n.NodePosition.Theta
	}
	if err := e.cmd.MoveTo(n.NodePosition.X, n.NodePosition.Y, theta); err != nil {
		*queue = nil // halt until fleet control re-plans
		e.reportUnreachable(n, "move dispatch failed: "+err.Error())
		return
	}
	*inFlight = &n
}

// handleInstantAction dispatches a charging instant action, unless an order or
// another instant action is executing (instant actions must not conflict with
// the running order).
func (e *Executor) handleInstantAction(a order.Action, busy bool, charge **order.Action) {
	if busy || *charge != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "another operation is executing")
		return
	}
	var err error
	switch a.ActionType {
	case "startCharging":
		err = e.cmd.StartCharging()
	case "stopCharging":
		err = e.cmd.StopCharging()
	default:
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, fmt.Sprintf("unsupported instant action %q", a.ActionType))
		return
	}
	if err != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, err.Error())
		return
	}
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusRunning, "")
	ac := a
	*charge = &ac
}

// handleCancel implements the cancelOrder instant action: stop the in-flight
// operation by kind (clean_stop for a clean, else the move's stop whose end
// event completes the cancel), then clear the held order. Idle vehicle or an
// orderId parameter naming a different order fails with NO_ORDER_TO_CANCEL.
func (e *Executor) handleCancel(a order.Action, queue *[]order.Node, nodeActions *[]order.Action, inFlight **order.Node, cleaning **order.Action, docking **order.Action, cancelling **order.Action) {
	snap := e.store.Snapshot()
	if wanted, ok := cancelTargetOrderID(a); ok && wanted != snap.OrderID {
		e.failCancel(a, fmt.Sprintf("no active order %q", wanted))
		return
	}
	if vehiclestate.IsIdle(snap) {
		e.failCancel(a, "vehicle is idle; no order to cancel")
		return
	}

	*queue = nil
	*nodeActions = nil
	if *cleaning != nil {
		// spec :477 a running clean is cancelled on the robot, not dropped;
		// stop-write failure keeps it held so a real clean_end still resolves it
		if err := e.cmd.StopCleaning(); err != nil {
			e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "clean stop dispatch failed: "+err.Error())
			return
		}
		*cleaning = nil // its actionState fails via CancelOrder; a late end event is stale
	}
	if *docking != nil {
		// a running dock is aborted on the robot (dock_stop, distinct from undock),
		// not dropped; stop-write failure keeps it held so a real dock end resolves it
		if err := e.cmd.AbortDocking(); err != nil {
			e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "dock stop dispatch failed: "+err.Error())
			return
		}
		*docking = nil // its actionState fails via CancelOrder; a late dock end is stale
	}
	if *inFlight != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusRunning, "")
		if err := e.cmd.StopMovement(); err != nil {
			e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "stop dispatch failed: "+err.Error())
			*inFlight = nil
			return
		}
		*inFlight = nil
		ac := a
		*cancelling = &ac
		return
	}
	e.store.CancelOrder()
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFinished, "")
}

func (e *Executor) failCancel(a order.Action, reason string) {
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, reason)
	e.store.AddError(state.Error{
		ErrorType:        state.ErrorTypeNoOrderToCancel,
		ErrorLevel:       state.ErrorLevelWarning,
		ErrorDescription: reason,
		ErrorReferences: []state.ErrorReference{
			{ReferenceKey: "actionId", ReferenceValue: a.ActionID},
		},
	})
}

func cancelTargetOrderID(a order.Action) (string, bool) {
	for _, p := range a.ActionParameters {
		if p.Key == "orderId" {
			s, ok := p.Value.(string)
			return s, ok
		}
	}
	return "", false
}

// completeCharging resolves the in-flight charging action from its end event;
// events with no matching in-flight action are stale and ignored.
func (e *Executor) completeCharging(ev robotbus.ExecutionEvent, charge **order.Action) {
	if *charge == nil {
		return
	}
	var done *robotbus.ChargingCompleted
	switch (*charge).ActionType {
	case "startCharging":
		done = ev.StartChargingCompleted
	case "stopCharging":
		done = ev.StopChargingCompleted
	}
	if done == nil {
		return
	}
	if done.Success {
		e.store.SetInstantActionStatus((*charge).ActionID, state.ActionStatusFinished, "")
	} else {
		e.store.SetInstantActionStatus((*charge).ActionID, state.ActionStatusFailed, done.Reason)
	}
	*charge = nil
}

// reportUnreachable surfaces a failed traversal as an order-scoped error; the
// store notifies, so the state publishes immediately.
func (e *Executor) reportUnreachable(n order.Node, reason string) {
	e.store.AddError(state.Error{
		ErrorType:        state.ErrorTypeNodeUnreachable,
		ErrorLevel:       state.ErrorLevelWarning,
		ErrorDescription: reason,
		ErrorReferences: []state.ErrorReference{
			{ReferenceKey: "orderId", ReferenceValue: e.orderID},
			{ReferenceKey: "nodeId", ReferenceValue: n.NodeID},
			{ReferenceKey: "sequenceId", ReferenceValue: fmt.Sprintf("%d", n.SequenceID)},
		},
	})
}

func (e *Executor) currentHeading() float64 {
	if p := e.store.Snapshot().MobileRobotPosition; p != nil {
		return p.Theta
	}
	return 0
}

// withinDeviation reports whether the robot's position lies inside the node's
// allowed deviation (default radius when the order specifies none).
func (e *Executor) withinDeviation(n order.Node) bool {
	pos := e.store.Snapshot().MobileRobotPosition
	if pos == nil || n.NodePosition == nil {
		return false
	}
	radius := defaultDeviationXY
	if dev := n.NodePosition.AllowedDeviationXY; dev != nil && dev.A > 0 {
		radius = dev.A
	}
	dx := pos.X - n.NodePosition.X
	dy := pos.Y - n.NodePosition.Y
	return math.Hypot(dx, dy) <= radius
}
