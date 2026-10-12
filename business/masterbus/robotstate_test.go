package masterbus

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestDecideOrder(t *testing.T) {
	base := order.Order{
		OrderID:       "order-X",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n2", SequenceID: 2, Released: true},
		},
		Edges: []order.Edge{
			{EdgeID: "e1", SequenceID: 1, Released: true},
		},
	}

	// held order that visits n0 twice (seq 0 and seq 4); decision point is n0@seq4.
	loopBase := order.Order{
		OrderID:       "order-X",
		OrderUpdateID: 1,
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true},
			{NodeID: "n5", SequenceID: 2, Released: true},
			{NodeID: "n0", SequenceID: 4, Released: true},
		},
		Edges: []order.Edge{
			{EdgeID: "e1", SequenceID: 1, Released: true},
			{EdgeID: "e3", SequenceID: 3, Released: true},
		},
	}

	cases := []struct {
		name    string
		held    order.Order // zero-value (no nodes) => fresh idle robot
		msg     order.Order
		wantDec orderDecision
		wantErr state.ErrorType
	}{
		{
			name: "same id, lower updateId -> outdated",
			held: base,
			msg: order.Order{
				OrderID:       "order-X",
				OrderUpdateID: 0,
				Nodes:         base.Nodes,
				Edges:         base.Edges,
			},
			wantDec: decisionReject,
			wantErr: state.ErrorTypeOutdatedOrderUpdate,
		},
		{
			name:    "same id, identical content -> ignore",
			held:    base,
			msg:     base,
			wantDec: decisionIgnore,
			wantErr: "",
		},
		{
			name: "same id, different content -> ignore",
			held: base,
			msg: order.Order{
				OrderID:       "order-X",
				OrderUpdateID: 1,
				Nodes: []order.Node{
					{NodeID: "n0", SequenceID: 0, Released: true},
				},
				Edges: []order.Edge{},
			},
			wantDec: decisionIgnore,
			wantErr: "",
		},
		{
			name: "same id, higher updateId, valid stitch -> accept update",
			held: base,
			msg: order.Order{
				OrderID:       "order-X",
				OrderUpdateID: 2,
				Nodes: []order.Node{
					{NodeID: "n2", SequenceID: 2, Released: true},
					{NodeID: "n4", SequenceID: 4, Released: true},
				},
				Edges: []order.Edge{
					{EdgeID: "e3", SequenceID: 3, Released: true},
				},
			},
			wantDec: decisionAcceptUpdate,
			wantErr: "",
		},
		{
			name: "same id, higher updateId, bad stitch -> start node out of range",
			held: base,
			msg: order.Order{
				OrderID:       "order-X",
				OrderUpdateID: 2,
				Nodes: []order.Node{
					{NodeID: "n9", SequenceID: 2, Released: true},
					{NodeID: "n4", SequenceID: 4, Released: true},
				},
				Edges: []order.Edge{
					{EdgeID: "e3", SequenceID: 3, Released: true},
				},
			},
			wantDec: decisionReject,
			wantErr: state.ErrorTypeStartNodeOutOfRange,
		},
		{
			name: "loop: stitch nodeId matches but sequenceId differs -> start node out of range",
			held: loopBase,
			msg: order.Order{
				OrderID:       "order-X",
				OrderUpdateID: 2,
				Nodes: []order.Node{
					{NodeID: "n0", SequenceID: 0, Released: true},
					{NodeID: "n6", SequenceID: 6, Released: true},
				},
				Edges: []order.Edge{
					{EdgeID: "e5", SequenceID: 5, Released: true},
				},
			},
			wantDec: decisionReject,
			wantErr: state.ErrorTypeStartNodeOutOfRange,
		},
		{
			name: "loop: stitch matches decision point n0@seq4 -> accept update",
			held: loopBase,
			msg: order.Order{
				OrderID:       "order-X",
				OrderUpdateID: 2,
				Nodes: []order.Node{
					{NodeID: "n0", SequenceID: 4, Released: true},
					{NodeID: "n6", SequenceID: 6, Released: true},
				},
				Edges: []order.Edge{
					{EdgeID: "e5", SequenceID: 5, Released: true},
				},
			},
			wantDec: decisionAcceptUpdate,
			wantErr: "",
		},
		{
			name: "different id while active -> other order active",
			held: base,
			msg: order.Order{
				OrderID:       "order-Y",
				OrderUpdateID: 0,
				Nodes: []order.Node{
					{NodeID: "m0", SequenceID: 0, Released: true},
				},
				Edges: []order.Edge{},
			},
			wantDec: decisionReject,
			wantErr: state.ErrorTypeOtherOrderActive,
		},
		{
			name: "idle, non-zero updateId -> accept new",
			msg: order.Order{
				OrderID:       "order-Z",
				OrderUpdateID: 1,
				Nodes: []order.Node{
					{NodeID: "m0", SequenceID: 0, Released: true},
				},
				Edges: []order.Edge{},
			},
			wantDec: decisionAcceptNew,
			wantErr: "",
		},
		{
			name: "idle, zero updateId -> accept new",
			msg: order.Order{
				OrderID:       "order-Z",
				OrderUpdateID: 0,
				Nodes: []order.Node{
					{NodeID: "m0", SequenceID: 0, Released: true},
				},
				Edges: []order.Edge{},
			},
			wantDec: decisionAcceptNew,
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRobotState(vehiclestate.New())
			if len(tc.held.Nodes) > 0 {
				r.applyOrder(tc.held)
			}
			gotDec, gotErr := r.decideOrder(tc.msg)
			if gotDec != tc.wantDec {
				t.Errorf("decision = %v, want %v", gotDec, tc.wantDec)
			}
			if gotErr != tc.wantErr {
				t.Errorf("errorType = %q, want %q", gotErr, tc.wantErr)
			}
		})
	}
}
