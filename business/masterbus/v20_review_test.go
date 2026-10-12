package masterbus

import (
	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestV20DuplicateIgnoresChangedUnsupportedFields(t *testing.T) {
	b, ft := newTestBus(t)
	o := order.Order{OrderID: "duplicate", Nodes: []order.Node{{NodeID: "n0", Released: true}}}
	b.handleOrder(mustOrderBytes(t, o))
	o.ZoneSetID = "unsupported"
	b.handleOrder(mustOrderBytes(t, o))
	if got := lastState(t, b, ft); len(got.Errors) != 0 {
		t.Fatalf("duplicate identity must be ignored: %+v", got.Errors)
	}
}

func TestV20InvalidCleaningParametersRejectedBeforeAcceptance(t *testing.T) {
	b, ft := newTestBus(t)
	called := false
	b.SetOrderAcceptedHook(func(order.Order) { called = true })
	o := order.Order{OrderID: "invalid-clean", Nodes: []order.Node{{NodeID: "n0", Released: true, Actions: []order.Action{{ActionID: "clean", ActionType: "startCleaning", BlockingType: order.BlockingTypeHard}}}}}
	b.handleOrder(mustOrderBytes(t, o))
	got := lastState(t, b, ft)
	if called || got.OrderID != "" || !hasError(got, state.ErrorTypeOrderError) {
		t.Fatalf("invalid clean accepted: called=%v state=%+v", called, got)
	}
}

func TestV20RejectsUnsupportedChargingStation(t *testing.T) {
	b, ft := newTestBus(t)
	called := false
	b.SetInstantActionHook(func(order.Action) { called = true })
	b.handleInstantActions([]byte(`{"headerId":1,"timestamp":"2026-10-10T00:00:00.00Z","version":"2.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"dock","actionType":"startCharging","blockingType":"HARD","actionParameters":[{"key":"stationName","value":"other-station"}]}]}`))
	got := lastState(t, b, ft)
	if called || len(got.ActionStates) != 1 || got.ActionStates[0].ActionStatus != state.ActionStatusFailed {
		t.Fatalf("unsupported station dispatched: called=%v state=%+v", called, got)
	}
}

func TestV20DuplicateInstantActionIsDispatchedOnce(t *testing.T) {
	b, ft := newTestBus(t)
	calls := 0
	b.SetInstantActionHook(func(order.Action) { calls++ })
	msg := []byte(`{"headerId":1,"timestamp":"2026-10-10T00:00:00.00Z","version":"2.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"dock","actionType":"startCharging","blockingType":"HARD"},{"actionId":"dock","actionType":"startCharging","blockingType":"HARD"}]}`)
	b.handleInstantActions(msg)
	b.handleInstantActions(msg)
	got := lastState(t, b, ft)
	if calls != 1 || len(got.ActionStates) != 1 {
		t.Fatalf("retransmission duplicated action: calls=%d states=%+v", calls, got.ActionStates)
	}
}

func TestV20RejectsNavigationWithoutMasterControlOrLocalization(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode state.OperatingMode
		pos  *state.AGVPosition
	}{
		{"manual", state.OperatingModeManual, &state.AGVPosition{MapID: "map-1", PositionInitialized: true}},
		{"service", state.OperatingModeService, &state.AGVPosition{MapID: "map-1", PositionInitialized: true}},
		{"teachin", state.OperatingModeTeachIn, &state.AGVPosition{MapID: "map-1", PositionInitialized: true}},
		{"unknown pose", state.OperatingModeAutomatic, nil},
		{"uninitialized", state.OperatingModeAutomatic, &state.AGVPosition{MapID: "map-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ft := newTestBus(t)
			b.robot.store.SetTelemetry(vehiclestate.Telemetry{OperatingMode: &tc.mode, AGVPosition: tc.pos})
			called := false
			b.SetOrderAcceptedHook(func(order.Order) { called = true })
			b.handleOrder(mustOrderBytes(t, order.Order{OrderID: "not-ready", Nodes: []order.Node{{NodeID: "n0", Released: true}, {NodeID: "n1", SequenceID: 2, Released: true}}, Edges: []order.Edge{{EdgeID: "e", SequenceID: 1, Released: true}}}))
			got := lastState(t, b, ft)
			if called || got.OrderID != "" || !hasError(got, state.ErrorTypeOrderError) {
				t.Fatalf("unready navigation accepted: %+v", got)
			}
		})
	}
}
