package executor

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
)

func param(key string, value any) order.ActionParameter {
	return order.ActionParameter{Key: key, Value: value}
}

func relocalizeAction(id, mapID string, x, y, theta float64) order.Action {
	return order.Action{
		ActionID:   id,
		ActionType: "initializePosition",
		ActionParameters: []order.ActionParameter{
			param("x", x), param("y", y), param("theta", theta), param("mapId", mapID), param("lastNodeId", "n7"),
		},
	}
}

// seedPosition sets only the robot's reported pose (no active order), so the
// executor is idle. Localized:false reflects the primary use case: a delocalized
// robot still reports the map it is on.
func seedPosition(s *vehiclestate.Store, mapID string) {
	s.SetTelemetry(vehiclestate.Telemetry{
		MobileRobotPosition: &state.MobileRobotPosition{X: 1, Y: 2, MapID: mapID, Localized: false},
	})
}

func instantResult(s *vehiclestate.Store, actionID string) string {
	for _, as := range s.Snapshot().InstantActionStates {
		if as.ActionID == actionID {
			return as.ActionResult
		}
	}
	return ""
}

func TestExecutor_RelocalizeRunsToFinished(t *testing.T) {
	store := vehiclestate.New()
	seedPosition(store, "map-1")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := relocalizeAction("r1", "map-1", 1.5, -2.0, 0.75)
	acceptInstant(store, a)
	e.InstantAction(a)

	select {
	case m := <-cmd.relocs:
		if m.x != 1.5 || m.y != -2.0 || m.theta != 0.75 {
			t.Errorf("Relocalize pose = %+v, want {1.5 -2 0.75}", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no Relocalize command dispatched")
	}
	waitInstantStatus(t, store, "r1", state.ActionStatusRunning)

	cmd.events <- robotbus.ExecutionEvent{RelocalizeCompleted: &robotbus.RelocalizeCompleted{Success: true, Score: 0.95}}
	waitInstantStatus(t, store, "r1", state.ActionStatusFinished)
	if got := store.Snapshot().LastNodeID; got != "n7" {
		t.Fatalf("lastNodeId = %q, want n7", got)
	}
	pose := store.Snapshot().MobileRobotPosition
	if pose == nil || !pose.Localized || pose.X != 1.5 || pose.Y != -2 || pose.Theta != 0.75 || pose.MapID != "map-1" {
		t.Fatalf("FINISHED must report the confirmed pose, got %+v", pose)
	}
}

func TestExecutor_RelocalizeFailureReportsScore(t *testing.T) {
	store := vehiclestate.New()
	seedPosition(store, "map-1")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := relocalizeAction("r2", "map-1", 1.5, 2.0, 0.75)
	acceptInstant(store, a)
	e.InstantAction(a)

	select {
	case <-cmd.relocs:
	case <-time.After(2 * time.Second):
		t.Fatal("no Relocalize command dispatched")
	}

	cmd.events <- robotbus.ExecutionEvent{RelocalizeCompleted: &robotbus.RelocalizeCompleted{Success: false, Score: 0.30}}
	waitInstantStatus(t, store, "r2", state.ActionStatusFailed)
	if res := instantResult(store, "r2"); !strings.Contains(res, "0.30") {
		t.Errorf("ActionResult = %q, want the localization score 0.30 in the reason", res)
	}
}

func TestExecutor_RelocalizeMapMismatchFails(t *testing.T) {
	store := vehiclestate.New()
	seedPosition(store, "map-1")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := relocalizeAction("r3", "map-2", 1.5, 2.0, 0.75) // robot is on map-1
	acceptInstant(store, a)
	e.InstantAction(a)

	waitInstantStatus(t, store, "r3", state.ActionStatusFailed)
	if res := instantResult(store, "r3"); !strings.Contains(res, "map-2") {
		t.Errorf("ActionResult = %q, want the rejected mapId map-2 named in the reason", res)
	}
	select {
	case <-cmd.relocs:
		t.Fatal("Relocalize dispatched for a map the robot is not on")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExecutor_RelocalizeUnknownMapFails(t *testing.T) {
	store := vehiclestate.New() // no MobileRobotPosition: active map unknown
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := relocalizeAction("r4", "map-1", 1.5, 2.0, 0.75)
	acceptInstant(store, a)
	e.InstantAction(a)

	waitInstantStatus(t, store, "r4", state.ActionStatusFailed)
	if res := instantResult(store, "r4"); !strings.Contains(res, "active map") {
		t.Errorf("ActionResult = %q, want a reason that the active map is unknown", res)
	}
	select {
	case <-cmd.relocs:
		t.Fatal("Relocalize dispatched with no known active map")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExecutor_RelocalizeRejectedWhileOrderExecuting(t *testing.T) {
	store := vehiclestate.New()
	seedStore(store, 1.02, 2.03) // active order + on map-1
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	e.OrderAccepted(singleTargetOrder())
	select {
	case <-cmd.moves:
	case <-time.After(2 * time.Second):
		t.Fatal("no move dispatched")
	}

	a := relocalizeAction("r5", "map-1", 1.5, 2.0, 0.75) // matches active map; only busy blocks it
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "r5", state.ActionStatusFailed)
	select {
	case <-cmd.relocs:
		t.Fatal("Relocalize dispatched while an order was executing")
	case <-time.After(100 * time.Millisecond):
	}

	// the running order is undisturbed
	cmd.events <- robotbus.ExecutionEvent{MoveCompleted: &robotbus.MoveCompleted{Success: true}}
	waitLastNode(t, store, "n1")
}

func TestExecutor_RelocalizeRejectedWhileCharging(t *testing.T) {
	store := vehiclestate.New()
	seedPosition(store, "map-1")
	store.SetTelemetry(vehiclestate.Telemetry{MobileRobotPosition: &state.MobileRobotPosition{Localized: true, MapID: "map-1"}})
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	// a dock (charging) is in flight
	charge := order.Action{ActionID: "ch1", ActionType: "startCharging"}
	acceptInstant(store, charge)
	e.InstantAction(charge)
	select {
	case <-cmd.chargeStarts:
	case <-time.After(2 * time.Second):
		t.Fatal("no StartCharging dispatched")
	}

	a := relocalizeAction("r8", "map-1", 1.5, 2.0, 0.75) // matches active map; only busy blocks it
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "r8", state.ActionStatusFailed)
	select {
	case <-cmd.relocs:
		t.Fatal("Relocalize dispatched while a dock was in flight")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExecutor_RelocalizeDispatchErrorFails(t *testing.T) {
	store := vehiclestate.New()
	seedPosition(store, "map-1")
	cmd := newFakeCommander()
	cmd.relocErr = errors.New("ws down")
	e := New(store, cmd)
	startExecutor(t, e)

	a := relocalizeAction("r6", "map-1", 1.5, 2.0, 0.75)
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "r6", state.ActionStatusFailed)
	if res := instantResult(store, "r6"); !strings.Contains(res, "dispatch failed") {
		t.Errorf("ActionResult = %q, want a dispatch-failed reason", res)
	}
}

func TestExecutor_RelocalizeMalformedParamsFails(t *testing.T) {
	store := vehiclestate.New()
	seedPosition(store, "map-1")
	cmd := newFakeCommander()
	e := New(store, cmd)
	startExecutor(t, e)

	a := order.Action{
		ActionID:   "r7",
		ActionType: "initializePosition",
		ActionParameters: []order.ActionParameter{
			param("x", 1.5), param("mapId", "map-1"), // missing y, theta
		},
	}
	acceptInstant(store, a)
	e.InstantAction(a)
	waitInstantStatus(t, store, "r7", state.ActionStatusFailed)
	if res := instantResult(store, "r7"); !strings.Contains(res, "theta") {
		t.Errorf("ActionResult = %q, want the missing-param reason", res)
	}
	select {
	case <-cmd.relocs:
		t.Fatal("Relocalize dispatched with malformed params")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRelocalizeParams(t *testing.T) {
	tests := []struct {
		name    string
		params  []order.ActionParameter
		want    relocalizePose
		wantErr bool
	}{
		{
			name: "all fields",
			params: []order.ActionParameter{
				param("x", 1.5), param("y", -2.0), param("theta", 0.75),
				param("mapId", "map-1"), param("lastNodeId", "n7"),
			},
			want: relocalizePose{X: 1.5, Y: -2.0, Theta: 0.75, MapID: "map-1", LastNodeID: "n7"},
		},
		{
			name: "lastNodeId required",
			params: []order.ActionParameter{
				param("x", 1.5), param("y", 2.0), param("theta", 0.75), param("mapId", "map-1"),
			},
			wantErr: true,
		},
		{
			name: "zero pose is valid, not treated as missing",
			params: []order.ActionParameter{
				param("x", 0.0), param("y", 0.0), param("theta", 0.0), param("mapId", "map-1"), param("lastNodeId", ""),
			},
			want: relocalizePose{X: 0, Y: 0, Theta: 0, MapID: "map-1"},
		},
		{
			name:    "missing x",
			params:  []order.ActionParameter{param("y", 2.0), param("theta", 0.75), param("mapId", "map-1")},
			wantErr: true,
		},
		{
			name:    "missing y",
			params:  []order.ActionParameter{param("x", 1.5), param("theta", 0.75), param("mapId", "map-1")},
			wantErr: true,
		},
		{
			name:    "missing theta",
			params:  []order.ActionParameter{param("x", 1.5), param("y", 2.0), param("mapId", "map-1")},
			wantErr: true,
		},
		{
			name:    "missing mapId",
			params:  []order.ActionParameter{param("x", 1.5), param("y", 2.0), param("theta", 0.75)},
			wantErr: true,
		},
		{
			name:    "empty mapId",
			params:  []order.ActionParameter{param("x", 1.5), param("y", 2.0), param("theta", 0.75), param("mapId", "")},
			wantErr: true,
		},
		{
			name:    "x wrong type",
			params:  []order.ActionParameter{param("x", "1.5"), param("y", 2.0), param("theta", 0.75), param("mapId", "map-1")},
			wantErr: true,
		},
		{
			name:    "mapId wrong type",
			params:  []order.ActionParameter{param("x", 1.5), param("y", 2.0), param("theta", 0.75), param("mapId", 3)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relocalizeParams(order.Action{ActionType: "initializePosition", ActionParameters: tt.params})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("relocalizeParams() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("relocalizeParams() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("relocalizeParams() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
