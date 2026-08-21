package masterbus

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/factsheet"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestPublishOnline_EmitsRetainedFactsheet(t *testing.T) {
	b, ft := newTestBus(t)

	b.publishOnline()

	topic := "vda5050/v3/lionsbot/TESTBOT/factsheet"
	pub, ok := lastPublishOn(ft, topic)
	if !ok {
		t.Fatalf("no publish on %s; published topics = %v", topic, publishedTopics(ft))
	}
	if !pub.retained {
		t.Error("factsheet must be retained so a late-joining master is not blind to the vehicle")
	}
	if pub.qos != 0 {
		t.Errorf("factsheet qos = %d, want 0 (VDA5050_EN.md:189)", pub.qos)
	}

	fs, ok := pub.payload.(factsheet.Factsheet)
	if !ok {
		t.Fatalf("factsheet payload type = %T, want factsheet.Factsheet", pub.payload)
	}

	// The published document must validate against the real schema.
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("../../reference/VDA5050-3.0.0/schemas/factsheet.schema")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	raw, err := json.Marshal(fs)
	if err != nil {
		t.Fatalf("marshal factsheet: %v", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal factsheet: %v", err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("published factsheet not schema-valid: %v\npayload: %s", err, raw)
	}

	// Header comes from the bus config, like connection/state.
	if fs.SerialNumber != "TESTBOT" || fs.Version != "3.0.0" || fs.Manufacturer != "lionsbot" {
		t.Errorf("header = %+v, want TESTBOT/3.0.0/lionsbot", fs.Header)
	}
	if fs.HeaderID == 0 {
		t.Error("factsheet headerId must come from the per-topic counter (>=1)")
	}

	// Behavior a master keys off: the advertised action vocabulary.
	wantActions := map[string]bool{
		"startCleaning": true, "startCharging": true, "stopCharging": true,
		"cancelOrder": true, "startPause": true, "stopPause": true, "factsheetRequest": true,
		"initializePosition": true,
	}
	got := map[string]factsheet.MobileRobotAction{}
	for _, a := range fs.ProtocolFeatures.MobileRobotActions {
		got[a.ActionType] = a
	}
	for name := range wantActions {
		if _, ok := got[name]; !ok {
			t.Errorf("mobileRobotActions missing %q; got %v", name, keysOf(got))
		}
	}
	if len(got) != len(wantActions) {
		t.Errorf("mobileRobotActions = %v, want exactly %d actions", keysOf(got), len(wantActions))
	}

	// startCleaning declares its parameters and HARD-blocking, pausable, cancellable.
	sc := got["startCleaning"]
	if !sc.PauseAllowed || !sc.CancelAllowed {
		t.Errorf("startCleaning pause/cancel = %v/%v, want true/true", sc.PauseAllowed, sc.CancelAllowed)
	}
	params := map[string]factsheet.DataType{}
	for _, p := range sc.ActionParameters {
		params[p.Key] = p.ValueDataType
	}
	if params["zones"] != factsheet.DataTypeArray || params["bypassBlackout"] != factsheet.DataTypeBool {
		t.Errorf("startCleaning params = %v, want zones:ARRAY + bypassBlackout:BOOL", params)
	}

	// startCharging/stopCharging are dockable as node actions: advertised at
	// INSTANT + NODE; startCharging blocks HARD and is pausable + cancellable.
	dock := got["startCharging"]
	if !hasScope(dock, factsheet.ScopeInstant) || !hasScope(dock, factsheet.ScopeNode) {
		t.Errorf("startCharging scopes = %v, want INSTANT+NODE", dock.ActionScopes)
	}
	if len(dock.BlockingTypes) == 0 || dock.BlockingTypes[0] != factsheet.BlockingHard {
		t.Errorf("startCharging blocking = %v, want HARD", dock.BlockingTypes)
	}
	if !dock.PauseAllowed {
		t.Error("startCharging pauseAllowed = false, want true (dock_pause/dock_continue)")
	}
	if !dock.CancelAllowed {
		t.Error("startCharging cancelAllowed = false, want true (dock_stop)")
	}
	undock := got["stopCharging"]
	if !hasScope(undock, factsheet.ScopeInstant) || !hasScope(undock, factsheet.ScopeNode) {
		t.Errorf("stopCharging scopes = %v, want INSTANT+NODE", undock.ActionScopes)
	}
	// undock stays uncontrollable once started (no undock pause/stop on the robot)
	if undock.PauseAllowed || undock.CancelAllowed {
		t.Errorf("stopCharging pause/cancel = %v/%v, want false/false (no undock control)", undock.PauseAllowed, undock.CancelAllowed)
	}

	if fs.TypeSpecification.SeriesName != "R3" {
		t.Errorf("seriesName = %q, want R3", fs.TypeSpecification.SeriesName)
	}
}

func hasScope(a factsheet.MobileRobotAction, want factsheet.ActionScope) bool {
	return slices.Contains(a.ActionScopes, want)
}

func keysOf(m map[string]factsheet.MobileRobotAction) []string {
	return slices.Collect(maps.Keys(m))
}

// factsheetRequest is a pure adapter-side data request: served entirely in
// masterbus (publish + FINISHED), never forwarded to the executor.
func TestHandleInstantActions_FactsheetRequestServedInBus(t *testing.T) {
	b, ft := newTestBus(t)
	hookCalled := false
	b.SetInstantActionHook(func(order.Action) { hookCalled = true })

	msg := []byte(`{"headerId":1,"timestamp":"2026-07-20T00:00:00.000Z","version":"3.0.0","manufacturer":"lionsbot","serialNumber":"TESTBOT","actions":[{"actionId":"fs-1","actionType":"factsheetRequest","blockingType":"NONE"}]}`)
	b.handleInstantActions(msg)

	topic := "vda5050/v3/lionsbot/TESTBOT/factsheet"
	pub, ok := lastPublishOn(ft, topic)
	if !ok {
		t.Fatalf("factsheetRequest must (re)publish a factsheet on %s; topics = %v", topic, publishedTopics(ft))
	}
	if !pub.retained || pub.qos != 0 {
		t.Errorf("factsheet publish retained/qos = %v/%d, want true/0", pub.retained, pub.qos)
	}
	if _, ok := pub.payload.(factsheet.Factsheet); !ok {
		t.Errorf("factsheet payload type = %T, want factsheet.Factsheet", pub.payload)
	}

	st := lastState(t, b, ft)
	if len(st.InstantActionStates) != 1 {
		t.Fatalf("InstantActionStates = %+v, want 1 entry", st.InstantActionStates)
	}
	as := st.InstantActionStates[0]
	if as.ActionID != "fs-1" || as.ActionType != "factsheetRequest" || as.ActionStatus != state.ActionStatusFinished {
		t.Errorf("instant action state = %+v, want fs-1 factsheetRequest FINISHED", as)
	}

	if hookCalled {
		t.Error("factsheetRequest must NOT reach the instant-action hook (executor untouched)")
	}
	if _, ok := findError(st, state.ErrorTypeInvalidInstantAction); ok {
		t.Errorf("factsheetRequest must not be rejected as unsupported; errors = %+v", st.Errors)
	}
}

// The advertised action vocabulary must not drift from the intake guard's
// source of truth: same set, and scopes consistent with the registry.
func TestMobileRobotActionsMatchRegistry(t *testing.T) {
	advertised := map[string]factsheet.MobileRobotAction{}
	for _, a := range mobileRobotActions() {
		advertised[a.ActionType] = a
	}

	for name := range supportedActions {
		if _, ok := advertised[name]; !ok {
			t.Errorf("registry action %q is not advertised in the factsheet", name)
		}
	}
	for name := range advertised {
		if _, ok := supportedActions[name]; !ok {
			t.Errorf("advertised action %q is absent from the supported-action registry", name)
		}
	}

	for name, a := range advertised {
		scope := supportedActions[name]
		if scope.instant && !hasScope(a, factsheet.ScopeInstant) {
			t.Errorf("%q is instant in the registry but not scoped INSTANT in the factsheet", name)
		}
		if scope.order && !(hasScope(a, factsheet.ScopeNode) || hasScope(a, factsheet.ScopeEdge)) {
			t.Errorf("%q is an order action in the registry but not scoped NODE/EDGE in the factsheet", name)
		}
	}
}
