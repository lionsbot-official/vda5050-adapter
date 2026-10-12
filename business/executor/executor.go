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
	store        *vehiclestate.Store
	cmd          robotbus.RobotCommander
	orders       chan order.Order
	instants     chan order.Action
	operationMap string
	orderID      string        // current order, for error references (sequencer goroutine only)
	paused       bool          // pause mode; nothing dispatches while set (sequencer goroutine only)
	settling     bool          // settle gap after a move; nothing dispatches while set
	settleDelay  time.Duration // gap before dispatching the node after a completed move; 0 in tests
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
	var pausing *order.Action    // pause activation awaits a noninterruptible auto-undock
	var cancelling *order.Action // cancelOrder awaiting the interrupted move's end event
	interruptedAction := false   // a stopped clean/dock awaits its matching end event
	stopping := false            // a replan interrupted the in-flight move; await its end event
	undocking := false           // auto-undock in flight; first move held until it completes
	var settleC <-chan time.Time // fires when the post-move settle gap elapses; then dispatch

	for {
		if e.store.OrderCleared() {
			queue, nodeActions = nil, nil
			settleC = nil
			e.settling = false
		}
		if e.store.ModeChangePending() {
			if inFlight == nil && cleaning == nil && docking == nil && charge == nil && reloc == nil && !stopping && !undocking && cancelling == nil {
				if pausing != nil {
					e.store.SetInstantActionStatus(pausing.ActionID, state.ActionStatusFailed, "operating mode changed")
					pausing = nil
				}
				e.store.CompleteModeTransition()
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-e.store.ReadinessChanges():
			if cancelling == nil && !interruptedAction {
				if inFlight != nil && !e.readyToDrive(inFlight.NodePosition.MapID) {
					if err := e.cmd.StopMovement(); err != nil {
						e.reportUnreachable(*inFlight, "safety stop dispatch failed: "+err.Error())
						continue
					}
					queue = append([]order.Node{*inFlight}, queue...)
					inFlight = nil
					stopping = true
				} else if !e.readyToDrive(e.operationMap) {
					var err error
					switch {
					case cleaning != nil:
						err = e.cmd.StopCleaning()
						if err != nil {
							e.store.SetActionStatus(cleaning.ActionID, state.ActionStatusRunning, "safety stop dispatch failed: "+err.Error())
						}
						interruptedAction = err == nil
					case docking != nil && docking.ActionType == "startCharging":
						err = e.cmd.AbortDocking()
						if err != nil {
							e.store.SetActionStatus(docking.ActionID, state.ActionStatusRunning, "safety stop dispatch failed: "+err.Error())
						}
						interruptedAction = err == nil
					case charge != nil && charge.ActionType == "startCharging":
						err = e.cmd.AbortDocking()
						if err != nil {
							e.store.SetInstantActionStatus(charge.ActionID, state.ActionStatusRunning, "safety stop dispatch failed: "+err.Error())
						}
						interruptedAction = err == nil
					}
				}
			}
			if stopping || undocking || cancelling != nil || interruptedAction {
				continue
			}
			e.beginPlan(&queue, &inFlight, &undocking)
			e.beginNodeActions(&nodeActions, &cleaning, &docking)
		case <-settleC:
			settleC = nil
			e.settling = false
			e.dispatchNext(&queue, &inFlight)
		case a := <-e.instants:
			pending := false
			for _, tracked := range e.store.Snapshot().InstantActionStates {
				if tracked.ActionID == a.ActionID && tracked.ActionStatus == state.ActionStatusWaiting {
					pending = true
					break
				}
			}
			if !pending {
				continue
			}
			switch a.ActionType {
			case "cancelOrder":
				e.handleCancel(a, &queue, &nodeActions, &inFlight, &cleaning, &docking, &cancelling, undocking || stopping || interruptedAction)
			case "startPause":
				if undocking {
					if pausing != nil {
						e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "pause activation already pending")
						continue
					}
					pausing = &a
					e.paused = true
					e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusRunning, "")
					continue
				}
				e.handlePause(a, inFlight, cleaning, docking, charge)
			case "stopPause":
				if pausing != nil {
					e.store.SetInstantActionStatus(pausing.ActionID, state.ActionStatusFailed, "pause activation superseded by stopPause")
					pausing = nil
				}
				if stopping || interruptedAction || cancelling != nil || undocking {
					e.paused = false
					e.store.SetPaused(false)
					e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFinished, "")
					continue
				}
				e.handleResume(a, &queue, &inFlight, &undocking, cleaning, docking, charge)
				e.beginNodeActions(&nodeActions, &cleaning, &docking)
			case "initializePosition":
				busy := inFlight != nil || len(queue) > 0 || stopping || undocking || cancelling != nil || len(nodeActions) > 0 || e.paused || cleaning != nil || docking != nil || charge != nil
				e.handleRelocalize(a, busy, &reloc)
			default:
				busy := inFlight != nil || len(queue) > 0 || stopping || undocking || cancelling != nil || len(nodeActions) > 0 || e.paused || cleaning != nil || docking != nil || reloc != nil
				e.handleInstantAction(a, busy, &charge)
			}
		case o := <-e.orders:
			if e.store.OrderCancelled() || e.store.OrderCleared() || e.store.Snapshot().OrderID != o.OrderID {
				continue
			}
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
					continue
				}
				inFlight = nil
				stopping = true
				continue
			}
			if !stopping && !interruptedAction {
				e.beginPlan(&queue, &inFlight, &undocking)
				e.beginNodeActions(&nodeActions, &cleaning, &docking)
			}
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if pausing != nil && undocking && ev.StopChargingCompleted != nil {
				e.store.SetPaused(true)
				e.store.SetInstantActionStatus(pausing.ActionID, state.ActionStatusFinished, "")
				pausing = nil
			}
			if interruptedAction && cancelling == nil {
				matched := false
				switch {
				case cleaning != nil && ev.CleanCompleted != nil:
					e.store.SetActionStatus(cleaning.ActionID, state.ActionStatusFailed, "safety interlock interrupted action")
					cleaning = nil
					matched = true
				case docking != nil && ev.StartChargingCompleted != nil:
					e.store.SetActionStatus(docking.ActionID, state.ActionStatusFailed, "safety interlock interrupted action")
					docking = nil
					matched = true
				case charge != nil && ev.StartChargingCompleted != nil:
					e.store.SetInstantActionStatus(charge.ActionID, state.ActionStatusFailed, "safety interlock interrupted action")
					charge = nil
					matched = true
				}
				if matched {
					interruptedAction = false
					continue
				}
			}
			if cancelling != nil {
				matched := false
				switch {
				case cleaning != nil && ev.CleanCompleted != nil:
					e.store.SetActionStatus(cleaning.ActionID, state.ActionStatusFailed, "order cancelled")
					cleaning = nil
					matched = true
				case docking != nil && docking.ActionType == "startCharging" && ev.StartChargingCompleted != nil:
					e.store.SetActionStatus(docking.ActionID, state.ActionStatusFailed, "order cancelled")
					docking = nil
					matched = true
				case docking != nil && docking.ActionType == "stopCharging" && ev.StopChargingCompleted != nil:
					e.completeDocking(ev, &docking)
					matched = true
				case undocking && ev.StopChargingCompleted != nil:
					undocking = false
					matched = true
				case (inFlight != nil || stopping) && ev.MoveCompleted != nil:
					inFlight = nil
					stopping = false
					matched = true
				}
				if matched {
					interruptedAction = false
					if cleaning == nil && docking == nil && !undocking && inFlight == nil && !stopping {
						e.store.CancelOrder()
						e.store.SetInstantActionStatus(cancelling.ActionID, state.ActionStatusFinished, "")
						cancelling = nil
					}
					continue
				}
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
	snapshot := e.store.Snapshot()
	last := snapshot.LastNodeSequenceID
	waiting := map[string]bool{}
	for _, a := range snapshot.ActionStates {
		waiting[a.ActionID] = a.ActionStatus == state.ActionStatusWaiting
	}
	var acts []order.Action
	for _, n := range o.Nodes {
		if n.Released && int(n.SequenceID) <= last {
			for _, a := range n.Actions {
				if waiting[a.ActionID] {
					acts = append(acts, a)
				}
			}
		}
	}
	return acts
}

// beginNodeActions runs the held current-node actions off a single-node order:
// startCleaning, or startCharging/stopCharging as a self-navigating dock/undock.
func (e *Executor) beginNodeActions(acts *[]order.Action, cleaning **order.Action, docking **order.Action) {
	if e.paused || !e.readyToDrive("") {
		return
	}
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
	if len(*queue) == 0 || *inFlight != nil || e.paused || e.settling || !e.readyToDrive((*queue)[0].NodePosition.MapID) {
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
func (e *Executor) handlePause(a order.Action, inFlight *order.Node, cleaning *order.Action, docking *order.Action, charge *order.Action) {
	var err error
	switch {
	case cleaning != nil:
		err = e.cmd.PauseCleaning()
	case docking != nil && docking.ActionType == "startCharging":
		err = e.cmd.PauseDocking()
	case charge != nil && charge.ActionType == "startCharging":
		err = e.cmd.PauseDocking()
	case inFlight != nil:
		err = e.cmd.PauseMovement()
	}
	if err != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "pause dispatch failed: "+err.Error())
		return
	}
	if cleaning != nil {
		e.store.SetActionStatus(cleaning.ActionID, state.ActionStatusPaused, "")
	}
	if docking != nil && docking.ActionType == "startCharging" {
		e.store.SetActionStatus(docking.ActionID, state.ActionStatusPaused, "")
	}
	if charge != nil && charge.ActionType == "startCharging" {
		e.store.SetInstantActionStatus(charge.ActionID, state.ActionStatusPaused, "")
	}
	e.paused = true
	e.store.SetPaused(true)
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFinished, "")
}

// handleResume deactivates pause mode: resume the in-flight operation by kind
// (clean_continue with the order's bypassBlackout, else p2p_continue), then
// dispatch whatever was held. Resuming a non-paused vehicle is a no-op FINISHED.
func (e *Executor) handleResume(a order.Action, queue *[]order.Node, inFlight **order.Node, undocking *bool, cleaning *order.Action, docking *order.Action, charge *order.Action) {
	if e.paused && !e.readyToDrive("") {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "robot is not ready for automatic driving")
		return
	}
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
		case docking != nil && docking.ActionType == "startCharging":
			err = e.cmd.ResumeDocking()
		case charge != nil && charge.ActionType == "startCharging":
			err = e.cmd.ResumeDocking()
		case *inFlight != nil:
			err = e.cmd.ResumeMovement()
		}
		if err != nil {
			e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "resume dispatch failed: "+err.Error())
			return
		}
	}
	if cleaning != nil {
		e.store.SetActionStatus(cleaning.ActionID, state.ActionStatusRunning, "")
	}
	if docking != nil && docking.ActionType == "startCharging" {
		e.store.SetActionStatus(docking.ActionID, state.ActionStatusRunning, "")
	}
	if charge != nil && charge.ActionType == "startCharging" {
		e.store.SetInstantActionStatus(charge.ActionID, state.ActionStatusRunning, "")
	}
	e.paused = false
	e.store.SetPaused(false)
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFinished, "")
	e.beginPlan(queue, inFlight, undocking)
}

func (e *Executor) readyToDrive(mapID string) bool {
	snapshot := e.store.Snapshot()
	if snapshot.SafetyState.ActiveEmergencyStop != state.ActiveEmergencyStopNone || snapshot.SafetyState.FieldViolation {
		return false
	}
	for _, err := range snapshot.Errors {
		if err.ErrorLevel == state.ErrorLevelFatal || err.ErrorLevel == state.ErrorLevelCritical {
			return false
		}
	}
	mode := e.store.RequestedOperatingMode()
	return snapshot.MobileRobotPosition != nil && snapshot.MobileRobotPosition.Localized &&
		(mapID == "" || snapshot.MobileRobotPosition.MapID == mapID) &&
		(mode == state.OperatingModeAutomatic || mode == state.OperatingModeSemiAutomatic)
}

func (e *Executor) dispatchNext(queue *[]order.Node, inFlight **order.Node) {
	if e.paused || e.settling || *inFlight != nil || len(*queue) == 0 || !e.readyToDrive((*queue)[0].NodePosition.MapID) {
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
	if !e.readyToDrive("") {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "robot is not ready for automatic driving")
		return
	}
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
	e.operationMap = e.currentMap()
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusRunning, "")
	ac := a
	*charge = &ac
}

// A successful stop write is not evidence that movement or an action has stopped.
func (e *Executor) handleCancel(a order.Action, queue *[]order.Node, nodeActions *[]order.Action, inFlight **order.Node, cleaning **order.Action, docking **order.Action, cancelling **order.Action, waiting bool) {
	if *cancelling != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "order cancellation already running")
		return
	}
	snap := e.store.Snapshot()
	if wanted, ok := cancelTargetOrderID(a); ok && wanted != snap.OrderID {
		e.failCancel(a, fmt.Sprintf("no active order %q", wanted))
		return
	}
	if vehiclestate.IsIdle(snap) {
		e.failCancel(a, "vehicle is idle; no order to cancel")
		return
	}
	var err error
	switch {
	case !waiting && *cleaning != nil:
		err = e.cmd.StopCleaning()
	case !waiting && *docking != nil && (*docking).ActionType == "startCharging":
		err = e.cmd.AbortDocking()
	case !waiting && *inFlight != nil:
		err = e.cmd.StopMovement()
	}
	if err != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "stop dispatch failed: "+err.Error())
		return
	}
	e.store.BeginOrderCancellation()
	*queue = nil
	*nodeActions = nil
	if *cleaning != nil || *docking != nil || *inFlight != nil || waiting {
		ac := a
		*cancelling = &ac
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusRunning, "")
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

func (e *Executor) currentMap() string {
	if pose := e.store.Snapshot().MobileRobotPosition; pose != nil {
		return pose.MapID
	}
	return ""
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
	if pos == nil || !pos.Localized || n.NodePosition == nil || pos.MapID != n.NodePosition.MapID {
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
