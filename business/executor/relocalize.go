package executor

import (
	"fmt"
	"github.com/lionsbot-official/vda5050-adapter/business/common/actionparams"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

type relocalizePose = actionparams.Pose

func relocalizeParams(a order.Action) (relocalizePose, error) { return actionparams.Relocalize(a) }

// handleRelocalize serves initPosition: fail while another operation runs,
// on malformed params, or when the target map is not the robot's active map
// (single-map version); otherwise dispatch hot_localize and await the outcome.
func (e *Executor) handleRelocalize(a order.Action, busy bool, reloc **order.Action) {
	mode := e.store.Snapshot().OperatingMode
	if mode != state.OperatingModeAutomatic && mode != state.OperatingModeSemiAutomatic {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "master control is not permitted in current mode")
		return
	}
	if busy || *reloc != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "another operation is executing")
		return
	}
	pose, err := relocalizeParams(a)
	if err != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, err.Error())
		return
	}
	// The robot's hot_localize carries no map id and cannot switch maps; the
	// driver can only convert coordinates for the loaded map. Active map comes
	// from the reported pose, present even while not localized.
	active := e.store.Snapshot().AGVPosition
	if active == nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "cannot relocalize: the robot's active map is unknown")
		return
	}
	if active.MapID != pose.MapID {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed,
			fmt.Sprintf("mapId %q is not the robot's active map %q (single-map relocalization)", pose.MapID, active.MapID))
		return
	}
	if err := e.cmd.Relocalize(pose.X, pose.Y, pose.Theta); err != nil {
		e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusFailed, "relocalize dispatch failed: "+err.Error())
		return
	}
	e.store.SetInstantActionStatus(a.ActionID, state.ActionStatusRunning, "")
	ac := a
	*reloc = &ac
}

// completeRelocalize resolves the in-flight relocalization from its hot_localize
// feedback; a nil in-flight action means the event is stale and is ignored.
func (e *Executor) completeRelocalize(done *robotbus.RelocalizeCompleted, reloc **order.Action) {
	if *reloc == nil {
		return
	}
	if done.Success {
		pose, _ := relocalizeParams(**reloc)
		e.store.CompleteRelocalization((*reloc).ActionID, state.AGVPosition{X: pose.X, Y: pose.Y, Theta: pose.Theta, MapID: pose.MapID, PositionInitialized: true}, pose.LastNodeID)
	} else {
		e.store.SetInstantActionStatus((*reloc).ActionID, state.ActionStatusFailed,
			fmt.Sprintf("relocalization failed (localization score %.2f)", done.Score))
	}
	*reloc = nil
}
