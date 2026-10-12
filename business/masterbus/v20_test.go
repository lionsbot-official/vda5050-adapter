package masterbus

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestV20PublishedStateContract(t *testing.T) {
	b, ft := newTestBus(t)
	b.robot.store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "map-1", PositionInitialized: true}})
	b.publishState()
	raw, err := json.Marshal(lastState(t, b, ft))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version":"2.0.0"`, `"agvPosition":`, `"positionInitialized":true`, `"batteryState":`, `"batteryCharge":`, `"eStop":"NONE"`, `"actionStates":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s: %s", want, raw)
		}
	}
	for _, forbidden := range []string{`"mobileRobotPosition":`, `"powerSupply":`, `"instantActionStates":`, `"plannedPath":`, `"activeEmergencyStop":`, `"maps":`, `"errorHint":`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("V3 field %s: %s", forbidden, raw)
		}
	}
}

func TestV20CombinedActionLifecycle(t *testing.T) {
	b, ft := newTestBus(t)
	b.robot.store.AddInstantActionState(state.ActionState{ActionID: "complete", ActionStatus: state.ActionStatusFinished})
	b.robot.store.AddInstantActionState(state.ActionState{ActionID: "running", ActionStatus: state.ActionStatusRunning})
	b.publishState()
	raw, _ := json.Marshal(lastState(t, b, ft))
	if !strings.Contains(string(raw), `"actionId":"running"`) {
		t.Fatalf("instant missing from actionStates: %s", raw)
	}
	b.robot.store.CommitOrderProjection(vehiclestate.OrderProjection{OrderID: "next", NodeStates: []state.NodeState{}, EdgeStates: []state.EdgeState{}, ActionStates: []state.ActionState{}})
	b.publishState()
	got := lastState(t, b, ft)
	if len(got.ActionStates) != 1 || got.ActionStates[0].ActionID != "running" {
		t.Fatalf("new order must retain running instant only: %+v", got.ActionStates)
	}
	b.robot.store.CancelOrder()
	b.publishState()
	if got := lastState(t, b, ft).ActionStates[0].ActionStatus; got != state.ActionStatusRunning {
		t.Fatalf("order cancellation changed instant status: %s", got)
	}
}

func TestV20RawOrderValidation(t *testing.T) {
	valid := `{"headerId":1,"timestamp":"2026-10-10T00:00:00.00Z","version":"2.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","orderId":"o1","orderUpdateId":0,"nodes":[{"nodeId":"n0","sequenceId":0,"released":true,"actions":[]},{"nodeId":"n1","sequenceId":2,"released":true,"nodePosition":{"x":1,"y":2,"mapId":"map-1","allowedDeviationXY":0.1},"actions":[]}],"edges":[{"edgeId":"e1","sequenceId":1,"released":true,"startNodeId":"n0","endNodeId":"n1","actions":[]}]}`
	cases := []struct {
		name, raw string
		want      state.ErrorType
	}{
		{"scalar tolerance", valid, ""},
		{"initial update need not be zero", strings.Replace(valid, `"orderUpdateId":0`, `"orderUpdateId":7`, 1), ""},
		{"wrong version", strings.Replace(valid, `"version":"2.0.0"`, `"version":"3.0.0"`, 1), state.ErrorTypeValidationFailure},
		{"wrong identity", strings.Replace(valid, `"serialNumber":"TESTBOT"`, `"serialNumber":"other"`, 1), state.ErrorTypeValidationFailure},
		{"missing endpoints", strings.Replace(valid, `"startNodeId":"n0",`, "", 1), state.ErrorTypeValidationFailure},
		{"wrong endpoints", strings.Replace(valid, `"startNodeId":"n0"`, `"startNodeId":"other"`, 1), state.ErrorTypeValidationFailure},
		{"null actions", strings.Replace(valid, `"actions":[]`, `"actions":null`, 1), state.ErrorTypeValidationFailure},
		{"missing header", strings.Replace(valid, `"headerId":1,`, "", 1), state.ErrorTypeValidationFailure},
		{"V3 ellipse", strings.Replace(valid, `"allowedDeviationXY":0.1`, `"allowedDeviationXY":{"a":1,"b":1}`, 1), state.ErrorTypeValidationFailure},
		{"missing navigation position", strings.Replace(valid, `"nodePosition":{"x":1,"y":2,"mapId":"map-1","allowedDeviationXY":0.1},`, "", 1), state.ErrorTypeOrderError},
		{"unsupported orientation type", strings.Replace(valid, `"edgeId":"e1"`, `"edgeId":"e1","orientationType":"GLOBAL"`, 1), state.ErrorTypeOrderError},
		{"V2.1 header", strings.Replace(valid, `"2.0.0"`, `"2.1.0"`, 1), state.ErrorTypeValidationFailure},
		{"unsupported speed", strings.Replace(valid, `"edgeId":"e1"`, `"edgeId":"e1","maxSpeed":0.2`, 1), state.ErrorTypeOrderError},
		{"V2.1 corridor", strings.Replace(valid, `"edgeId":"e1"`, `"edgeId":"e1","corridor":{"leftWidth":1,"rightWidth":1}`, 1), state.ErrorTypeValidationFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, ft := newTestBus(t)
			called := false
			b.SetOrderAcceptedHook(func(order.Order) { called = true })
			b.handleOrder([]byte(tc.raw))
			got := lastState(t, b, ft)
			if tc.want == "" {
				if !called || got.OrderID != "o1" || len(got.Errors) != 0 {
					t.Fatalf("valid order not accepted: %+v", got)
				}
				return
			}
			if called || got.OrderID != "" || !hasError(got, tc.want) {
				t.Fatalf("rejected order reached executor or wrong error: %+v", got)
			}
		})
	}
}

func TestV20DuplicateOrderIdentityIgnored(t *testing.T) {
	b, ft := newTestBus(t)
	o := order.Order{OrderID: "o1", Nodes: []order.Node{{NodeID: "n0", Released: true}}, Edges: []order.Edge{}}
	b.handleOrder(mustOrderBytes(t, o))
	o.Nodes[0].NodeID = "changed"
	b.handleOrder(mustOrderBytes(t, o))
	if got := lastState(t, b, ft); got.LastNodeID != "n0" || len(got.Errors) != 0 {
		t.Fatalf("duplicate changed held order: %+v", got)
	}
}

func TestV20OrderUpdateKeepsActionProgressAndInstantHistory(t *testing.T) {
	b, ft := newTestBus(t)
	b.robot.store.CommitOrderProjection(vehiclestate.OrderProjection{OrderID: "o1", ActionStates: []state.ActionState{{ActionID: "a1", ActionStatus: state.ActionStatusRunning}}})
	b.robot.store.AddInstantActionState(state.ActionState{ActionID: "i1", ActionStatus: state.ActionStatusFinished})
	b.robot.store.CommitOrderProjection(vehiclestate.OrderProjection{OrderID: "o1", OrderUpdateID: 1, KeepLastNode: true, ActionStates: []state.ActionState{{ActionID: "a1", ActionStatus: state.ActionStatusWaiting}}})
	b.publishState()
	got := lastState(t, b, ft)
	if len(got.ActionStates) != 2 || got.ActionStates[0].ActionStatus != state.ActionStatusRunning || got.ActionStates[1].ActionID != "i1" {
		t.Fatalf("update erased action progress/history: %+v", got.ActionStates)
	}
}

func TestV20RawInstantActionBlockingAndVersion(t *testing.T) {
	valid := `{"headerId":1,"timestamp":"2026-10-10T00:00:00.00Z","version":"2.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"i1","actionType":"startCharging","blockingType":"HARD"}]}`
	for _, tc := range []struct {
		name, raw string
		accepted  bool
	}{
		{"HARD dock", valid, true},
		{"NONE dock", strings.Replace(valid, `"HARD"`, `"NONE"`, 1), false},
		{"V3 SINGLE", strings.Replace(valid, `"HARD"`, `"SINGLE"`, 1), false},
		{"V3 header", strings.Replace(valid, `"2.0.0"`, `"3.0.0"`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ft := newTestBus(t)
			called := false
			b.SetInstantActionHook(func(order.Action) { called = true })
			b.handleInstantActions([]byte(tc.raw))
			if called != tc.accepted {
				t.Fatalf("executor called=%v, want %v", called, tc.accepted)
			}
			raw, err := json.Marshal(lastState(t, b, ft))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"instantActionStates"`) {
				t.Fatalf("V3 field published: %s", raw)
			}
		})
	}
}

func TestV20RejectsRelabelledConfiguration(t *testing.T) {
	for _, version := range []string{"2.1.0", "3.0.0"} {
		if _, err := NewWithTransport(&fakeTransport{}, vehiclestate.New(), "TESTBOT", version, "lionsbot"); err == nil {
			t.Fatalf("accepted incompatible version %s", version)
		}
	}
}

func TestV20RejectsPositionOnAnotherMap(t *testing.T) {
	b, ft := newTestBus(t)
	b.robot.store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{MapID: "active-map", PositionInitialized: true}})
	o := order.Order{OrderID: "o1", Nodes: []order.Node{{NodeID: "n0", Released: true, NodePosition: &order.NodePosition{MapID: "other-map"}}}}
	called := false
	b.SetOrderAcceptedHook(func(order.Order) { called = true })
	b.handleOrder(mustOrderBytes(t, o))
	if called || !hasError(lastState(t, b, ft), state.ErrorTypeOrderError) {
		t.Fatalf("cross-map instruction was not rejected")
	}
}

func TestV20FactsheetOmitsLaterFields(t *testing.T) {
	b, _ := newTestBus(t)
	raw, err := json.Marshal(b.buildFactsheet())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"vehicleConfig":`, `"blockingTypes":`, `"mobileRobotActions":`} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("later-edition field %s: %s", field, raw)
		}
	}
}

func TestV20MergePreservesOriginalStitchInstructions(t *testing.T) {
	base := order.Order{OrderID: "o1", Nodes: []order.Node{{NodeID: "n0", Released: true, Actions: []order.Action{{ActionID: "original", ActionType: "startCharging"}}}}}
	update := order.Order{OrderID: "o1", OrderUpdateID: 1, Nodes: []order.Node{{NodeID: "n0", Released: true, Actions: []order.Action{{ActionID: "changed", ActionType: "stopCharging"}}}}}
	got := mergeOrder(base, update)
	if len(got.Nodes) != 1 || len(got.Nodes[0].Actions) != 1 || got.Nodes[0].Actions[0].ActionID != "original" {
		t.Fatalf("update replaced released stitch instructions: %+v", got)
	}
}
