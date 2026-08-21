package factsheet

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// validR3 is a representative, schema-shaped factsheet. Tests here assert
// structure/invariants, not the datasheet numbers (those are data, asserted at
// the publish seam).
func validR3() Factsheet {
	return Factsheet{
		Header: common.Header{
			HeaderID:     1,
			Timestamp:    "2026-07-20T10:00:00.000Z",
			Version:      "3.0.0",
			Manufacturer: "lionsbot",
			SerialNumber: "TESTBOT",
		},
		TypeSpecification: TypeSpecification{
			SeriesName:            "R3",
			MobileRobotKinematics: KinematicsDifferential,
			MobileRobotClass:      ClassCleaning,
			MaximumLoadMass:       0,
			LocalizationTypes:     []LocalizationType{LocalizationNatural},
			NavigationTypes:       []NavigationType{NavigationFreelyNavigating},
			SupportedZones:        []ZoneType{},
		},
		PhysicalParameters: PhysicalParameters{
			MinimumSpeed:        0.1,
			MaximumSpeed:        1.0,
			MaximumAcceleration: 0.8,
			MaximumDeceleration: 0.8,
			MinimumHeight:       0.81,
			MaximumHeight:       0.81,
			Width:               0.45,
			Length:              0.60,
		},
		ProtocolLimits: ProtocolLimits{
			Timing: Timing{
				MinimumOrderInterval: 0,
				MinimumStateInterval: 0,
				DefaultStateInterval: 1.0,
			},
		},
		ProtocolFeatures: ProtocolFeatures{
			OptionalParameters: []OptionalParameter{},
			MobileRobotActions: []MobileRobotAction{
				{
					ActionType:    "startCleaning",
					ActionScopes:  []ActionScope{ScopeNode},
					PauseAllowed:  true,
					CancelAllowed: true,
					BlockingTypes: []BlockingType{BlockingHard},
					ActionParameters: []ActionParameter{
						{Key: "zones", ValueDataType: DataTypeArray},
						{Key: "bypassBlackout", ValueDataType: DataTypeBool},
					},
				},
				{ActionType: "factsheetRequest", ActionScopes: []ActionScope{ScopeInstant}, PauseAllowed: false, CancelAllowed: false},
			},
		},
	}
}

func TestFactsheetSchemaConformance(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("../../../../reference/VDA5050-3.0.0/schemas/factsheet.schema")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	cases := []struct {
		name      string
		mutate    func(*Factsheet)
		wantValid bool
	}{
		{name: "pinned R3 factsheet", mutate: func(*Factsheet) {}, wantValid: true},
		{
			name:      "invalid action scope",
			mutate:    func(f *Factsheet) { f.ProtocolFeatures.MobileRobotActions[0].ActionScopes = []ActionScope{"FLYING"} },
			wantValid: false,
		},
		{
			name:      "invalid supported zone",
			mutate:    func(f *Factsheet) { f.TypeSpecification.SupportedZones = []ZoneType{"NONSENSE"} },
			wantValid: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := validR3()
			tc.mutate(&fs)

			raw, err := json.Marshal(fs)
			if err != nil {
				t.Fatalf("marshal factsheet: %v", err)
			}

			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("unmarshal factsheet: %v", err)
			}

			err = schema.Validate(doc)
			gotValid := err == nil
			if gotValid != tc.wantValid {
				t.Errorf("conformance = %v, want %v\npayload: %s\nvalidate err: %v",
					gotValid, tc.wantValid, raw, err)
			}
		})
	}
}

// TestFactsheetMarshalInvariants locks the JSON shape a fleet master keys off:
// required arrays as [] (never null) and the required-but-content-empty objects
// as {}, with the optional configuration block omitted.
func TestFactsheetMarshalInvariants(t *testing.T) {
	raw, err := json.Marshal(validR3())
	if err != nil {
		t.Fatalf("marshal factsheet: %v", err)
	}
	got := string(raw)

	mustContain := []string{
		`"supportedZones":[]`,
		`"optionalParameters":[]`,
		`"maximumStringLengths":{}`,
		`"maximumArrayLengths":{}`,
		`"mobileRobotGeometry":{}`,
		`"loadSpecification":{}`,
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("factsheet JSON missing %s\npayload: %s", s, got)
		}
	}

	if strings.Contains(got, "mobileRobotConfiguration") {
		t.Errorf("mobileRobotConfiguration must be omitted (optional, deferred)\npayload: %s", got)
	}
}
