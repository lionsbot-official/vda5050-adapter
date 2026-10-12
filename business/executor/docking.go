package executor

import (
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// startDocking dispatches a node-scoped dock (startCharging) or undock
// (stopCharging) and tracks it as the in-flight docking op; dock_start/
// undock_start self-navigate, so no move is queued. Any failure lands in the
// action's state, never a silent drop.
func (e *Executor) startDocking(a order.Action, docking **order.Action) {
	if *docking != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, "another operation is executing")
		return
	}
	var err error
	switch a.ActionType {
	case "startCharging":
		err = e.cmd.StartCharging()
	case "stopCharging":
		err = e.cmd.StopCharging()
	}
	if err != nil {
		e.store.SetActionStatus(a.ActionID, state.ActionStatusFailed, "dock dispatch failed: "+err.Error())
		return
	}
	e.store.SetActionStatus(a.ActionID, state.ActionStatusRunning, "")
	ac := a
	*docking = &ac
}

// completeDocking resolves the in-flight node dock/undock from its end event;
// events with no matching in-flight dock are stale and ignored.
func (e *Executor) completeDocking(ev robotbus.ExecutionEvent, docking **order.Action) {
	if *docking == nil {
		return
	}
	var done *robotbus.ChargingCompleted
	switch (*docking).ActionType {
	case "startCharging":
		done = ev.StartChargingCompleted
	case "stopCharging":
		done = ev.StopChargingCompleted
	}
	if done == nil {
		return
	}
	if done.Success {
		e.store.SetActionStatus((*docking).ActionID, state.ActionStatusFinished, "")
	} else {
		e.store.SetActionStatus((*docking).ActionID, state.ActionStatusFailed, done.Reason)
	}
	*docking = nil
}
