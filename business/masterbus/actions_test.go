package masterbus

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
)

// allowOrderAction registers a test-only supported order action for one test.
func allowOrderAction(t *testing.T, actionType string) {
	t.Helper()
	prev, had := supportedActions[actionType]
	supportedActions[actionType] = actionScope{order: true}
	t.Cleanup(func() {
		if had {
			supportedActions[actionType] = prev
			return
		}
		delete(supportedActions, actionType)
	})
}

func TestUnsupportedOrderAction(t *testing.T) {
	node := func(actions ...order.Action) order.Node {
		return order.Node{NodeID: "n0", SequenceID: 0, Released: true, Actions: actions}
	}

	tests := []struct {
		name  string
		setup func(t *testing.T)
		o     order.Order
		want  string // "" = all supported
	}{
		{
			name: "actionless order passes",
			o:    order.Order{Nodes: []order.Node{node()}, Edges: []order.Edge{}},
		},
		{
			name: "unknown node action caught",
			o: order.Order{
				Nodes: []order.Node{node(order.Action{ActionID: "a1", ActionType: "spotClean"})},
				Edges: []order.Edge{},
			},
			want: "spotClean",
		},
		{
			name: "unknown edge action caught",
			o: order.Order{
				Nodes: []order.Node{node(), {NodeID: "n1", SequenceID: 2, Released: true}},
				Edges: []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true, Actions: []order.Action{{ActionID: "a1", ActionType: "beep"}}}},
			},
			want: "beep",
		},
		{
			name: "instant-only action not valid on an order",
			o: order.Order{
				Nodes: []order.Node{node(order.Action{ActionID: "a1", ActionType: "startPause"})},
				Edges: []order.Edge{},
			},
			want: "startPause",
		},
		{
			name:  "registered order action passes",
			setup: func(t *testing.T) { allowOrderAction(t, "beep") },
			o: order.Order{
				Nodes: []order.Node{node(order.Action{ActionID: "a1", ActionType: "beep"})},
				Edges: []order.Edge{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			got, found := unsupportedOrderAction(tt.o)
			if tt.want == "" {
				if found {
					t.Fatalf("unsupportedOrderAction = %q, want none", got)
				}
				return
			}
			if !found || got != tt.want {
				t.Fatalf("unsupportedOrderAction = %q (found=%v), want %q", got, found, tt.want)
			}
		})
	}
}
