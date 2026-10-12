package masterbus

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/cleaningcatalog"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

type recordedPublish struct {
	topic    string
	payload  any
	qos      byte
	retained bool
}

type fakeTransport struct {
	published []recordedPublish
	onConnect func()
}

func (f *fakeTransport) Publish(topic string, payload any, qos byte, retained bool) error {
	f.published = append(f.published, recordedPublish{topic, payload, qos, retained})
	return nil
}

func (f *fakeTransport) Subscribe(topic string, qos byte) (chan []byte, error) {
	return make(chan []byte), nil
}

func (f *fakeTransport) Connect() error {
	return nil
}
func (f *fakeTransport) Disconnect() {}
func (f *fakeTransport) SetOnConnect(fn func()) {
	f.onConnect = fn
}

func newTestBus(t *testing.T) (*Bus, *fakeTransport) {
	t.Helper()
	ft := &fakeTransport{}
	b, err := NewWithTransport(ft, vehiclestate.New(), "TESTBOT", "2.0.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	b.robot.store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	return b, ft
}

func lastState(t *testing.T, b *Bus, ft *fakeTransport) state.State {
	t.Helper()
	for i := len(ft.published) - 1; i >= 0; i-- {
		if ft.published[i].topic != b.stateTopic {
			continue
		}
		st, ok := ft.published[i].payload.(state.State)
		if !ok {
			t.Fatalf("state payload type = %T, want state.State", ft.published[i].payload)
		}
		return st
	}
	t.Fatalf("no state published to %s", b.stateTopic)
	return state.State{}
}

func mustOrderBytes(t *testing.T, o order.Order) []byte {
	t.Helper()
	if o.Version == "" {
		o.Header = common.Header{Version: "2.0.0", Timestamp: "2026-10-10T00:00:00.00Z", Manufacturer: "lionsbot", SerialNumber: "TESTBOT"}
	}
	o.Nodes = append([]order.Node{}, o.Nodes...)
	o.Edges = append([]order.Edge{}, o.Edges...)
	for i := range o.Nodes {
		if o.Nodes[i].SequenceID > 0 && o.Nodes[i].NodePosition == nil {
			o.Nodes[i].NodePosition = &order.NodePosition{X: float64(o.Nodes[i].SequenceID), MapID: "map-1"}
		}
		o.Nodes[i].Actions = append([]order.Action{}, o.Nodes[i].Actions...)
		for j := range o.Nodes[i].Actions {
			if o.Nodes[i].Actions[j].BlockingType == "" {
				o.Nodes[i].Actions[j].BlockingType = order.BlockingTypeHard
			}
		}
	}
	for i := range o.Edges {
		e := &o.Edges[i]
		e.Actions = append([]order.Action{}, e.Actions...)
		for j := range e.Actions {
			if e.Actions[j].BlockingType == "" {
				e.Actions[j].BlockingType = order.BlockingTypeHard
			}
		}
		for _, n := range o.Nodes {
			if n.SequenceID+1 == e.SequenceID && e.StartNodeID == "" {
				e.StartNodeID = n.NodeID
			}
			if n.SequenceID == e.SequenceID+1 && e.EndNodeID == "" {
				e.EndNodeID = n.NodeID
			}
		}
	}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	return data
}

func hasError(st state.State, et state.ErrorType) bool {
	_, ok := findError(st, et)
	return ok
}

func findError(st state.State, et state.ErrorType) (state.Error, bool) {
	i := slices.IndexFunc(st.Errors, func(e state.Error) bool { return e.ErrorType == et })
	if i < 0 {
		return state.Error{}, false
	}
	return st.Errors[i], true
}

func hasRef(e state.Error, key, value string) bool {
	return slices.Contains(e.ErrorReferences, state.ErrorReference{ReferenceKey: key, ReferenceValue: value})
}

func TestHandleOrder_AcceptsValidNewOrder(t *testing.T) {
	b, ft := newTestBus(t)
	allowOrderAction(t, "noop")
	allowOrderAction(t, "beep")

	o := order.Order{
		OrderID:       "order-1",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{
				NodeID: "n0", SequenceID: 0, Released: true, Actions: []order.Action{{ActionID: "a0", ActionType: "noop"}},
			},
			{
				NodeID: "n1", SequenceID: 2, Released: true, Actions: []order.Action{{ActionID: "a1", ActionType: "beep"}},
			},
		},
		Edges: []order.Edge{
			{
				EdgeID: "e1", SequenceID: 1, Released: true, Actions: []order.Action{},
			},
		},
	}

	b.handleOrder(mustOrderBytes(t, o))

	st := lastState(t, b, ft)

	if st.OrderID != "order-1" {
		t.Errorf("OrderID = %q, want %q", st.OrderID, "order-1")
	}
	if st.OrderUpdateID != 0 {
		t.Errorf("OrderUpdateID = %d, want 0", st.OrderUpdateID)
	}
	if st.LastNodeID != "n0" {
		t.Errorf("LastNodeID = %q, want %q", st.LastNodeID, "n0")
	}
	if len(st.NodeStates) != 1 || st.NodeStates[0].NodeID != "n1" {
		t.Errorf("Nodestates = %+v, want only n1 (seq-0 excluded)", st.NodeStates)
	}
	if len(st.EdgeStates) != 1 || st.EdgeStates[0].EdgeID != "e1" {
		t.Errorf("EdgeStates = %+v, want only e1", st.EdgeStates)
	}
	if len(st.ActionStates) != 2 {
		t.Fatalf("ActionStates len = %d, want 2", len(st.ActionStates))
	}
	for _, as := range st.ActionStates {
		if as.ActionStatus != state.ActionStatusWaiting {
			t.Errorf("action %q status = %q, want WAITING", as.ActionID, as.ActionStatus)
		}
	}
	if len(st.Errors) != 0 {
		t.Errorf("Errors = %+v, want none", st.Errors)
	}

}

func TestHandleOrder_MalformedJSON_ValidationFailure(t *testing.T) {
	b, ft := newTestBus(t)
	b.handleOrder([]byte("{not valid json"))
	st := lastState(t, b, ft)
	if !hasError(st, state.ErrorTypeValidationFailure) {
		t.Errorf("Errors = %+v, want one of type %s", st.Errors, state.ErrorTypeValidationFailure)
	}
}

func TestHandleOrder_RejectsInvalidNewOrder(t *testing.T) {
	cases := []struct {
		name    string
		order   order.Order
		wantErr state.ErrorType
	}{
		{
			name: "zero nodes",
			order: order.Order{
				OrderID:       "o-zero",
				OrderUpdateID: 0,
				Nodes:         []order.Node{},
				Edges:         []order.Edge{},
			},
			wantErr: state.ErrorTypeValidationFailure,
		},
		{
			name: "invalid edge counts",
			order: order.Order{
				OrderID:       "order-0",
				OrderUpdateID: 0,
				Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0}, {NodeID: "n1", SequenceID: 2}},
				Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1}, {EdgeID: "e2", SequenceID: 3}},
			},
			wantErr: state.ErrorTypeValidationFailure,
		},
		{
			name: "new order missing seqId 0",
			order: order.Order{
				OrderID:       "order-0",
				OrderUpdateID: 0,
				Nodes:         []order.Node{{NodeID: "n3", SequenceID: 4}, {NodeID: "n2", SequenceID: 2}},
				Edges:         []order.Edge{{EdgeID: "e2", SequenceID: 3}},
			},
			wantErr: state.ErrorTypeValidationFailure,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, ft := newTestBus(t)
			b.handleOrder(mustOrderBytes(t, tc.order))
			st := lastState(t, b, ft)
			e, ok := findError(st, tc.wantErr)
			if !ok {
				t.Fatalf("Errors = %+v, want one of type %s", st.Errors, tc.wantErr)
			}
			if !hasRef(e, "orderId", tc.order.OrderID) {
				t.Errorf("error refs = %+v, want orderId=%q", e.ErrorReferences, tc.order.OrderID)
			}
		})
	}
}

func TestHandleOrder_NewOrderWhileOtherOrderActive(t *testing.T) {
	b, ft := newTestBus(t)
	active := order.Order{
		OrderID:       "order-0",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", Released: true, SequenceID: 0}, {NodeID: "n1", SequenceID: 2}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1}},
	}
	newOrder := order.Order{
		OrderID:       "order-1",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0}, {NodeID: "n1", SequenceID: 2}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1}},
	}

	b.handleOrder(mustOrderBytes(t, active))
	b.handleOrder(mustOrderBytes(t, newOrder))
	st := lastState(t, b, ft)
	e, ok := findError(st, state.ErrorTypeOrderError)
	if !ok {
		t.Fatalf("Errors = %+v, want %s", st.Errors, state.ErrorTypeOrderError)
	}
	if st.OrderID != active.OrderID {
		t.Fatalf("OrderID = %q, want order-0 (active order left untouched)", st.OrderID)
	}
	if !hasRef(e, "orderId", newOrder.OrderID) {
		t.Errorf("error refs = %+v, want orderId=%q", e.ErrorReferences, newOrder.OrderID)
	}
}

func TestHandleOrder_ShapeInvalidRejectedBeforeDecideOrder(t *testing.T) {
	b, ft := newTestBus(t)

	active := order.Order{
		OrderID:       "order-A",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}, {NodeID: "n1", SequenceID: 2}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1}},
	}
	b.handleOrder(mustOrderBytes(t, active))

	// same orderId + same orderUpdateId, but zero nodes.
	// shape must be rejected first
	bad := order.Order{
		OrderID:       "order-A",
		OrderUpdateID: 0,
		Nodes:         []order.Node{},
		Edges:         []order.Edge{},
	}
	b.handleOrder(mustOrderBytes(t, bad))

	st := lastState(t, b, ft)
	if !hasError(st, state.ErrorTypeValidationFailure) {
		t.Errorf("Errors = %+v, want %s", st.Errors, state.ErrorTypeValidationFailure)
	}
	if hasError(st, state.ErrorTypeOrderError) {
		t.Errorf("Errors = %+v, shape must be rejected before the update-id check", st.Errors)
	}
	if st.OrderID != active.OrderID {
		t.Errorf("OrderID = %q, want %q (held order untouched)", st.OrderID, active.OrderID)
	}
}

func TestHandleOrder_UpdateWithoutSeqZeroAccepted(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-B",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}, {NodeID: "n2", SequenceID: 2, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, base))

	// valid-shape update starting at the decision point (seq 2), no seq-0 node.
	// seq-0 rule is new-order-only; an update must not be rejected for lacking it.
	update := order.Order{
		OrderID:       "order-B",
		OrderUpdateID: 1,
		Nodes:         []order.Node{{NodeID: "n2", SequenceID: 2, Released: true}, {NodeID: "n3", SequenceID: 4, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e3", SequenceID: 3, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, update))

	st := lastState(t, b, ft)
	if hasError(st, state.ErrorTypeValidationFailure) {
		t.Errorf("Errors = %+v, update without seq-0 must not be a validation failure", st.Errors)
	}
	if st.OrderUpdateID != 1 {
		t.Errorf("OrderUpdateID = %d, want 1 (update accepted)", st.OrderUpdateID)
	}
}

func TestHandleOrder_BadStitchUpdateRejected(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-c",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}, {NodeID: "n2", SequenceID: 2, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, base))

	// decision point is n2/seq2 (highest released node of the held order).
	// this update's first ndoe is a different node -> not a valid continuation
	badUpdate := order.Order{
		OrderID:       "order-c",
		OrderUpdateID: 1,
		Nodes:         []order.Node{{NodeID: "n9", SequenceID: 2, Released: true}, {NodeID: "n3", SequenceID: 4, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e3", SequenceID: 3, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, badUpdate))

	st := lastState(t, b, ft)
	e, ok := findError(st, state.ErrorTypeOrderError)
	if !ok {
		t.Fatalf("Errors = %+v, want %s", st.Errors, state.ErrorTypeOrderError)
	}
	if !hasRef(e, "orderId", badUpdate.OrderID) {
		t.Errorf("error ref = %+v, want orderId=%q", e.ErrorReferences, badUpdate.OrderID)
	}
	if st.OrderID != base.OrderID || st.OrderUpdateID != 0 {
		t.Errorf("held order = (%q, %d), want (%q, 0) untouched", st.OrderID, st.OrderUpdateID, base.OrderID)
	}
}

func nodeIDs(ns []state.NodeState) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.NodeID
	}
	return out
}

func edgeIDs(es []state.EdgeState) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.EdgeID
	}
	return out
}

func TestHandleOrder_ValidUpdateMergedOntoBase(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-D",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n2", SequenceID: 2, Released: true},
			{NodeID: "n4", SequenceID: 4, Released: true},
		},
		Edges: []order.Edge{
			{EdgeID: "e1", SequenceID: 1, Released: true},
			{EdgeID: "e3", SequenceID: 3, Released: true},
		},
	}
	b.handleOrder(mustOrderBytes(t, base))

	update := order.Order{
		OrderID:       "order-D",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n4", SequenceID: 4, Released: true},
			{NodeID: "n6", SequenceID: 6, Released: true},
		},
		Edges: []order.Edge{
			{EdgeID: "e5", SequenceID: 5, Released: true},
		},
	}
	b.handleOrder(mustOrderBytes(t, update))

	st := lastState(t, b, ft)
	if st.OrderUpdateID != 1 {
		t.Fatalf("OrderUpdateID = %d, want 1", st.OrderUpdateID)
	}
	if got, want := nodeIDs(st.NodeStates), []string{"n2", "n4", "n6"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NodeStates = %v, want %v", got, want)
	}
	if got, want := edgeIDs(st.EdgeStates), []string{"e1", "e3", "e5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EdgeStates = %v, want %v", got, want)
	}
	if st.LastNodeID != "n0" {
		t.Errorf("LastNodeID = %q, want n0", st.LastNodeID)
	}
}

func actionIDs(as []state.ActionState) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ActionID
	}
	return out
}

func TestHandleOrder_HorizonReplacementDropsStaleActions(t *testing.T) {
	b, ft := newTestBus(t)
	allowOrderAction(t, "noop")

	base := order.Order{
		OrderID:       "order-E",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true, Actions: []order.Action{{ActionID: "a0", ActionType: "noop"}}},
			{NodeID: "n2", SequenceID: 2, Released: true, Actions: []order.Action{{ActionID: "a2", ActionType: "noop"}}},
			{NodeID: "n4", SequenceID: 4, Released: false, Actions: []order.Action{{ActionID: "stale", ActionType: "noop"}}},
		},
		Edges: []order.Edge{
			{EdgeID: "e1", SequenceID: 1, Released: true},
			{EdgeID: "e3", SequenceID: 3, Released: false},
		},
	}
	b.handleOrder(mustOrderBytes(t, base))

	update := order.Order{
		OrderID:       "order-E",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n2", SequenceID: 2, Released: true, Actions: []order.Action{{ActionID: "a2", ActionType: "noop"}}},
			{NodeID: "n8", SequenceID: 4, Released: true, Actions: []order.Action{{ActionID: "fresh", ActionType: "noop"}}},
		},
		Edges: []order.Edge{
			{EdgeID: "e7", SequenceID: 3, Released: true},
		},
	}
	b.handleOrder(mustOrderBytes(t, update))

	st := lastState(t, b, ft)
	got := actionIDs(st.ActionStates)
	if slices.Contains(got, "stale") {
		t.Errorf("ActionStates = %v, stale horizon action must be dropped", got)
	}
	if !slices.Contains(got, "fresh") {
		t.Errorf("ActionStates = %v, want fresh action present", got)
	}
	for _, a := range st.ActionStates {
		if a.ActionStatus != state.ActionStatusWaiting {
			t.Errorf("action %q status = %q, want WAITING", a.ActionID, a.ActionStatus)
		}
	}
}

func TestHandleOrder_HorizonPromotedToBase(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-F",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n2", SequenceID: 2, Released: true},
			{NodeID: "n4", SequenceID: 4, Released: false},
		},
		Edges: []order.Edge{
			{EdgeID: "e1", SequenceID: 1, Released: true},
			{EdgeID: "e3", SequenceID: 3, Released: false},
		},
	}
	b.handleOrder(mustOrderBytes(t, base))

	update := order.Order{
		OrderID:       "order-F",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n2", SequenceID: 2, Released: true},
			{NodeID: "n4", SequenceID: 4, Released: true},
		},
		Edges: []order.Edge{
			{EdgeID: "e3", SequenceID: 3, Released: true},
		},
	}
	b.handleOrder(mustOrderBytes(t, update))

	st := lastState(t, b, ft)
	found := false
	for _, n := range st.NodeStates {
		if n.NodeID == "n4" {
			found = true
			if !n.Released {
				t.Errorf("n4 Released = false, want true after promotion")
			}
		}
	}
	if !found {
		t.Fatalf("NodeStates = %v, want n4 present", nodeIDs(st.NodeStates))
	}
}

func TestHandleOrder_SingleNodeBaseContinuation(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-G",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}},
		Edges:         []order.Edge{},
	}
	b.handleOrder(mustOrderBytes(t, base))

	update := order.Order{
		OrderID:       "order-G",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n2", SequenceID: 2, Released: true},
		},
		Edges: []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, update))

	st := lastState(t, b, ft)
	if st.OrderUpdateID != 1 {
		t.Fatalf("OrderUpdateID = %d, want 1", st.OrderUpdateID)
	}
	if got, want := nodeIDs(st.NodeStates), []string{"n2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NodeStates = %v, want %v", got, want)
	}
	if st.LastNodeID != "n0" {
		t.Errorf("LastNodeID = %q, want n0", st.LastNodeID)
	}
}

func TestHandleOrder_AcceptedUpdateClearsOrderScopedErrors(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-H",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}, {NodeID: "n2", SequenceID: 2, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, base))

	badUpdate := order.Order{
		OrderID:       "order-H",
		OrderUpdateID: 1,
		Nodes:         []order.Node{{NodeID: "n9", SequenceID: 2, Released: true}, {NodeID: "n4", SequenceID: 4, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e3", SequenceID: 3, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, badUpdate))
	if st := lastState(t, b, ft); !hasError(st, state.ErrorTypeOrderError) {
		t.Fatalf("precondition: want START_NODE_OUT_OF_RANGE, got %+v", st.Errors)
	}

	goodUpdate := order.Order{
		OrderID:       "order-H",
		OrderUpdateID: 1,
		Nodes:         []order.Node{{NodeID: "n2", SequenceID: 2, Released: true}, {NodeID: "n4", SequenceID: 4, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e3", SequenceID: 3, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, goodUpdate))

	st := lastState(t, b, ft)
	if hasError(st, state.ErrorTypeOrderError) {
		t.Errorf("Errors = %+v, order-scoped error must be cleared on accept", st.Errors)
	}
	if st.OrderUpdateID != 1 {
		t.Errorf("OrderUpdateID = %d, want 1", st.OrderUpdateID)
	}
}

func TestHandleOrder_IdempotentUpdateResendIgnored(t *testing.T) {
	b, ft := newTestBus(t)

	base := order.Order{
		OrderID:       "order-I",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}, {NodeID: "n2", SequenceID: 2, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, base))

	update := order.Order{
		OrderID:       "order-I",
		OrderUpdateID: 1,
		Nodes:         []order.Node{{NodeID: "n2", SequenceID: 2, Released: true}, {NodeID: "n4", SequenceID: 4, Released: true}},
		Edges:         []order.Edge{{EdgeID: "e3", SequenceID: 3, Released: true}},
	}
	b.handleOrder(mustOrderBytes(t, update))
	b.handleOrder(mustOrderBytes(t, update)) // identical re-send

	st := lastState(t, b, ft)
	if hasError(st, state.ErrorTypeOrderError) {
		t.Errorf("Errors = %+v, identical re-send must be ignored", st.Errors)
	}
	if st.OrderUpdateID != 1 {
		t.Errorf("OrderUpdateID = %d, want 1", st.OrderUpdateID)
	}
	if got, want := nodeIDs(st.NodeStates), []string{"n2", "n4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NodeStates = %v, want %v", got, want)
	}
}

// pins a wedge observed on hardware: an accepted order with an inert action
// kept isIdle false forever, blocking every later order until process restart
func TestHandleOrder_UnsupportedActionOrderRejected_VehicleNotWedged(t *testing.T) {
	b, ft := newTestBus(t)

	clean := order.Order{
		OrderID:       "clean-1",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n1", SequenceID: 0, Released: true, Actions: []order.Action{{ActionID: "a1", ActionType: "spotClean"}}},
		},
		Edges: []order.Edge{},
	}
	b.handleOrder(mustOrderBytes(t, clean))

	st := lastState(t, b, ft)
	if !hasError(st, state.ErrorTypeOrderError) {
		t.Fatalf("errors = %+v, want INVALID_ORDER_ACTION", st.Errors)
	}
	if st.OrderID != "" {
		t.Errorf("OrderID = %q, want untouched", st.OrderID)
	}
	if len(st.ActionStates) != 0 {
		t.Errorf("ActionStates = %+v, want none", st.ActionStates)
	}

	p2p := order.Order{
		OrderID:       "move-1",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true, Actions: []order.Action{}},
			{NodeID: "n1", SequenceID: 2, Released: true, Actions: []order.Action{}},
		},
		Edges: []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true, Actions: []order.Action{}}},
	}
	b.handleOrder(mustOrderBytes(t, p2p))

	st = lastState(t, b, ft)
	if st.OrderID != "move-1" {
		t.Fatalf("OrderID = %q, want move-1 (vehicle wedged?)", st.OrderID)
	}
	if hasError(st, state.ErrorTypeOrderError) {
		t.Errorf("unexpected OTHER_ORDER_ACTIVE after rejected action order")
	}
	if hasError(st, state.ErrorTypeOrderError) {
		t.Errorf("INVALID_ORDER_ACTION not cleared by accepted order")
	}
}

// the dashboard's Clean button: one positionless seq-0 released node carrying
// startCleaning. Must be accepted, its action tracked WAITING (order scope).
func TestHandleOrder_CleaningOrderAccepted(t *testing.T) {
	b, ft := newTestBus(t)

	clean := order.Order{
		OrderID:       "clean-1",
		OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n1", SequenceID: 0, Released: true, Actions: []order.Action{{
				ActionID:   "a1",
				ActionType: "startCleaning",
				ActionParameters: []order.ActionParameter{
					{Key: "zones", Value: []any{map[string]any{"name": "Zone A"}}},
				},
			}}},
		},
		Edges: []order.Edge{},
	}
	b.handleOrder(mustOrderBytes(t, clean))

	st := lastState(t, b, ft)
	if hasError(st, state.ErrorTypeOrderError) {
		t.Fatalf("errors = %+v, startCleaning must be a supported order action", st.Errors)
	}
	if st.OrderID != "clean-1" {
		t.Fatalf("OrderID = %q, want clean-1", st.OrderID)
	}
	if len(st.ActionStates) != 1 || st.ActionStates[0].ActionID != "a1" ||
		st.ActionStates[0].ActionStatus != state.ActionStatusWaiting {
		t.Errorf("ActionStates = %+v, want a1 WAITING", st.ActionStates)
	}
}

func TestHandleOrder_UpdateAfterCancelRejected(t *testing.T) {
	b, ft := newTestBus(t)
	b.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-c", OrderUpdateID: 0,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n1", SequenceID: 2, Released: true},
		},
		Edges: []order.Edge{{EdgeID: "e01", SequenceID: 1, Released: true}},
	}))

	b.robot.store.CancelOrder()

	// any further message for the cancelled orderId must reject per Fig-8 step 7
	b.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-c", OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n1", SequenceID: 2, Released: true},
			{NodeID: "n2", SequenceID: 4, Released: true},
		},
		Edges: []order.Edge{{EdgeID: "e12", SequenceID: 3, Released: true}},
	}))
	st := lastState(t, b, ft)
	if !hasError(st, state.ErrorTypeOrderError) {
		t.Fatalf("errors = %+v, want ORDER_UPDATE_FOLLOWING_CANCEL", st.Errors)
	}

	// a NEW order is fine and clears the cancelled flag
	b.handleOrder(mustOrderBytes(t, order.Order{
		OrderID: "o-next", OrderUpdateID: 0,
		Nodes: []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}},
		Edges: []order.Edge{},
	}))
	st = lastState(t, b, ft)
	if st.OrderID != "o-next" {
		t.Fatalf("OrderID = %q, want o-next accepted after cancel", st.OrderID)
	}
	if hasError(st, state.ErrorTypeOrderError) {
		t.Error("ORDER_UPDATE_FOLLOWING_CANCEL not cleared by accepted order")
	}
}

func lastPublishOn(ft *fakeTransport, topic string) (recordedPublish, bool) {
	for i := len(ft.published) - 1; i >= 0; i-- {
		if ft.published[i].topic == topic {
			return ft.published[i], true
		}
	}
	return recordedPublish{}, false
}

func TestPublishCleaningCatalog_RetainedOnVendorTopic(t *testing.T) {
	b, ft := newTestBus(t)

	cat := cleaningcatalog.Catalog{
		MapID: "map-1",
		Zones: []cleaningcatalog.Zone{{
			Name: "Lobby", SelectedMode: "Standard",
			Modes: []cleaningcatalog.Mode{{Name: "Standard", Configs: []cleaningcatalog.Config{
				{Name: "water", Value: 3, Min: 0, Max: 5, Step: 1},
			}}},
		}},
		Docks: []string{"dock-1"},
	}
	if err := b.PublishCleaningCatalog(cat); err != nil {
		t.Fatalf("PublishCleaningCatalog: %v", err)
	}

	want := "vda5050/v2/lionsbot/TESTBOT/cleaningcatalog"
	pub, ok := lastPublishOn(ft, want)
	if !ok {
		t.Fatalf("no publish on %s; published topics = %v", want, publishedTopics(ft))
	}
	if !pub.retained {
		t.Error("cleaning catalog must be retained (late-joining dashboards need it)")
	}
	body, _ := json.Marshal(pub.payload)
	var got struct {
		SerialNumber string `json:"serialNumber"`
		MapID        string `json:"mapId"`
		Zones        []struct {
			Name  string `json:"name"`
			Modes []struct {
				Configs []struct {
					Name string  `json:"name"`
					Max  float64 `json:"max"`
				} `json:"configs"`
			} `json:"modes"`
		} `json:"zones"`
		Docks []string `json:"docks"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("catalog payload not JSON: %v", err)
	}
	if got.SerialNumber != "TESTBOT" || got.MapID != "map-1" {
		t.Errorf("envelope = %+v, want serial TESTBOT / map-1", got)
	}
	if len(got.Zones) != 1 || got.Zones[0].Name != "Lobby" ||
		got.Zones[0].Modes[0].Configs[0].Name != "water" || got.Zones[0].Modes[0].Configs[0].Max != 5 {
		t.Errorf("zones = %+v, want Lobby/water with max=5 (slider bounds)", got.Zones)
	}
	if len(got.Docks) != 1 || got.Docks[0] != "dock-1" {
		t.Errorf("docks = %v, want [dock-1]", got.Docks)
	}
}

func TestPublishCleaningCatalog_RepublishedOnConnect(t *testing.T) {
	b, ft := newTestBus(t)
	b.transport.SetOnConnect(b.publishOnline) // Start wires this; emulate it

	// catalog arrives before the transport is "connected"
	cat := cleaningcatalog.Catalog{MapID: "map-1", Zones: []cleaningcatalog.Zone{{Name: "Lobby"}}}
	if err := b.PublishCleaningCatalog(cat); err != nil {
		t.Fatalf("PublishCleaningCatalog: %v", err)
	}

	topic := "vda5050/v2/lionsbot/TESTBOT/cleaningcatalog"
	before := countPublishOn(ft, topic)
	ft.onConnect() // a (re)connect
	if countPublishOn(ft, topic) <= before {
		t.Error("connect must republish the buffered catalog so start order doesn't matter")
	}
}

func publishedTopics(ft *fakeTransport) []string {
	var ts []string
	for _, p := range ft.published {
		ts = append(ts, p.topic)
	}
	return ts
}

func countPublishOn(ft *fakeTransport, topic string) int {
	n := 0
	for _, p := range ft.published {
		if p.topic == topic {
			n++
		}
	}
	return n
}
