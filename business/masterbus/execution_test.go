package masterbus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/executor"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func TestHandleOrder_AcceptHookReceivesOrder(t *testing.T) {
	bus, _ := newTestBus(t)
	got := make(chan order.Order, 2)
	bus.SetOrderAcceptedHook(func(o order.Order) { got <- o })

	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-hook", OrderUpdateID: 0,
		Nodes: []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}},
		Edges: []order.Edge{},
	}))

	select {
	case o := <-got:
		if o.OrderID != "o-hook" {
			t.Errorf("hook order = %q, want o-hook", o.OrderID)
		}
	case <-time.After(time.Second):
		t.Fatal("accept hook not called")
	}
}

func TestHandleOrder_RejectDoesNotCallHook(t *testing.T) {
	bus, _ := newTestBus(t)
	got := make(chan order.Order, 2)
	bus.SetOrderAcceptedHook(func(o order.Order) { got <- o })

	bus.handleOrder([]byte(`{"orderId":"bad","orderUpdateId":0,"nodes":[],"edges":[]}`))

	select {
	case o := <-got:
		t.Fatalf("hook called for rejected order: %+v", o)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleOrder_UpdatePreservesTraversedLastNode(t *testing.T) {
	bus, ft := newTestBus(t)
	theta := 0.0
	pos := func(x float64) *order.NodePosition {
		return &order.NodePosition{X: x, Y: 0, MapID: "m", Theta: &theta}
	}
	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-keep", OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true, NodePosition: pos(1)},
			{NodeID: "n2", SequenceID: 4, Released: true, NodePosition: pos(2)},
		},
		Edges: []order.Edge{
			{EdgeID: "e01", SequenceID: 1, Released: true},
			{EdgeID: "e12", SequenceID: 3, Released: true},
		},
	}))

	// the robot traverses to n1
	bus.robot.store.CommitTraversal("n1", 2)

	// a valid stitched update extends from the decision point n2@4
	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-keep", OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n2", SequenceID: 4, Released: true, NodePosition: pos(2)},
			{NodeID: "n3", SequenceID: 6, Released: true, NodePosition: pos(3)},
		},
		Edges: []order.Edge{{EdgeID: "e23", SequenceID: 5, Released: true}},
	}))

	got := lastState(t, bus, ft)
	if got.LastNodeID != "n1" || got.LastNodeSequenceID != 2 {
		t.Errorf("lastNode after update = %s/%d, want preserved n1/2 (traversal must survive rebuild)",
			got.LastNodeID, got.LastNodeSequenceID)
	}
	for _, n := range got.NodeStates {
		if n.SequenceID <= 2 {
			t.Errorf("traversed node resurrected in nodeStates: %+v", n)
		}
	}
}

func TestHandleOrder_StartNodePositionCheck(t *testing.T) {
	theta := 0.0
	cases := []struct {
		name       string
		robotPos   *state.AGVPosition
		nodePos    *order.NodePosition
		wantAccept bool
	}{
		{"outside deviation rejected", &state.AGVPosition{X: 10, Y: 10, MapID: "m", PositionInitialized: true},
			&order.NodePosition{X: 0, Y: 0, MapID: "m", Theta: &theta}, false},
		{"within deviation accepted", &state.AGVPosition{X: 0.2, Y: 0.1, MapID: "m", PositionInitialized: true},
			&order.NodePosition{X: 0, Y: 0, MapID: "m", Theta: &theta}, true},
		// Uninitialized coordinates cannot establish a reachable first node.
		{"uninitialized pose rejected", &state.AGVPosition{X: 10, Y: 10, MapID: "m", PositionInitialized: false},
			&order.NodePosition{X: 0, Y: 0, MapID: "m", Theta: &theta}, false},
		{"wider allowed deviation accepted", &state.AGVPosition{X: 2, Y: 0, MapID: "m", PositionInitialized: true},
			&order.NodePosition{X: 0, Y: 0, MapID: "m", Theta: &theta, AllowedDeviationXY: new(3.0)}, true},
		{"no node position accepted", &state.AGVPosition{X: 10, Y: 10, MapID: "m", PositionInitialized: true}, nil, true},
		{"unknown robot position rejected", nil,
			&order.NodePosition{X: 0, Y: 0, MapID: "m", Theta: &theta}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus, ft := newTestBus(t)
			bus.robot.store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: tc.robotPos})
			bus.handleOrder(mustOrderBytes(t, order.Order{
				OrderID: "o-pos", OrderUpdateID: 0,
				Nodes: []order.Node{{NodeID: "n0", SequenceID: 0, Released: true, NodePosition: tc.nodePos}},
				Edges: []order.Edge{},
			}))
			got := lastState(t, bus, ft)
			if tc.wantAccept {
				if got.OrderID != "o-pos" {
					t.Errorf("order not accepted: state orderId = %q", got.OrderID)
				}
				if hasError(got, state.ErrorTypeOrderError) {
					t.Error("unexpected START_NODE_OUT_OF_RANGE on accepted order")
				}
			} else {
				if got.OrderID == "o-pos" {
					t.Error("order accepted, want rejected (robot outside start-node deviation)")
				}
				if !hasError(got, state.ErrorTypeOrderError) {
					t.Errorf("errors = %+v, want START_NODE_OUT_OF_RANGE", got.Errors)
				}
			}
		})
	}
}

type syncTransport struct {
	mu     sync.Mutex
	n      int
	states []state.State
}

func (s *syncTransport) Publish(topic string, payload any, qos byte, retained bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if st, ok := payload.(state.State); ok {
		s.states = append(s.states, st)
	}
	return nil
}

func (s *syncTransport) lastNodeSeen(want string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.states {
		if st.LastNodeID == want {
			return true
		}
	}
	return false
}

func (s *syncTransport) instantStatusSeen(actionID string, status state.ActionStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.states {
		for _, as := range st.InstantActionStates {
			if as.ActionID == actionID && as.ActionStatus == status {
				return true
			}
		}
	}
	return false
}

func (s *syncTransport) actionStatusSeen(actionID string, status state.ActionStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.states {
		for _, as := range st.ActionStates {
			if as.ActionID == actionID && as.ActionStatus == status {
				return true
			}
		}
	}
	return false
}
func (s *syncTransport) Subscribe(topic string, qos byte) (chan []byte, error) {
	return make(chan []byte), nil
}
func (s *syncTransport) Connect() error      { return nil }
func (s *syncTransport) Disconnect()         {}
func (s *syncTransport) SetOnConnect(func()) {}

func (s *syncTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

type e2eCommander struct {
	moves        chan struct{ x, y, theta float64 }
	chargeStarts chan struct{}
	cleanStarts  chan []robotbus.ZoneRef
	events       chan robotbus.ExecutionEvent
}

func (f *e2eCommander) MoveTo(x, y, theta float64) error {
	f.moves <- struct{ x, y, theta float64 }{x, y, theta}
	return nil
}
func (f *e2eCommander) StopMovement() error   { return nil }
func (f *e2eCommander) PauseMovement() error  { return nil }
func (f *e2eCommander) ResumeMovement() error { return nil }
func (f *e2eCommander) StartCharging() error {
	if f.chargeStarts != nil {
		f.chargeStarts <- struct{}{}
	}
	return nil
}
func (f *e2eCommander) StopCharging() error { return nil }
func (f *e2eCommander) StartCleaning(zones []robotbus.ZoneRef, bypassBlackout bool) error {
	if f.cleanStarts != nil {
		f.cleanStarts <- zones
	}
	return nil
}
func (f *e2eCommander) StopCleaning() error                  { return nil }
func (f *e2eCommander) PauseCleaning() error                 { return nil }
func (f *e2eCommander) ResumeCleaning(bypass bool) error     { return nil }
func (f *e2eCommander) PauseDocking() error                  { return nil }
func (f *e2eCommander) ResumeDocking() error                 { return nil }
func (f *e2eCommander) AbortDocking() error                  { return nil }
func (f *e2eCommander) Relocalize(x, y, theta float64) error { return nil }
func (f *e2eCommander) Events() (<-chan robotbus.ExecutionEvent, error) {
	return f.events, nil
}

func TestOrderExecutionEndToEnd(t *testing.T) {
	st := &syncTransport{}
	store := vehiclestate.New()
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	bus, err := NewWithTransport(st, store, "TESTBOT", "2.1.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	cmd := &e2eCommander{
		moves:  make(chan struct{ x, y, theta float64 }, 4),
		events: make(chan robotbus.ExecutionEvent, 4),
	}
	exec := executor.New(store, cmd)
	bus.SetOrderAcceptedHook(exec.OrderAccepted)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = exec.Start(ctx) }()
	go bus.StartChangePublisher(ctx)

	// robot sits at the target already (within default deviation)
	store.SetTelemetry(vehiclestate.Telemetry{
		AGVPosition: &state.AGVPosition{X: 1.0, Y: 2.0, MapID: "map-1", PositionInitialized: true},
	})

	theta := 1.5
	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-e2e", OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true,
				NodePosition: &order.NodePosition{X: 1, Y: 2, MapID: "map-1", Theta: &theta}},
		},
		Edges: []order.Edge{{EdgeID: "e01", SequenceID: 1, Released: true}},
	}))

	select {
	case m := <-cmd.moves:
		if m.x != 1 || m.y != 2 {
			t.Fatalf("MoveTo = %+v, want target (1,2)", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor never dispatched after order accept")
	}

	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.lastNodeSeen("n1") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("published state never showed lastNodeId n1 (traversal not committed/published)")
}

func TestChangePublisher_PublishesOnStoreChange(t *testing.T) {
	st := &syncTransport{}
	store := vehiclestate.New()
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	bus, err := NewWithTransport(st, store, "TESTBOT", "2.1.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bus.StartChangePublisher(ctx)

	store.CommitTraversal("n1", 2)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.count() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no publish after store change")
}

func TestInstantActionExecutionEndToEnd(t *testing.T) {
	st := &syncTransport{}
	store := vehiclestate.New()
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	bus, err := NewWithTransport(st, store, "TESTBOT", "2.1.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	cmd := &e2eCommander{
		moves:        make(chan struct{ x, y, theta float64 }, 4),
		chargeStarts: make(chan struct{}, 4),
		events:       make(chan robotbus.ExecutionEvent, 4),
	}
	exec := executor.New(store, cmd)
	bus.SetInstantActionHook(exec.InstantAction)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = exec.Start(ctx) }()
	go bus.StartChangePublisher(ctx)

	bus.handleInstantActions([]byte(`{"headerId":1,"timestamp":"2026-07-17T00:00:00.000Z","version":"2.1.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"ia-e2e","actionType":"startCharging","blockingType":"HARD"}]}`))

	select {
	case <-cmd.chargeStarts:
	case <-time.After(2 * time.Second):
		t.Fatal("executor never dispatched StartCharging after instant action accept")
	}

	cmd.events <- robotbus.ExecutionEvent{StartChargingCompleted: &robotbus.ChargingCompleted{Success: true}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.instantStatusSeen("ia-e2e", state.ActionStatusFinished) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("published state never showed ia-e2e FINISHED")
}

// the dashboard's Clean button end to end: wire bytes in, published action
// states out — WAITING on accept, RUNNING on dispatch, FINISHED on end event.
func TestCleaningExecutionEndToEnd(t *testing.T) {
	st := &syncTransport{}
	store := vehiclestate.New()
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	bus, err := NewWithTransport(st, store, "TESTBOT", "2.1.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	cmd := &e2eCommander{
		moves:       make(chan struct{ x, y, theta float64 }, 4),
		cleanStarts: make(chan []robotbus.ZoneRef, 4),
		events:      make(chan robotbus.ExecutionEvent, 4),
	}
	exec := executor.New(store, cmd)
	bus.SetOrderAcceptedHook(exec.OrderAccepted)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = exec.Start(ctx) }()
	go bus.StartChangePublisher(ctx)

	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "clean-e2e", OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true, Actions: []order.Action{{
				ActionID: "a-clean", ActionType: "startCleaning", BlockingType: order.BlockingTypeHard,
				ActionParameters: []order.ActionParameter{
					{Key: "zones", Value: []any{map[string]any{"name": "Zone A"}}},
					{Key: "homePoi", Value: map[string]any{"name": "Home"}},
				},
			}}},
		},
		Edges: []order.Edge{},
	}))

	select {
	case zones := <-cmd.cleanStarts:
		if len(zones) != 1 || zones[0].Name != "Zone A" {
			t.Fatalf("StartCleaning zones = %+v, want [Zone A]", zones)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor never dispatched StartCleaning after order accept")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.actionStatusSeen("a-clean", state.ActionStatusRunning) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !st.actionStatusSeen("a-clean", state.ActionStatusRunning) {
		t.Fatal("published state never showed a-clean RUNNING")
	}

	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: true}}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.actionStatusSeen("a-clean", state.ActionStatusFinished) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("published state never showed a-clean FINISHED")
}

// pins an incident observed on hardware: a FAILED move held nodeStates forever,
// so every new order rejected OTHER_ORDER_ACTIVE; cancelOrder is the spec's exit.
func TestCancelOrderRecoversFromFailedOrder(t *testing.T) {
	st := &syncTransport{}
	store := vehiclestate.New()
	store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	bus, err := NewWithTransport(st, store, "TESTBOT", "2.1.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	cmd := &e2eCommander{
		moves:  make(chan struct{ x, y, theta float64 }, 4),
		events: make(chan robotbus.ExecutionEvent, 4),
	}
	exec := executor.New(store, cmd)
	bus.SetOrderAcceptedHook(exec.OrderAccepted)
	bus.SetInstantActionHook(exec.InstantAction)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = exec.Start(ctx) }()
	go bus.StartChangePublisher(ctx)

	store.SetTelemetry(vehiclestate.Telemetry{
		AGVPosition: &state.AGVPosition{X: 1.0, Y: 2.0, MapID: "map-1", PositionInitialized: true},
	})
	theta := 1.5
	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-fail", OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true,
				NodePosition: &order.NodePosition{X: 30, Y: 40, MapID: "map-1", Theta: &theta}},
		},
		Edges: []order.Edge{{EdgeID: "e01", SequenceID: 1, Released: true}},
	}))
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch")
	}
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: false}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := store.Snapshot()
		if len(snap.Errors) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// the wedge: a new order is rejected while the failed order's states are held
	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-blocked", OrderUpdateID: 0,
		Nodes: []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}},
		Edges: []order.Edge{},
	}))
	if snap := store.Snapshot(); snap.OrderID != "o-fail" {
		t.Fatalf("OrderID = %q, want o-fail still held", snap.OrderID)
	}

	// the exit: cancelOrder clears it
	bus.handleInstantActions([]byte(`{"headerId":9,"timestamp":"2026-07-17T00:00:00.000Z","version":"2.1.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"c-e2e","actionType":"cancelOrder","blockingType":"NONE"}]}`))

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.instantStatusSeen("c-e2e", state.ActionStatusFinished) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !st.instantStatusSeen("c-e2e", state.ActionStatusFinished) {
		t.Fatal("cancelOrder never reported FINISHED")
	}

	// and a new order is accepted + dispatched
	bus.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-recovered", OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true,
				NodePosition: &order.NodePosition{X: 1, Y: 2, MapID: "map-1", Theta: &theta}},
		},
		Edges: []order.Edge{{EdgeID: "e01", SequenceID: 1, Released: true}},
	}))
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no dispatch after cancel recovery (still wedged?)")
	}
	if snap := store.Snapshot(); snap.OrderID != "o-recovered" {
		t.Fatalf("OrderID = %q, want o-recovered", snap.OrderID)
	}
}
