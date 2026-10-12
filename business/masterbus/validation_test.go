package masterbus

import (
	"encoding/json"
	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"testing"
)

func TestRawMessagesRejectInvalidEnvelope(t *testing.T) {
	for _, instant := range []bool{false, true} {
		for _, field := range []string{"headerId", "timestamp", "version", "manufacturer", "serialNumber", "wrongVersion", "wrongVehicle"} {
			name := field
			if instant {
				name = "instant/" + name
			}
			t.Run(name, func(t *testing.T) {
				b, ft := newTestBus(t)
				dispatched := false
				b.SetOrderAcceptedHook(func(order.Order) { dispatched = true })
				b.SetInstantActionHook(func(order.Action) { dispatched = true })
				doc := map[string]any{"headerId": 0, "timestamp": "2026-10-10T00:00:00.000Z", "version": "3.0.0", "manufacturer": "lionsbot", "serialNumber": "TESTBOT"}
				if instant {
					doc["actions"] = []any{map[string]any{"actionId": "a1", "actionType": "startPause", "blockingType": "NONE"}}
				} else {
					doc["orderId"] = "o1"
					doc["orderUpdateId"] = 0
					doc["nodes"] = []any{map[string]any{"nodeId": "n0", "sequenceId": 0, "released": true, "actions": []any{}}}
					doc["edges"] = []any{}
				}
				switch field {
				case "wrongVersion":
					doc["version"] = "2.1.0"
				case "wrongVehicle":
					doc["serialNumber"] = "OTHER"
				default:
					delete(doc, field)
				}
				raw, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				if instant {
					b.handleInstantActions(raw)
				} else {
					b.handleOrder(raw)
				}
				if dispatched {
					t.Fatal("invalid message reached executor")
				}
				if !hasError(lastState(t, b, ft), state.ErrorTypeValidationFailure) {
					t.Fatal("missing validation failure")
				}
			})
		}
	}
}

func TestUnsupportedMotionConstraintsRejectedBeforeDispatch(t *testing.T) {
	for _, field := range []string{"speed", "length", "ellipse", "angularTolerance", "retry"} {
		t.Run(field, func(t *testing.T) {
			b, ft := newTestBus(t)
			dispatched := false
			b.SetOrderAcceptedHook(func(order.Order) { dispatched = true })
			o := order.Order{OrderID: "o1", Nodes: []order.Node{{NodeID: "n0", Released: true}, {NodeID: "n1", SequenceID: 2, Released: true, NodePosition: &order.NodePosition{X: 1, MapID: "map-1"}}}, Edges: []order.Edge{{EdgeID: "e1", SequenceID: 1, Released: true}}}
			switch field {
			case "speed":
				o.Edges[0].MaximumSpeed = new(0.1)
			case "length":
				o.Edges[0].Length = new(5.0)
			case "ellipse":
				o.Nodes[1].NodePosition.AllowedDeviationXY = &order.AllowedDeviationXY{A: 2, B: 1}
			case "angularTolerance":
				o.Nodes[1].NodePosition.AllowedDeviationTheta = new(0.1)
			case "retry":
				o.Nodes[0].Actions = []order.Action{{ActionID: "a1", ActionType: "startCharging", BlockingType: order.BlockingTypeHard, Retriable: new(true)}}
			}
			b.handleOrder(mustOrderBytes(t, o))
			got := lastState(t, b, ft)
			e, ok := findError(got, state.ErrorTypeUnsupportedParameter)
			if dispatched || got.OrderID != "" || !ok || e.ErrorLevel != state.ErrorLevelCritical {
				t.Fatalf("unsupported constraint accepted or wrongly reported: %+v", got)
			}
		})
	}
}

func TestV3InvalidCleaningParametersRejectedBeforeAcceptance(t *testing.T) {
	b, ft := newTestBus(t)
	called := false
	b.SetOrderAcceptedHook(func(order.Order) { called = true })
	o := order.Order{OrderID: "invalid-clean", Nodes: []order.Node{{NodeID: "n0", Released: true, Actions: []order.Action{{ActionID: "clean", ActionType: "startCleaning", BlockingType: order.BlockingTypeHard}}}}}
	b.handleOrder(mustOrderBytes(t, o))
	got := lastState(t, b, ft)
	if called || got.OrderID != "" || !hasError(got, state.ErrorTypeInvalidOrderAction) {
		t.Fatalf("invalid clean accepted: called=%v state=%+v", called, got)
	}
}

func TestV3RejectsUnsupportedChargingStation(t *testing.T) {
	b, ft := newTestBus(t)
	called := false
	b.SetInstantActionHook(func(order.Action) { called = true })
	b.handleInstantActions([]byte(`{"headerId":1,"timestamp":"2026-10-10T00:00:00.00Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"dock","actionType":"startCharging","blockingType":"NONE","actionParameters":[{"key":"stationName","value":"other-station"}]}]}`))
	got := lastState(t, b, ft)
	if called || len(got.InstantActionStates) != 1 || got.InstantActionStates[0].ActionStatus != state.ActionStatusFailed {
		t.Fatalf("unsupported station dispatched: called=%v state=%+v", called, got)
	}
}

func TestV3DuplicateInstantActionIsDispatchedOnce(t *testing.T) {
	b, ft := newTestBus(t)
	calls := 0
	b.SetInstantActionHook(func(order.Action) { calls++ })
	msg := []byte(`{"headerId":1,"timestamp":"2026-10-10T00:00:00.00Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"dock","actionType":"startCharging","blockingType":"NONE"},{"actionId":"dock","actionType":"startCharging","blockingType":"NONE"}]}`)
	b.handleInstantActions(msg)
	b.handleInstantActions(msg)
	got := lastState(t, b, ft)
	if calls != 1 || len(got.InstantActionStates) != 1 {
		t.Fatalf("retransmission duplicated action: calls=%d states=%+v", calls, got.InstantActionStates)
	}
}

func TestDuplicateInstantActionDoesNotDispatchTwice(t *testing.T) {
	b, ft := newTestBus(t)
	calls := 0
	b.SetInstantActionHook(func(order.Action) { calls++ })
	raw := []byte(`{"headerId":1,"timestamp":"2026-10-10T00:00:00.000Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"pause","actionType":"startPause","blockingType":"NONE"},{"actionId":"pause","actionType":"startPause","blockingType":"NONE"}]}`)
	b.handleInstantActions(raw)
	b.robot.store.SetInstantActionStatus("pause", state.ActionStatusFinished, "")
	b.handleInstantActions(raw)
	got := lastState(t, b, ft)
	if calls != 1 || len(got.InstantActionStates) != 1 || got.InstantActionStates[0].ActionStatus != state.ActionStatusFinished {
		t.Fatalf("duplicate redispatched or reset: calls=%d state=%+v", calls, got.InstantActionStates)
	}
}

func TestOrderAdmissionRespectsOperatingMode(t *testing.T) {
	for _, mode := range []state.OperatingMode{state.OperatingModeManual, state.OperatingModeService, state.OperatingModeTeachIn, state.OperatingModeStartup, state.OperatingModeIntervened} {
		t.Run(string(mode), func(t *testing.T) {
			b, ft := newTestBus(t)
			b.robot.store.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &mode})
			accepted := false
			b.SetOrderAcceptedHook(func(order.Order) { accepted = true })
			b.handleOrder(mustOrderBytes(t, order.Order{OrderID: "order-1", Nodes: []order.Node{{NodeID: "n0", Released: true}}}))
			if mode == state.OperatingModeIntervened {
				if !accepted {
					t.Fatal("INTERVENED must buffer orders")
				}
			} else if accepted || !hasError(lastState(t, b, ft), state.ErrorTypeMobileRobotNotAvailable) {
				t.Fatal("unavailable mode accepted order or omitted rejection")
			}
		})
	}
}
