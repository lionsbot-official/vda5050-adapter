package masterbus

import (
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

func TestHandleOrder_PublishedStateCarriesTelemetry(t *testing.T) {
	ft := &fakeTransport{}
	store := vehiclestate.New()
	b, err := NewWithTransport(ft, store, "TESTBOT", "3.0.0", "lionsbot")
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}

	pos := state.MobileRobotPosition{X: 7.5, Y: 8.25, Theta: 0.5, MapID: "map-1", Localized: true}
	driving := true
	mode := state.OperatingModeAutomatic
	store.SetTelemetry(vehiclestate.Telemetry{
		PowerSupply:         &state.PowerSupply{StateOfCharge: 42, Charging: true},
		SafetyState:         &state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone},
		OperatingMode:       &mode,
		Driving:             &driving,
		MobileRobotPosition: &pos,
		Information: []state.Info{{
			InfoType:       "lionsbot.telemetry",
			InfoLevel:      state.InfoLevelInfo,
			InfoReferences: []state.InfoReference{{ReferenceKey: "waterLevel", ReferenceValue: "50"}},
		}},
	})

	o := order.Order{
		OrderID:       "order-1",
		OrderUpdateID: 0,
		Nodes:         []order.Node{{NodeID: "n0", SequenceID: 0, Released: true}},
		Edges:         []order.Edge{},
	}
	b.handleOrder(mustOrderBytes(t, o))

	st := lastState(t, b, ft)
	wantPower := state.PowerSupply{StateOfCharge: 42, Charging: true}
	if st.PowerSupply != wantPower {
		t.Errorf("powerSupply = %+v, want %+v", st.PowerSupply, wantPower)
	}
	if st.MobileRobotPosition == nil {
		t.Fatal("mobileRobotPosition = nil, want present")
	}
	if *st.MobileRobotPosition != pos {
		t.Errorf("mobileRobotPosition = %+v, want %+v", *st.MobileRobotPosition, pos)
	}
	if !st.Driving {
		t.Error("driving = false, want true")
	}
	if st.OperatingMode != state.OperatingModeAutomatic {
		t.Errorf("operating mode = %q, want AUTOMATIC", st.OperatingMode)
	}
	wantSafety := state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false}
	if st.SafetyState != wantSafety {
		t.Errorf("safetyState = %+v, want %+v", st.SafetyState, wantSafety)
	}
	if st.OrderID != "order-1" || st.LastNodeID != "n0" {
		t.Errorf("order fields = %q/%q, want order-1/n0", st.OrderID, st.LastNodeID)
	}
	if len(st.Information) != 1 || st.Information[0].InfoType != "lionsbot.telemetry" {
		t.Fatalf("information = %+v, want one lionsbot.telemetry group", st.Information)
	}
	refs := st.Information[0].InfoReferences
	if len(refs) != 1 || refs[0].ReferenceKey != "waterLevel" || refs[0].ReferenceValue != "50" {
		t.Errorf("infoReferences = %+v, want waterLevel=50", refs)
	}
}
