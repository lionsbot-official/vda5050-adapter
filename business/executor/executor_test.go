package executor

import (
	"context"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

type move struct{ x, y, theta float64 }

type cleanStart struct {
	zones  []robotbus.ZoneRef
	bypass bool
}

type fakeCommander struct {
	moves         chan move
	stops         chan struct{}
	pauses        chan struct{}
	resumes       chan struct{}
	chargeStarts  chan struct{}
	chargeStops   chan struct{}
	cleanStarts   chan cleanStart
	cleanStops    chan struct{}
	cleanPauses   chan struct{}
	cleanResumes  chan bool
	dockPauses    chan struct{}
	dockResumes   chan struct{}
	dockStops     chan struct{}
	relocs        chan move
	events        chan robotbus.ExecutionEvent
	moveErr       error
	stopErr       error
	pauseErr      error
	chargeErr     error
	cleanErr      error
	cleanStopErr  error
	cleanPauseErr error
	dockStopErr   error
	relocErr      error
}

func newFakeCommander() *fakeCommander {
	return &fakeCommander{
		moves:        make(chan move, 8),
		stops:        make(chan struct{}, 8),
		pauses:       make(chan struct{}, 8),
		resumes:      make(chan struct{}, 8),
		chargeStarts: make(chan struct{}, 8),
		chargeStops:  make(chan struct{}, 8),
		cleanStarts:  make(chan cleanStart, 8),
		cleanStops:   make(chan struct{}, 8),
		cleanPauses:  make(chan struct{}, 8),
		cleanResumes: make(chan bool, 8),
		dockPauses:   make(chan struct{}, 8),
		dockResumes:  make(chan struct{}, 8),
		dockStops:    make(chan struct{}, 8),
		relocs:       make(chan move, 8),
		events:       make(chan robotbus.ExecutionEvent, 8),
	}
}

func (f *fakeCommander) MoveTo(x, y, theta float64) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moves <- move{x, y, theta}
	return nil
}
func (f *fakeCommander) StopMovement() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stops <- struct{}{}
	return nil
}
func (f *fakeCommander) PauseMovement() error {
	if f.pauseErr != nil {
		return f.pauseErr
	}
	f.pauses <- struct{}{}
	return nil
}
func (f *fakeCommander) ResumeMovement() error {
	if f.pauseErr != nil {
		return f.pauseErr
	}
	f.resumes <- struct{}{}
	return nil
}
func (f *fakeCommander) StartCharging() error {
	if f.chargeErr != nil {
		return f.chargeErr
	}
	f.chargeStarts <- struct{}{}
	return nil
}
func (f *fakeCommander) StopCharging() error {
	if f.chargeErr != nil {
		return f.chargeErr
	}
	f.chargeStops <- struct{}{}
	return nil
}
func (f *fakeCommander) StartCleaning(zones []robotbus.ZoneRef, bypassBlackout bool) error {
	if f.cleanErr != nil {
		return f.cleanErr
	}
	f.cleanStarts <- cleanStart{zones: zones, bypass: bypassBlackout}
	return nil
}
func (f *fakeCommander) StopCleaning() error {
	if f.cleanStopErr != nil {
		return f.cleanStopErr
	}
	f.cleanStops <- struct{}{}
	return nil
}
func (f *fakeCommander) PauseCleaning() error {
	if f.cleanPauseErr != nil {
		return f.cleanPauseErr
	}
	f.cleanPauses <- struct{}{}
	return nil
}
func (f *fakeCommander) ResumeCleaning(bypassBlackout bool) error {
	if f.cleanPauseErr != nil {
		return f.cleanPauseErr
	}
	f.cleanResumes <- bypassBlackout
	return nil
}
func (f *fakeCommander) PauseDocking() error {
	f.dockPauses <- struct{}{}
	return nil
}
func (f *fakeCommander) ResumeDocking() error {
	f.dockResumes <- struct{}{}
	return nil
}
func (f *fakeCommander) AbortDocking() error {
	if f.dockStopErr != nil {
		return f.dockStopErr
	}
	f.dockStops <- struct{}{}
	return nil
}
func (f *fakeCommander) Relocalize(x, y, theta float64) error {
	if f.relocErr != nil {
		return f.relocErr
	}
	f.relocs <- move{x, y, theta}
	return nil
}
func (f *fakeCommander) Events() (<-chan robotbus.ExecutionEvent, error) {
	return f.events, nil
}

func floatPtr(f float64) *float64 { return &f }

// twoNodeOrder: seq-0 start node + one released target with a position.
func singleTargetOrder() order.Order {
	return order.Order{
		OrderID: "o1",
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true,
				NodePosition: &order.NodePosition{X: 1, Y: 2, MapID: "map-1", Theta: floatPtr(1.5)}},
		},
		Edges: []order.Edge{{EdgeID: "e01", SequenceID: 1, Released: true}},
	}
}

func seedStore(s *vehiclestate.Store, atX, atY float64) {
	s.CommitOrderProjection(vehiclestate.OrderProjection{
		OrderID: "o1", LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates:   []state.NodeState{{NodeID: "n1", SequenceID: 2, Released: true}},
		EdgeStates:   []state.EdgeState{{EdgeID: "e01", SequenceID: 1, Released: true}},
		ActionStates: []state.ActionState{},
	})
	s.SetTelemetry(vehiclestate.Telemetry{
		AGVPosition: &state.AGVPosition{X: atX, Y: atY, MapID: "map-1", PositionInitialized: true},
	})
}

func startExecutor(t *testing.T, e *Executor) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = e.Start(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("executor did not stop on ctx cancel")
		}
	})
}

func waitLastNode(t *testing.T, s *vehiclestate.Store, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Snapshot().LastNodeID == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("lastNodeId = %q, want %q", s.Snapshot().LastNodeID, want)
}

func TestExecutor_DispatchesAndCommits(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03) // within default deviation of the target
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())

	select {
	case m := <-cmd.moves:
		if m.x != 1 || m.y != 2 || m.theta != 1.5 {
			t.Errorf("MoveTo = %+v, want {1 2 1.5}", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no MoveTo dispatched")
	}

	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")

	got := store.Snapshot()
	if len(got.NodeStates) != 0 || len(got.EdgeStates) != 0 {
		t.Errorf("states not drained: nodes=%+v edges=%+v", got.NodeStates, got.EdgeStates)
	}
	select {
	case m := <-cmd.moves:
		t.Errorf("unexpected second dispatch %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestExecutor_NoReleasedTarget_NoDispatch(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	o := singleTargetOrder()
	o.Nodes[1].Released = false // horizon only
	e.OrderAccepted(o)

	select {
	case m := <-cmd.moves:
		t.Fatalf("horizon node dispatched: %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExecutor_TraversesMultiNodeBaseInSequence(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	e.settleDelay = 0 // no post-move gap in this sequencing test
	startExecutor(t, e)

	o := order.Order{
		OrderID: "o1",
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true, NodePosition: &order.NodePosition{X: 1, Y: 0, MapID: "map-1", Theta: floatPtr(0)}},
			{NodeID: "n2", SequenceID: 4, Released: true, NodePosition: &order.NodePosition{X: 2, Y: 0, MapID: "map-1", Theta: floatPtr(0)}},
			{NodeID: "n3", SequenceID: 6, Released: false, NodePosition: &order.NodePosition{X: 3, Y: 0, MapID: "map-1", Theta: floatPtr(0)}},
		},
	}
	e.OrderAccepted(o)

	// node 1: dispatched alone, completes, commits
	select {
	case m := <-cmd.moves:
		if m.x != 1 {
			t.Fatalf("first dispatch = %+v, want x=1", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first dispatch")
	}
	select {
	case m := <-cmd.moves:
		t.Fatalf("second node dispatched while first in flight: %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{X: 1, Y: 0, MapID: "map-1", PositionInitialized: true}})
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")

	// node 2 dispatched only after node 1 committed
	select {
	case m := <-cmd.moves:
		if m.x != 2 {
			t.Fatalf("second dispatch = %+v, want x=2", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no second dispatch")
	}
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{X: 2, Y: 0, MapID: "map-1", PositionInitialized: true}})
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n2")

	// horizon n3 must never be dispatched; base exhausted -> idle
	select {
	case m := <-cmd.moves:
		t.Fatalf("horizon node dispatched: %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExecutor_UpdateMidMoveStopsFirst(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no initial dispatch")
	}

	// update arrives mid-move: stop first, no new dispatch until the end event
	update := singleTargetOrder()
	update.OrderUpdateID = 1
	update.Nodes[1] = order.Node{NodeID: "n9", SequenceID: 2, Released: true,
		NodePosition: &order.NodePosition{X: 7, Y: 8, MapID: "map-1", Theta: floatPtr(0)}}
	e.OrderAccepted(update)

	select {
	case <-cmd.stops:
	case <-time.After(2 * time.Second):
		t.Fatal("no StopMovement on mid-move update")
	}
	select {
	case m := <-cmd.moves:
		t.Fatalf("dispatched before the stopped move ended: %+v", m)
	case <-time.After(50 * time.Millisecond):
	}

	// the stopped move's end event must not commit the old target
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false, Reason: "stopped"}}
	select {
	case m := <-cmd.moves:
		if m.x != 7 || m.y != 8 {
			t.Fatalf("replanned dispatch = %+v, want the update's target (7,8)", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch after the stopped move ended")
	}
	if got := store.Snapshot().LastNodeID; got != "n0" {
		t.Errorf("lastNodeId = %q, want n0 (stopped move must not commit)", got)
	}
}

// An order update that only APPENDS nodes beyond the one in flight (its first
// pending node is still the in-flight target) must NOT stop the robot: the
// current move is not stale. Stopping on every append thrashes the robot with
// spurious p2p_stops and strands it mid-route.
func TestExecutor_AppendUpdateKeepsMoving(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	e.settleDelay = 0 // no post-move gap in this append test
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder()) // MoveTo n1 (1,2); n1 now in flight
	select {
	case m := <-cmd.moves:
		if m.x != 1 {
			t.Fatalf("first dispatch = %+v, want x=1", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no initial dispatch")
	}

	// append update: n1 (in flight) restated, plus a new node n2 beyond it
	update := order.Order{
		OrderID:       "o1",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true, NodePosition: &order.NodePosition{X: 1, Y: 2, MapID: "map-1", Theta: floatPtr(1.5)}},
			{NodeID: "n2", SequenceID: 4, Released: true, NodePosition: &order.NodePosition{X: 5, Y: 6, MapID: "map-1", Theta: floatPtr(0)}},
		},
		Edges: []order.Edge{
			{EdgeID: "e01", SequenceID: 1, Released: true},
			{EdgeID: "e12", SequenceID: 3, Released: true},
		},
	}
	e.OrderAccepted(update)

	select {
	case <-cmd.stops:
		t.Fatal("append update stopped the in-flight move (spurious p2p_stop)")
	case <-time.After(100 * time.Millisecond):
	}

	// robot reaches n1 -> commit -> flow on to the appended n2, no re-dispatch of n1
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{X: 1, Y: 2, MapID: "map-1", PositionInitialized: true}})
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")

	select {
	case m := <-cmd.moves:
		if m.x != 5 || m.y != 6 {
			t.Fatalf("next dispatch = %+v, want the appended node (5,6)", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not continue to the appended node")
	}
}

// The real robot rejects a p2p_start sent the instant a move ends — it needs a
// settle gap. After committing a node, the next dispatch waits settleDelay.
func TestExecutor_SettlesBeforeNextMove(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	e.settleDelay = 120 * time.Millisecond
	startExecutor(t, e)

	o := order.Order{
		OrderID: "o1",
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true, NodePosition: &order.NodePosition{X: 1, Y: 0, MapID: "map-1", Theta: floatPtr(0)}},
			{NodeID: "n2", SequenceID: 4, Released: true, NodePosition: &order.NodePosition{X: 2, Y: 0, MapID: "map-1", Theta: floatPtr(0)}},
		},
	}
	e.OrderAccepted(o)

	// the first node dispatches at once — a fresh order to an already-idle robot
	select {
	case m := <-cmd.moves:
		if m.x != 1 {
			t.Fatalf("first dispatch = %+v, want x=1", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first dispatch")
	}

	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{X: 1, Y: 0, MapID: "map-1", PositionInitialized: true}})
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")

	// the next node must NOT dispatch immediately — the robot needs the settle gap
	select {
	case m := <-cmd.moves:
		t.Fatalf("dispatched the next node with no settle gap: %+v", m)
	case <-time.After(30 * time.Millisecond):
	}

	// after the settle elapses, it dispatches the next node
	select {
	case m := <-cmd.moves:
		if m.x != 2 {
			t.Fatalf("post-settle dispatch = %+v, want x=2 (n2)", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next node not dispatched after the settle gap")
	}
}

func TestExecutor_ThetaAbsentUsesCurrentHeading(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	store.SetTelemetry(vehiclestate.Telemetry{
		AGVPosition: &state.AGVPosition{X: 0, Y: 0, Theta: 0.7, MapID: "map-1", PositionInitialized: true},
	})
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	o := singleTargetOrder()
	o.Nodes[1].NodePosition.Theta = nil
	e.OrderAccepted(o)

	select {
	case m := <-cmd.moves:
		if m.theta != 0.7 {
			t.Errorf("theta = %v, want current heading 0.7", m.theta)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch")
	}
}

func hasNodeUnreachable(s *vehiclestate.Store, nodeID string) bool {
	for _, e := range s.Snapshot().Errors {
		if e.ErrorType != state.ErrorTypeNodeUnreachable {
			continue
		}
		for _, r := range e.ErrorReferences {
			if r.ReferenceKey == "nodeId" && r.ReferenceValue == nodeID {
				return true
			}
		}
	}
	return false
}

func waitError(t *testing.T, s *vehiclestate.Store, nodeID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hasNodeUnreachable(s, nodeID) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no NODE_UNREACHABLE error for %s; errors = %+v", nodeID, s.Snapshot().Errors)
}

func TestExecutor_FailedMoveHaltsWithError(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false, Reason: "blocked"}}

	waitError(t, store, "n1")
	if got := store.Snapshot().LastNodeID; got != "n0" {
		t.Errorf("lastNodeId = %q, want n0 (failed move must not commit)", got)
	}
	select {
	case m := <-cmd.moves:
		t.Fatalf("dispatched after failure: %+v (executor must halt)", m)
	case <-time.After(100 * time.Millisecond):
	}

	// a newly accepted order resumes dispatch
	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch after a new order (halt must not be permanent)")
	}
}

func TestExecutor_CompletedOutsideDeviationErrors(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 50, 50) // far from the target
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}

	waitError(t, store, "n1")
}

func TestExecutor_DispatchWriteErrorHaltsWithError(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	cmd.moveErr = context.DeadlineExceeded // any write error
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	waitError(t, store, "n1")
}

func TestExecutor_StopWriteErrorHaltsWithError(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 0, 0)
	cmd := newFakeCommander()
	cmd.stopErr = context.DeadlineExceeded
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch")
	}
	// a genuine replan (different target) mid-move triggers StopMovement, which fails
	replan := singleTargetOrder()
	replan.OrderUpdateID = 1
	replan.Nodes[1] = order.Node{NodeID: "n9", SequenceID: 2, Released: true,
		NodePosition: &order.NodePosition{X: 7, Y: 8, MapID: "map-1", Theta: floatPtr(0)}}
	e.OrderAccepted(replan)

	waitError(t, store, "n1")
	select {
	case m := <-cmd.moves:
		t.Fatalf("dispatched after stop failure: %+v", m)
	case <-time.After(100 * time.Millisecond):
	}

	charge := order.Action{ActionID: "dock-after-failed-stop", ActionType: "startCharging"}
	acceptInstant(store, charge)
	e.InstantAction(charge)
	waitInstantStatus(t, store, charge.ActionID, state.ActionStatusFailed)
	select {
	case <-cmd.chargeStarts:
		t.Fatal("charging dispatched while the original move could still be running")
	default:
	}

	// The reported stop failure synchronizes with waitError before the retry.
	cmd.stopErr = nil
	cancel := order.Action{ActionID: "cancel-after-failed-stop", ActionType: "cancelOrder"}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)
	select {
	case <-cmd.stops:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not retry stopping the original move")
	}
	waitInstantStatus(t, store, cancel.ActionID, state.ActionStatusRunning)
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false}}
	waitInstantStatus(t, store, cancel.ActionID, state.ActionStatusFinished)
}

func TestExecutor_OutsideDeviation_NoCommit(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 50, 50) // far from the target node
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no MoveTo dispatched")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}

	time.Sleep(100 * time.Millisecond)
	if got := store.Snapshot().LastNodeID; got != "n0" {
		t.Errorf("lastNodeId = %q, want n0 (no commit outside deviation)", got)
	}
}
