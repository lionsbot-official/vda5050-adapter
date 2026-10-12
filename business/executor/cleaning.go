package executor

import (
	"github.com/lionsbot-official/vda5050-adapter/business/common/actionparams"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// startCleaning dispatches a startCleaning node action upstream and tracks it
// as the in-flight clean; any failure lands in the action's state, never a
// silent drop (a stuck WAITING action wedges isIdle forever).
func (e *Executor) startCleaning(a order.Action, cleaning **order.Action) {
	if *cleaning != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, "another operation is executing")
		return
	}
	zones, bypass, err := cleaningParams(a)
	if err != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, err.Error())
		return
	}
	if err := e.cmd.StartCleaning(zones, bypass); err != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, "clean dispatch failed: "+err.Error())
		return
	}
	e.operationMap = e.currentMap()
	e.store.SetActionStatus(a.ActionID, state.ActionStatusRunning, "")
	ac := a
	*cleaning = &ac
}

// completeCleaning resolves the in-flight clean from its end event; events
// with no matching in-flight clean are stale and ignored.
func (e *Executor) completeCleaning(done *robotbus.CleanCompleted, cleaning **order.Action) {
	if *cleaning == nil {
		return
	}
	if done.Success {
		e.store.SetActionStatus((*cleaning).ActionID, state.ActionStatusFinished, "")
	} else {
		e.store.SetActionStatus((*cleaning).ActionID, state.ActionStatusFailed, done.Reason)
	}
	*cleaning = nil
}

// cleaningParams is shared with intake so invalid orders never replace the plan.
func cleaningParams(a order.Action) ([]robotbus.ZoneRef, bool, error) {
	return actionparams.Cleaning(a)
}
