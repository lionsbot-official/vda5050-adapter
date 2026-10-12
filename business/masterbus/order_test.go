package masterbus

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
)

func TestValidateShape(t *testing.T) {
	// helpers
	node := func(seq uint32) order.Node {
		return order.Node{
			NodeID:     "n",
			SequenceID: seq,
			Actions:    []order.Action{},
		}
	}

	edge := func(seq uint32) order.Edge {
		return order.Edge{
			EdgeID:     "e",
			SequenceID: seq,
			Actions:    []order.Action{},
		}
	}

	cases := []struct {
		name    string
		order   order.Order
		wantErr bool
	}{
		{
			name:    "valid single node no edges",
			order:   order.Order{Nodes: []order.Node{node(0)}, Edges: []order.Edge{}},
			wantErr: false,
		},
		{
			name:    "valid two nodes one edge",
			order:   order.Order{Nodes: []order.Node{node(0), node(2)}, Edges: []order.Edge{edge(1)}},
			wantErr: false,
		},
		{
			name:    "empty nodes reject",
			order:   order.Order{Nodes: []order.Node{}, Edges: []order.Edge{}},
			wantErr: true,
		},
		{
			name:    "edge count not nodes minus one reject",
			order:   order.Order{Nodes: []order.Node{node(0), node(2)}, Edges: []order.Edge{}},
			wantErr: true,
		},
		{
			name:    "too many edges rejected",
			order:   order.Order{Nodes: []order.Node{node(0)}, Edges: []order.Edge{edge(1)}},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateShape(tc.order)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("validateShape() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
func TestValidateNewOrder(t *testing.T) {
	// helpers
	node := func(seq uint32, released bool) order.Node {
		return order.Node{
			NodeID:     "n",
			SequenceID: seq,
			Released:   released,
			Actions:    []order.Action{},
		}
	}

	edge := func(seq uint32) order.Edge {
		return order.Edge{
			EdgeID:     "e",
			SequenceID: seq,
			Released:   true,
			Actions:    []order.Action{},
		}
	}

	cases := []struct {
		name    string
		order   order.Order
		wantErr bool
	}{
		{
			name:    "missing seq-0 node",
			order:   order.Order{Nodes: []order.Node{node(2, true)}, Edges: []order.Edge{edge(1)}},
			wantErr: true,
		},
		{
			name:    "seq-0 present but unreleased first node",
			order:   order.Order{Nodes: []order.Node{node(0, false)}, Edges: []order.Edge{}},
			wantErr: true,
		},
		{
			name:    "release seq-0 present",
			order:   order.Order{Nodes: []order.Node{node(0, true)}, Edges: []order.Edge{}},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNewOrder(tc.order)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("validateNewOrder() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
