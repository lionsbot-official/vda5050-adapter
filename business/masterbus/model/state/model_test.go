package state

import (
	"encoding/json"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func ptr[T any](v T) *T { return &v }

func TestStateSchemaConformance(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("../../../../reference/VDA5050-3.0.0/schemas/state.schema")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	cases := []struct {
		name      string
		st        State
		wantValid bool
	}{
		{
			name:      "valid",
			wantValid: true,
			st: State{
				Header: common.Header{
					HeaderID:     1,
					Timestamp:    "2026-06-29T19:18:00.123Z",
					Version:      "3.0.0",
					Manufacturer: "lionsbot",
					SerialNumber: "R3-0001",
				},
				OrderID:             "order-1",
				OrderUpdateID:       0,
				LastNodeID:          "n1",
				LastNodeSequenceID:  0,
				NodeStates:          []NodeState{}, //idle
				EdgeStates:          []EdgeState{}, //idle
				Driving:             false,
				ActionStates:        []ActionState{},
				InstantActionStates: []ActionState{},
				PowerSupply: PowerSupply{
					StateOfCharge:  99,
					Charging:       false,
					BatteryVoltage: ptr(24.0),
					BatteryCurrent: ptr(4.0),
					BatteryHealth:  ptr(99.0),
					Range:          ptr(100000.0),
				},
				OperatingMode: OperatingModeAutomatic,
				Errors:        []Error{},
				SafetyState: SafetyState{
					ActiveEmergencyStop: ActiveEmergencyStopNone,
					FieldViolation:      false,
				},
				Paused: ptr(false),
			},
		},
		{
			name:      "invalid OperatingMode",
			wantValid: false,
			st: State{
				Header: common.Header{
					HeaderID:     1,
					Timestamp:    "2026-06-29T19:18:00.123Z",
					Version:      "3.0.0",
					Manufacturer: "lionsbot",
					SerialNumber: "R3-0001",
				},
				OrderID:             "order-1",
				OrderUpdateID:       0,
				LastNodeID:          "n1",
				LastNodeSequenceID:  0,
				NodeStates:          []NodeState{}, //idle
				EdgeStates:          []EdgeState{}, //idle
				Driving:             false,
				ActionStates:        []ActionState{},
				InstantActionStates: []ActionState{},
				PowerSupply: PowerSupply{
					StateOfCharge:  99,
					Charging:       false,
					BatteryVoltage: ptr(24.0),
					BatteryCurrent: ptr(4.0),
					BatteryHealth:  ptr(99.0),
					Range:          ptr(100000.0),
				},
				OperatingMode: "meow",
				Errors:        []Error{},
				SafetyState: SafetyState{
					ActiveEmergencyStop: ActiveEmergencyStopNone,
					FieldViolation:      false,
				},
				Paused: ptr(false),
			},
		},
		{
			name:      "invalid ActiveEmergencyStop",
			wantValid: false,
			st: State{
				Header: common.Header{
					HeaderID:     1,
					Timestamp:    "2026-06-29T19:18:00.123Z",
					Version:      "3.0.0",
					Manufacturer: "lionsbot",
					SerialNumber: "R3-0001",
				},
				OrderID:             "order-1",
				OrderUpdateID:       0,
				LastNodeID:          "n1",
				LastNodeSequenceID:  0,
				NodeStates:          []NodeState{}, //idle
				EdgeStates:          []EdgeState{}, //idle
				Driving:             false,
				ActionStates:        []ActionState{},
				InstantActionStates: []ActionState{},
				PowerSupply: PowerSupply{
					StateOfCharge:  99,
					Charging:       false,
					BatteryVoltage: ptr(24.0),
					BatteryCurrent: ptr(4.0),
					BatteryHealth:  ptr(99.0),
					Range:          ptr(100000.0),
				},
				OperatingMode: OperatingModeAutomatic,
				Errors:        []Error{},
				SafetyState: SafetyState{
					ActiveEmergencyStop: "meow",
					FieldViolation:      false,
				},
				Paused: ptr(false),
			},
		},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(tc.st)
		if err != nil {
			t.Fatalf("marshal state: %v", err)
		}

		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal state: %v", err)
		}
		err = schema.Validate(doc)
		gotValid := err == nil
		if gotValid != tc.wantValid {
			t.Errorf("conformance = %v, want %v\npayload: %s\nvalidate err: %v",
				gotValid, tc.wantValid, raw, err)
		}
	}
}
