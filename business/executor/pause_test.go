package executor

import (
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func threeNodeOrder() order.Order {
	theta := 0.0
	pos := func(x float64) *order.NodePosition {
		return &order.NodePosition{X: x, Y: 2, MapID: "map-1", Theta: &theta}
	}
	return order.Order{
		OrderID: "o1",
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true, NodePosition: pos(1)},
			{NodeID: "n2", SequenceID: 4, Released: true, NodePosition: pos(5)},
		},
		Edges: []order.Edge{
			{EdgeID: "e01", SequenceID: 1, Released: true},
			{EdgeID: "e12", SequenceID: 3, Released: true},
		},
	}
}

func waitPaused(t *testing.T, s *vehiclestate.Store, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := s.Snapshot().Paused; p != nil && *p == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("paused never became %v", want)
}

func TestExecutor_PauseMidMoveHoldsNextDispatch(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(threeNodeOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no first move")
	}

	pause := order.Action{ActionID: "p1", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.InstantAction(pause)

	select {
	case <-cmd.pauses:
	case <-time.After(2 * time.Second):
		t.Fatal("no p2p_pause dispatched")
	}
	waitPaused(t, store, true)
	waitInstantStatus(t, store, "p1", state.ActionStatusFinished)

	// robot reports the paused move's end (it stopped at n1): traversal commits
	// but the NEXT node must not dispatch while paused
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")
	select {
	case m := <-cmd.moves:
		t.Fatalf("move %+v dispatched while paused", m)
	case <-time.After(50 * time.Millisecond):
	}

	resume := order.Action{ActionID: "p2", ActionType: "stopPause"}
	acceptInstant(store, resume)
	e.InstantAction(resume)
	waitPaused(t, store, false)
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("held node never dispatched after resume")
	}
}

func TestExecutor_ResumeSendsContinueForInFlightMove(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move")
	}

	pause := order.Action{ActionID: "p3", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.InstantAction(pause)
	select {
	case <-cmd.pauses:
	case <-time.After(2 * time.Second):
		t.Fatal("no pause")
	}

	resume := order.Action{ActionID: "p4", ActionType: "stopPause"}
	acceptInstant(store, resume)
	e.InstantAction(resume)
	select {
	case <-cmd.resumes:
	case <-time.After(2 * time.Second):
		t.Fatal("no p2p_continue for the in-flight move")
	}
	waitInstantStatus(t, store, "p4", state.ActionStatusFinished)
}

func TestExecutor_OrderAcceptedWhilePausedHeld(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	pause := order.Action{ActionID: "p5", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.InstantAction(pause)
	waitPaused(t, store, true)

	e.OrderAccepted(singleTargetOrder())
	select {
	case m := <-cmd.moves:
		t.Fatalf("move %+v dispatched while paused", m)
	case <-time.After(50 * time.Millisecond):
	}

	resume := order.Action{ActionID: "p6", ActionType: "stopPause"}
	acceptInstant(store, resume)
	e.InstantAction(resume)
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("order never dispatched after resume")
	}
}

func TestExecutor_PauseWriteErrorFails(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03)
	cmd := newFakeCommander()
	cmd.pauseErr = errChargeDown
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move")
	}

	pause := order.Action{ActionID: "p7", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.InstantAction(pause)
	waitInstantStatus(t, store, "p7", state.ActionStatusFailed)
	if p := store.Snapshot().Paused; p != nil && *p {
		t.Fatal("vehicle marked paused though the pause command failed")
	}
}
