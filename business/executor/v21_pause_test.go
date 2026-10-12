package executor

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestV21PauseReportsCleaningActionPaused(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	store.SetActionStatus("a1", state.ActionStatusRunning, "")
	e := New(store, newFakeCommander())
	clean := cleaningOrder().Nodes[0].Actions[0]
	pause := order.Action{ActionID: "pause", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.handlePause(pause, nil, &clean, nil)
	if got := store.Snapshot().ActionStates[0].ActionStatus; got != state.ActionStatusPaused {
		t.Fatalf("paused cleaning action status=%s", got)
	}
	resume := order.Action{ActionID: "resume", ActionType: "stopPause"}
	acceptInstant(store, resume)
	var queue []order.Node
	var inFlight *order.Node
	undocking := false
	e.handleResume(resume, &queue, &inFlight, &undocking, &clean, nil)
	if got := store.Snapshot().ActionStates[0].ActionStatus; got != state.ActionStatusRunning {
		t.Fatalf("resumed cleaning action status=%s", got)
	}
}

func TestV21PauseReportsInstantDockPausedAndRejectsUndock(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	dock := order.Action{ActionID: "dock", ActionType: "startCharging"}
	acceptInstant(store, dock)
	store.SetInstantActionStatus("dock", state.ActionStatusRunning, "")
	pause := order.Action{ActionID: "pause", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.handlePause(pause, nil, nil, &dock)
	waitInstantStatus(t, store, "dock", state.ActionStatusPaused)
	resume := order.Action{ActionID: "resume", ActionType: "stopPause"}
	acceptInstant(store, resume)
	var queue []order.Node
	var inFlight *order.Node
	undocking := false
	e.handleResume(resume, &queue, &inFlight, &undocking, nil, &dock)
	waitInstantStatus(t, store, "dock", state.ActionStatusRunning)
	dock.ActionType = "stopCharging"
	pause.ActionID = "pause-undock"
	acceptInstant(store, pause)
	e.handlePause(pause, nil, nil, &dock)
	waitInstantStatus(t, store, "pause-undock", state.ActionStatusFailed)
	if e.paused {
		t.Fatal("noninterruptible undock falsely marked paused")
	}
}
