package executor

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

// cleaningOrder mirrors the dashboard's Clean button: one positionless seq-0
// released node carrying startCleaning.
func cleaningOrder() order.Order {
	return order.Order{
		OrderID: "clean-1",
		Nodes: []order.Node{
			{NodeID: "n0", SequenceID: 0, Released: true, Actions: []order.Action{{
				ActionID:   "a1",
				ActionType: "startCleaning",
				ActionParameters: []order.ActionParameter{
					{Key: "zones", Value: []any{map[string]any{"name": "Zone A"}}},
					{Key: "bypassBlackout", Value: true},
				},
			}}},
		},
		Edges: []order.Edge{},
	}
}

// seedCleaningStore commits the projection masterbus builds for cleaningOrder.
func seedCleaningStore(s *vehiclestate.Store) {
	s.CommitOrderProjection(vehiclestate.OrderProjection{
		OrderID: "clean-1", LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{}, EdgeStates: []state.EdgeState{},
		ActionStates: []state.ActionState{
			{ActionID: "a1", ActionType: "startCleaning", ActionStatus: state.ActionStatusWaiting},
		},
	})
	s.SetTelemetry(vehiclestate.Telemetry{
		AGVPosition: &state.AGVPosition{X: 1, Y: 2, MapID: "map-1", PositionInitialized: true},
	})
}

func waitActionStatus(t *testing.T, s *vehiclestate.Store, actionID string, want state.ActionStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range s.Snapshot().ActionStates {
			if a.ActionID == actionID && a.ActionStatus == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("action %s = %+v, want %s", actionID, s.Snapshot().ActionStates, want)
}

func TestExecutor_CleaningOrder_DispatchesAndFinishes(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())

	select {
	case got := <-cmd.cleanStarts:
		want := cleanStart{zones: []robotbus.ZoneRef{{Name: "Zone A"}}, bypass: true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("StartCleaning = %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartCleaning not dispatched")
	}
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)
	if vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle idle while a clean is running")
	}

	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: true}}

	waitActionStatus(t, store, "a1", state.ActionStatusFinished)
	if !vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle not idle after the clean finished")
	}
}

func actionState(t *testing.T, s *vehiclestate.Store, actionID string) state.ActionState {
	t.Helper()
	for _, a := range s.Snapshot().ActionStates {
		if a.ActionID == actionID {
			return a
		}
	}
	t.Fatalf("action %s not in ActionStates", actionID)
	return state.ActionState{}
}

func TestExecutor_CleaningOrder_FailureEventFailsAction(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())
	<-cmd.cleanStarts
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)

	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: false, Reason: "brush jam"}}

	waitActionStatus(t, store, "a1", state.ActionStatusFailed)
	if got := actionState(t, store, "a1"); got.ResultDescription != "brush jam" {
		t.Errorf("resultDescription = %q, want brush jam", got.ResultDescription)
	}
	if !vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle not idle after the clean failed")
	}
}

func TestExecutor_CleaningOrder_DispatchErrorFailsAction(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	cmd.cleanErr = errors.New("ws down")
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())

	waitActionStatus(t, store, "a1", state.ActionStatusFailed)
	select {
	case <-cmd.cleanStarts:
		t.Fatal("StartCleaning dispatched despite write error")
	default:
	}

	// a later end event has no in-flight clean to resolve: FAILED must hold
	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: true}}
	time.Sleep(50 * time.Millisecond)
	if got := actionState(t, store, "a1"); got.ActionStatus != state.ActionStatusFailed {
		t.Errorf("action = %+v, stale end event must not resurrect it", got)
	}
}

func TestExecutor_CleaningOrder_MalformedParamsFailWithoutDispatch(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	o := cleaningOrder()
	o.Nodes[0].Actions[0].ActionParameters = nil // no zones
	e.OrderAccepted(o)

	waitActionStatus(t, store, "a1", state.ActionStatusFailed)
	select {
	case <-cmd.cleanStarts:
		t.Fatal("StartCleaning dispatched despite malformed params")
	default:
	}
	if !vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle wedged by a malformed clean action")
	}
}

func TestExecutor_CancelMidClean_LateEventIgnored(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())
	<-cmd.cleanStarts
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)

	cancel := order.Action{ActionID: "ia-cancel", ActionType: "cancelOrder"}
	acceptInstant(store, cancel)
	e.InstantAction(cancel)

	// spec 6.1.3 (:477): a running clean must be cancelled on the robot, not
	// just dropped locally — route the stop by op kind (clean_stop, not p2p_stop)
	select {
	case <-cmd.cleanStops:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelOrder mid-clean did not send clean_stop")
	}

	if vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("order became idle before robot completion")
	}
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)
	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: true}}
	waitActionStatus(t, store, "a1", state.ActionStatusFailed)
	if !vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle not idle after cancel")
	}

	// the robot's own end status arrives after the cancel: stale, ignored
	cmd.events <- robotbus.ExecutionEvent{CleanCompleted: &robotbus.CleanCompleted{Success: true}}
	time.Sleep(50 * time.Millisecond)
	if got := actionState(t, store, "a1"); got.ActionStatus != state.ActionStatusFailed {
		t.Errorf("action = %+v, late end event must not resurrect a cancelled clean", got)
	}
}

func TestExecutor_CancelMidClean_StopErrorFailsCancel(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	store.AddInstantActionState(state.ActionState{ActionID: "ia-cancel", ActionType: "cancelOrder", ActionStatus: state.ActionStatusWaiting})
	cmd := newFakeCommander()
	cmd.cleanStopErr = errors.New("ws down")
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())
	<-cmd.cleanStarts
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)

	e.InstantAction(order.Action{ActionID: "ia-cancel", ActionType: "cancelOrder"})

	// the stop could not be dispatched: the cancel fails and the clean stays
	// held (RUNNING) so a real clean_end still resolves it, never a false idle
	waitInstantStatus(t, store, "ia-cancel", state.ActionStatusFailed)
	if got := actionState(t, store, "a1"); got.ActionStatus != state.ActionStatusRunning {
		t.Errorf("clean action = %+v, want still RUNNING after a failed cancel stop", got)
	}
	if vehiclestate.IsIdle(store.Snapshot()) {
		t.Fatal("vehicle went idle despite a clean that could not be stopped")
	}
}

func TestExecutor_PauseResumeMidClean_RoutesByOpKind(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())
	<-cmd.cleanStarts
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)

	// startPause during a clean: clean_pause, never p2p_pause
	pause := order.Action{ActionID: "cp1", ActionType: "startPause"}
	acceptInstant(store, pause)
	e.InstantAction(pause)
	select {
	case <-cmd.cleanPauses:
	case <-time.After(2 * time.Second):
		t.Fatal("startPause mid-clean did not send clean_pause")
	}
	select {
	case <-cmd.pauses:
		t.Fatal("sent p2p_pause for a clean; pause must route by op kind")
	case <-time.After(50 * time.Millisecond):
	}
	waitPaused(t, store, true)
	waitInstantStatus(t, store, "cp1", state.ActionStatusFinished)

	// stopPause: clean_continue carrying the original bypassBlackout (true here)
	resume := order.Action{ActionID: "cp2", ActionType: "stopPause"}
	acceptInstant(store, resume)
	e.InstantAction(resume)
	select {
	case bypass := <-cmd.cleanResumes:
		if !bypass {
			t.Errorf("clean_continue bypass_blackout = false, want the order's true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopPause mid-clean did not send clean_continue")
	}
	select {
	case <-cmd.resumes:
		t.Fatal("sent p2p_continue for a clean; resume must route by op kind")
	case <-time.After(50 * time.Millisecond):
	}
	waitPaused(t, store, false)
	waitInstantStatus(t, store, "cp2", state.ActionStatusFinished)
}

func TestExecutor_ChargingRejectedWhileCleaning(t *testing.T) {
	store := vehiclestate.New()
	seedCleaningStore(store)
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(cleaningOrder())
	<-cmd.cleanStarts
	waitActionStatus(t, store, "a1", state.ActionStatusRunning)

	store.AddInstantActionState(state.ActionState{ActionID: "ia-dock", ActionType: "startCharging", ActionStatus: state.ActionStatusWaiting})
	e.InstantAction(order.Action{ActionID: "ia-dock", ActionType: "startCharging"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range store.Snapshot().InstantActionStates {
			if a.ActionID == "ia-dock" && a.ActionStatus == state.ActionStatusFailed {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("instant actions = %+v, want ia-dock FAILED while a clean is running", store.Snapshot().InstantActionStates)
}

func TestCleaningParams(t *testing.T) {
	// values shaped as encoding/json produces them from the wire
	action := func(params ...order.ActionParameter) order.Action {
		return order.Action{ActionID: "a1", ActionType: "startCleaning", ActionParameters: params}
	}
	zones := func(names ...string) order.ActionParameter {
		vs := make([]any, 0, len(names))
		for _, n := range names {
			vs = append(vs, map[string]any{"name": n})
		}
		return order.ActionParameter{Key: "zones", Value: vs}
	}

	tests := []struct {
		name       string
		a          order.Action
		wantZones  []robotbus.ZoneRef
		wantBypass bool
		wantErr    bool
	}{
		{
			name:      "single zone",
			a:         action(zones("Zone A")),
			wantZones: []robotbus.ZoneRef{{Name: "Zone A"}},
		},
		{
			name:      "multiple zones keep order",
			a:         action(zones("Zone A", "Zone B")),
			wantZones: []robotbus.ZoneRef{{Name: "Zone A"}, {Name: "Zone B"}},
		},
		{
			name:       "bypassBlackout true",
			a:          action(zones("Zone A"), order.ActionParameter{Key: "bypassBlackout", Value: true}),
			wantZones:  []robotbus.ZoneRef{{Name: "Zone A"}},
			wantBypass: true,
		},
		{
			name:      "homePoi ignored",
			a:         action(zones("Zone A"), order.ActionParameter{Key: "homePoi", Value: map[string]any{"name": "Home"}}),
			wantZones: []robotbus.ZoneRef{{Name: "Zone A"}},
		},
		{
			name: "per-zone cleaningMode override",
			a: action(order.ActionParameter{Key: "zones", Value: []any{
				map[string]any{"name": "Zone A", "cleaningMode": "Eco"},
			}}),
			wantZones: []robotbus.ZoneRef{{Name: "Zone A", CleaningMode: "Eco"}},
		},
		{
			name: "per-zone configs override (numbers off the wire)",
			a: action(order.ActionParameter{Key: "zones", Value: []any{
				map[string]any{"name": "Zone A", "cleaningMode": "Custom", "configs": map[string]any{"water": float64(9), "brush": float64(50)}},
			}}),
			wantZones: []robotbus.ZoneRef{{Name: "Zone A", CleaningMode: "Custom", Configs: map[string]float64{"water": 9, "brush": 50}}},
		},
		{
			name: "empty configs object is not an override",
			a: action(order.ActionParameter{Key: "zones", Value: []any{
				map[string]any{"name": "Zone A", "configs": map[string]any{}},
			}}),
			wantZones: []robotbus.ZoneRef{{Name: "Zone A"}},
		},
		{
			name: "configs not an object errors",
			a: action(order.ActionParameter{Key: "zones", Value: []any{
				map[string]any{"name": "Zone A", "configs": "loud"},
			}}),
			wantErr: true,
		},
		{
			name: "configs value not a number errors",
			a: action(order.ActionParameter{Key: "zones", Value: []any{
				map[string]any{"name": "Zone A", "configs": map[string]any{"water": "high"}},
			}}),
			wantErr: true,
		},
		{
			name:    "missing zones param",
			a:       action(),
			wantErr: true,
		},
		{
			name:    "empty zones array",
			a:       action(order.ActionParameter{Key: "zones", Value: []any{}}),
			wantErr: true,
		},
		{
			name:    "zone entry not an object",
			a:       action(order.ActionParameter{Key: "zones", Value: []any{"Zone A"}}),
			wantErr: true,
		},
		{
			name:    "zone entry without a name",
			a:       action(order.ActionParameter{Key: "zones", Value: []any{map[string]any{"name": ""}}}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotZones, gotBypass, err := cleaningParams(tt.a)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("cleaningParams = (%v, %v, nil), want error", gotZones, gotBypass)
				}
				return
			}
			if err != nil {
				t.Fatalf("cleaningParams error = %v", err)
			}
			if !reflect.DeepEqual(gotZones, tt.wantZones) || gotBypass != tt.wantBypass {
				t.Fatalf("cleaningParams = (%v, %v), want (%v, %v)", gotZones, gotBypass, tt.wantZones, tt.wantBypass)
			}
		})
	}
}
