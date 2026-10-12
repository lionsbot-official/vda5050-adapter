package vehiclestate

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestCommitTraversal(t *testing.T) {
	s := New()
	s.CommitOrderProjection(OrderProjection{
		OrderID: "o1", OrderUpdateID: 0,
		LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{
			{NodeID: "n1", SequenceID: 2, Released: true},
			{NodeID: "n2", SequenceID: 4, Released: true},
		},
		EdgeStates: []state.EdgeState{
			{EdgeID: "e01", SequenceID: 1, Released: true},
			{EdgeID: "e12", SequenceID: 3, Released: true},
		},
		ActionStates: []state.ActionState{},
	})
	s.SetTelemetry(Telemetry{Driving: boolPtr(true)})

	s.CommitTraversal("n1", 2)

	got := s.Snapshot()
	if got.LastNodeID != "n1" || got.LastNodeSequenceID != 2 {
		t.Errorf("lastNode = %s/%d, want n1/2", got.LastNodeID, got.LastNodeSequenceID)
	}
	if len(got.NodeStates) != 1 || got.NodeStates[0].NodeID != "n2" {
		t.Errorf("nodeStates = %+v, want only n2", got.NodeStates)
	}
	if len(got.EdgeStates) != 1 || got.EdgeStates[0].EdgeID != "e12" {
		t.Errorf("edgeStates = %+v, want only e12", got.EdgeStates)
	}
	if got.Driving != true {
		t.Error("telemetry field disturbed by CommitTraversal")
	}
	if got.OrderID != "o1" {
		t.Error("order identity disturbed by CommitTraversal")
	}

	select {
	case <-s.Changes():
	default:
		t.Error("CommitTraversal did not signal Changes")
	}
}

func TestChanges_Coalesce(t *testing.T) {
	s := New()
	s.CommitTraversal("a", 2)
	s.CommitTraversal("b", 4) // second signal must not block while unconsumed
	select {
	case <-s.Changes():
	default:
		t.Fatal("no change signal pending")
	}
	select {
	case <-s.Changes():
		t.Fatal("expected coalesced (single) pending signal")
	default:
	}
}

func boolPtr(b bool) *bool { return &b }

func drain(s *Store) {
	select {
	case <-s.Changes():
	default:
	}
}

func TestSetTelemetry_NotifiesOnRelevantDeltasOnly(t *testing.T) {
	estop := state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopManual}
	modeManual := state.OperatingModeManual
	cases := []struct {
		name          string
		first, second Telemetry
		wantSignal    bool
	}{
		{"driving flip signals",
			Telemetry{Driving: boolPtr(false)}, Telemetry{Driving: boolPtr(true)}, true},
		{"estop transition signals",
			Telemetry{}, Telemetry{SafetyState: &estop}, true},
		{"operating mode change signals",
			Telemetry{}, Telemetry{OperatingMode: &modeManual}, true},
		{"soc drift alone does not signal",
			Telemetry{PowerSupply: &state.PowerSupply{StateOfCharge: 80}},
			Telemetry{PowerSupply: &state.PowerSupply{StateOfCharge: 79}}, false},
		{"same values re-sent do not signal",
			Telemetry{Driving: boolPtr(true)}, Telemetry{Driving: boolPtr(true)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			s.SetTelemetry(tc.first)
			drain(s)
			s.SetTelemetry(tc.second)
			select {
			case <-s.Changes():
				if !tc.wantSignal {
					t.Error("unexpected change signal for irrelevant delta")
				}
			default:
				if tc.wantSignal {
					t.Error("no change signal for relevant delta")
				}
			}
		})
	}
}

func TestCommitOrderProjection_KeepLastNodePreservesTraversal(t *testing.T) {
	s := New()
	s.CommitOrderProjection(OrderProjection{
		OrderID: "o1", LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{
			{NodeID: "n1", SequenceID: 2, Released: true},
			{NodeID: "n2", SequenceID: 4, Released: true},
		},
		EdgeStates: []state.EdgeState{
			{EdgeID: "e01", SequenceID: 1, Released: true},
			{EdgeID: "e12", SequenceID: 3, Released: true},
		},
		ActionStates: []state.ActionState{},
	})
	s.CommitTraversal("n1", 2)

	// an order update rebuilds from the merged order, whose projection still
	// starts at the seq-0 node and re-lists already-traversed nodes
	s.CommitOrderProjection(OrderProjection{
		OrderID: "o1", OrderUpdateID: 1,
		LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{
			{NodeID: "n1", SequenceID: 2, Released: true},
			{NodeID: "n2", SequenceID: 4, Released: true},
			{NodeID: "n3", SequenceID: 6, Released: true},
		},
		EdgeStates: []state.EdgeState{
			{EdgeID: "e01", SequenceID: 1, Released: true},
			{EdgeID: "e12", SequenceID: 3, Released: true},
			{EdgeID: "e23", SequenceID: 5, Released: true},
		},
		ActionStates: []state.ActionState{},
		KeepLastNode: true,
	})

	got := s.Snapshot()
	if got.LastNodeID != "n1" || got.LastNodeSequenceID != 2 {
		t.Errorf("lastNode = %s/%d, want preserved n1/2", got.LastNodeID, got.LastNodeSequenceID)
	}
	if len(got.NodeStates) != 2 || got.NodeStates[0].NodeID != "n2" || got.NodeStates[1].NodeID != "n3" {
		t.Errorf("nodeStates = %+v, want traversed n1 filtered out", got.NodeStates)
	}
	if len(got.EdgeStates) != 2 || got.EdgeStates[0].EdgeID != "e12" {
		t.Errorf("edgeStates = %+v, want traversed edges filtered out", got.EdgeStates)
	}
	if got.OrderUpdateID != 1 {
		t.Errorf("orderUpdateId = %d, want 1 (identity fields still applied)", got.OrderUpdateID)
	}
}

func TestAddInstantActionState_AppendsAndNotifies(t *testing.T) {
	s := New()
	select {
	case <-s.Changes():
	default:
	}

	s.AddInstantActionState(state.ActionState{ActionID: "ia-1", ActionType: "startCharging", ActionStatus: state.ActionStatusWaiting})

	snap := s.Snapshot()
	if len(snap.InstantActionStates) != 1 || snap.InstantActionStates[0].ActionID != "ia-1" {
		t.Fatalf("InstantActionStates = %+v, want ia-1", snap.InstantActionStates)
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("expected change notification")
	}
}

func TestSetInstantActionStatus(t *testing.T) {
	s := New()
	s.AddInstantActionState(state.ActionState{ActionID: "ia-1", ActionType: "startCharging", ActionStatus: state.ActionStatusWaiting})
	select {
	case <-s.Changes():
	default:
	}

	s.SetInstantActionStatus("ia-1", state.ActionStatusFailed, "stuck")

	snap := s.Snapshot()
	if got := snap.InstantActionStates[0]; got.ActionStatus != state.ActionStatusFailed || got.ActionResult != "stuck" {
		t.Fatalf("instant action = %+v, want FAILED/stuck", got)
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("expected change notification")
	}

	// unknown id: no-op, no notification
	s.SetInstantActionStatus("nope", state.ActionStatusFinished, "")
	select {
	case <-s.Changes():
		t.Fatal("unexpected notification for unknown actionId")
	default:
	}
}

func TestSetActionStatus(t *testing.T) {
	s := New()
	s.CommitOrderProjection(OrderProjection{
		OrderID: "clean-1", LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{}, EdgeStates: []state.EdgeState{},
		ActionStates: []state.ActionState{
			{ActionID: "a1", ActionType: "startCleaning", ActionStatus: state.ActionStatusWaiting},
		},
	})
	select {
	case <-s.Changes():
	default:
	}

	s.SetActionStatus("a1", state.ActionStatusFailed, "zone not found")

	snap := s.Snapshot()
	if got := snap.ActionStates[0]; got.ActionStatus != state.ActionStatusFailed || got.ActionResult != "zone not found" {
		t.Fatalf("action = %+v, want FAILED/zone not found", got)
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("expected change notification")
	}

	// unknown id: no-op, no notification
	s.SetActionStatus("nope", state.ActionStatusFinished, "")
	select {
	case <-s.Changes():
		t.Fatal("unexpected notification for unknown actionId")
	default:
	}
}

func TestCancelOrder(t *testing.T) {
	s := New()
	s.CommitOrderProjection(OrderProjection{
		OrderID: "o1", OrderUpdateID: 2, LastNodeID: "n0", LastNodeSequenceID: 0,
		NodeStates: []state.NodeState{{NodeID: "n1", SequenceID: 2, Released: true}},
		EdgeStates: []state.EdgeState{{EdgeID: "e01", SequenceID: 1, Released: true}},
		ActionStates: []state.ActionState{
			{ActionID: "a1", ActionStatus: state.ActionStatusWaiting},
			{ActionID: "a2", ActionStatus: state.ActionStatusFinished},
		},
	})
	select {
	case <-s.Changes():
	default:
	}

	s.CancelOrder()

	snap := s.Snapshot()
	if len(snap.NodeStates) != 0 || len(snap.EdgeStates) != 0 {
		t.Errorf("states = %+v/%+v, want emptied", snap.NodeStates, snap.EdgeStates)
	}
	if snap.NodeStates == nil || snap.EdgeStates == nil {
		t.Error("states must stay empty slices, not nil")
	}
	if snap.OrderID != "o1" || snap.OrderUpdateID != 2 || snap.LastNodeID != "n0" {
		t.Errorf("order identity = %s/%d/%s, want kept o1/2/n0", snap.OrderID, snap.OrderUpdateID, snap.LastNodeID)
	}
	for _, a := range snap.ActionStates {
		if a.ActionID == "a1" && a.ActionStatus != state.ActionStatusFailed {
			t.Errorf("a1 status = %q, want FAILED", a.ActionStatus)
		}
		if a.ActionID == "a2" && a.ActionStatus != state.ActionStatusFinished {
			t.Errorf("a2 status = %q, want FINISHED untouched", a.ActionStatus)
		}
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("expected change notification")
	}
	if !s.OrderCancelled() {
		t.Fatal("OrderCancelled = false, want true after cancel")
	}

	s.CommitOrderProjection(OrderProjection{OrderID: "o2", NodeStates: []state.NodeState{}, EdgeStates: []state.EdgeState{}, ActionStates: []state.ActionState{}})
	if s.OrderCancelled() {
		t.Fatal("OrderCancelled = true after a new accepted order, want cleared")
	}
}

func TestSetPaused(t *testing.T) {
	s := New()
	if snap := s.Snapshot(); snap.Paused != nil {
		t.Fatalf("Paused = %v, want nil before any pause interaction", *snap.Paused)
	}
	select {
	case <-s.Changes():
	default:
	}

	s.SetPaused(true)
	if snap := s.Snapshot(); snap.Paused == nil || !*snap.Paused {
		t.Fatal("Paused, want explicit true")
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("expected change notification")
	}

	s.SetPaused(false)
	if snap := s.Snapshot(); snap.Paused == nil || *snap.Paused {
		t.Fatal("Paused, want explicit false after resume")
	}
}

func TestBeginCancellationFailsScheduledActionsAndKeepsRunningState(t *testing.T) {
	s := New()
	s.CommitOrderProjection(OrderProjection{
		OrderID: "o1", NodeStates: []state.NodeState{{NodeID: "n1", SequenceID: 2}},
		ActionStates: []state.ActionState{
			{ActionID: "waiting", ActionStatus: state.ActionStatusWaiting},
			{ActionID: "running", ActionStatus: state.ActionStatusRunning},
		},
	})
	s.BeginOrderCancellation()
	got := s.Snapshot()
	if !s.OrderCancelled() || IsIdle(got) || len(got.NodeStates) != 1 || got.ActionStates[0].ActionStatus != state.ActionStatusFailed || got.ActionStates[1].ActionStatus != state.ActionStatusRunning {
		t.Fatalf("cancellation lost running state or retained scheduled action: %+v", got)
	}
}
