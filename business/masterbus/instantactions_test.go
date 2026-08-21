package masterbus

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestHandleInstantActions_SupportedActionTracked(t *testing.T) {
	b, ft := newTestBus(t)
	var got []order.Action
	b.SetInstantActionHook(func(a order.Action) { got = append(got, a) })

	msg := []byte(`{"headerId":1,"timestamp":"2026-07-17T00:00:00.000Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"ia-1","actionType":"startCharging","blockingType":"NONE"}]}`)
	b.handleInstantActions(msg)

	st := lastState(t, b, ft)
	if len(st.InstantActionStates) != 1 {
		t.Fatalf("InstantActionStates = %+v, want 1 entry", st.InstantActionStates)
	}
	as := st.InstantActionStates[0]
	if as.ActionID != "ia-1" || as.ActionType != "startCharging" || as.ActionStatus != state.ActionStatusWaiting {
		t.Errorf("instant action state = %+v, want ia-1 startCharging WAITING", as)
	}
	if len(got) != 1 || got[0].ActionID != "ia-1" {
		t.Errorf("hook received %+v, want ia-1", got)
	}
	if len(st.Errors) != 0 {
		t.Errorf("Errors = %+v, want none", st.Errors)
	}
}

// initializePosition is a standard instant action forwarded to the executor
// (which translates it to the robot's hot_localize); it must not be rejected.
func TestHandleInstantActions_InitializePositionForwarded(t *testing.T) {
	b, ft := newTestBus(t)
	var got []order.Action
	b.SetInstantActionHook(func(a order.Action) { got = append(got, a) })

	msg := []byte(`{"headerId":1,"timestamp":"2026-07-21T00:00:00.000Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"ip-1","actionType":"initializePosition","blockingType":"NONE","actionParameters":[{"key":"x","value":1.0},{"key":"y","value":2.0},{"key":"theta","value":0.0},{"key":"mapId","value":"map-1"}]}]}`)
	b.handleInstantActions(msg)

	st := lastState(t, b, ft)
	if len(st.InstantActionStates) != 1 {
		t.Fatalf("InstantActionStates = %+v, want 1 entry", st.InstantActionStates)
	}
	as := st.InstantActionStates[0]
	if as.ActionID != "ip-1" || as.ActionType != "initializePosition" || as.ActionStatus != state.ActionStatusWaiting {
		t.Errorf("instant action state = %+v, want ip-1 initializePosition WAITING", as)
	}
	if len(got) != 1 || got[0].ActionID != "ip-1" {
		t.Errorf("hook received %+v, want ip-1 forwarded to the executor", got)
	}
	if _, ok := findError(st, state.ErrorTypeInvalidInstantAction); ok {
		t.Errorf("initializePosition must not be rejected as unsupported; errors = %+v", st.Errors)
	}
}

func TestHandleInstantActions_UnsupportedRejected(t *testing.T) {
	b, ft := newTestBus(t)

	msg := []byte(`{"headerId":2,"timestamp":"2026-07-17T00:00:00.000Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"ia-2","actionType":"flyToMoon","blockingType":"NONE"}]}`)
	b.handleInstantActions(msg)

	st := lastState(t, b, ft)
	if len(st.InstantActionStates) != 1 {
		t.Fatalf("InstantActionStates = %+v, want 1 entry", st.InstantActionStates)
	}
	as := st.InstantActionStates[0]
	if as.ActionID != "ia-2" || as.ActionStatus != state.ActionStatusFailed {
		t.Errorf("instant action state = %+v, want ia-2 FAILED", as)
	}
	e, ok := findError(st, state.ErrorTypeInvalidInstantAction)
	if !ok {
		t.Fatalf("Errors = %+v, want INVALID_INSTANT_ACTION", st.Errors)
	}
	if !hasRef(e, "actionId", "ia-2") {
		t.Errorf("error refs = %+v, want actionId=ia-2", e.ErrorReferences)
	}
}

// one message, one supported + one unsupported: both tracked, hook unset must not panic
func TestHandleInstantActions_MixedBatch(t *testing.T) {
	b, ft := newTestBus(t)

	msg := []byte(`{"headerId":3,"timestamp":"2026-07-17T00:00:00.000Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"ia-3","actionType":"stopCharging","blockingType":"NONE"},{"actionId":"ia-4","actionType":"teleport","blockingType":"NONE"}]}`)
	b.handleInstantActions(msg)

	st := lastState(t, b, ft)
	if len(st.InstantActionStates) != 2 {
		t.Fatalf("InstantActionStates = %+v, want 2 entries", st.InstantActionStates)
	}
	byID := map[string]state.ActionState{}
	for _, as := range st.InstantActionStates {
		byID[as.ActionID] = as
	}
	if byID["ia-3"].ActionStatus != state.ActionStatusWaiting {
		t.Errorf("ia-3 status = %q, want WAITING", byID["ia-3"].ActionStatus)
	}
	if byID["ia-4"].ActionStatus != state.ActionStatusFailed {
		t.Errorf("ia-4 status = %q, want FAILED", byID["ia-4"].ActionStatus)
	}
}

func TestHandleInstantActions_MalformedJSON(t *testing.T) {
	b, ft := newTestBus(t)
	b.handleInstantActions([]byte("{not valid json"))
	st := lastState(t, b, ft)
	if !hasError(st, state.ErrorTypeValidationFailure) {
		t.Errorf("Errors = %+v, want VALIDATION_FAILURE", st.Errors)
	}
}
