package executor

import (
	"errors"
	"fmt"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// relocalizePose is the world-frame pose an initializePosition action carries.
// LastNodeID is accepted but unused (the robot command takes only the pose).
type relocalizePose struct {
	X, Y, Theta float64
	MapID       string
	LastNodeID  string
}

// relocalizeParams extracts the initializePosition arguments (VDA5050_EN.md:714:
// x/y/theta metres+radians, mapId, lastNodeId) as they arrive off the wire.
// x/y/theta and a non-empty mapId are required; a zero coordinate is valid.
func relocalizeParams(a order.Action) (relocalizePose, error) {
	var pose relocalizePose
	var haveX, haveY, haveTheta bool
	for _, p := range a.ActionParameters {
		switch p.Key {
		case "x":
			f, ok := p.Value.(float64)
			if !ok {
				return relocalizePose{}, fmt.Errorf("x parameter is %T, want number", p.Value)
			}
			pose.X, haveX = f, true
		case "y":
			f, ok := p.Value.(float64)
			if !ok {
				return relocalizePose{}, fmt.Errorf("y parameter is %T, want number", p.Value)
			}
			pose.Y, haveY = f, true
		case "theta":
			f, ok := p.Value.(float64)
			if !ok {
				return relocalizePose{}, fmt.Errorf("theta parameter is %T, want number", p.Value)
			}
			pose.Theta, haveTheta = f, true
		case "mapId":
			s, ok := p.Value.(string)
			if !ok {
				return relocalizePose{}, fmt.Errorf("mapId parameter is %T, want string", p.Value)
			}
			pose.MapID = s
		case "lastNodeId":
			if s, ok := p.Value.(string); ok {
				pose.LastNodeID = s // accepted, unused
			}
		}
	}
	if !haveX || !haveY || !haveTheta {
		return relocalizePose{}, errors.New("initializePosition requires x, y and theta")
	}
	if pose.MapID == "" {
		return relocalizePose{}, errors.New("initializePosition requires mapId")
	}
	return pose, nil
}

// handleRelocalize serves initializePosition: fail while another operation runs,
// on malformed params, or when the target map is not the robot's active map
// (single-map version); otherwise dispatch hot_localize and await the outcome.
func (e *Executor) handleRelocalize(a order.Action, busy bool, reloc **order.Action) {
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
	// from the reported pose, present even while delocalized.
	active := e.store.Snapshot().MobileRobotPosition
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
		e.store.SetInstantActionStatus((*reloc).ActionID, state.ActionStatusFinished, "")
	} else {
		e.store.SetInstantActionStatus((*reloc).ActionID, state.ActionStatusFailed,
			fmt.Sprintf("relocalization failed (localization score %.2f)", done.Score))
	}
	*reloc = nil
}
