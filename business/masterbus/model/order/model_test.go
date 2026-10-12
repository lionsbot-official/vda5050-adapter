package order

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestOrderSchemaConformance(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("../../../../reference/VDA5050-2.0.0/schemas/order.schema")

	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	cases := []struct {
		name      string
		wantValid bool
		raw       []byte
	}{
		{
			name:      "valid",
			wantValid: true,
			raw: []byte(`{
				"headerId": 2,
				"timestamp": "2006-01-02T15:04:05.000Z",
				"version": "2.0.0",
				"manufacturer": "lionsbot",
				"serialNumber": "R3-0001",
				"orderId": "2",
				"orderUpdateId": 1,
				"nodes": [],
				"edges": [],
				"orderDescription": "test order description"}`),
		},
	}

	for _, tc := range cases {
		var doc any
		if err := json.Unmarshal(tc.raw, &doc); err != nil {
			t.Fatalf("unmarshal order: %v", err)
		}
		err = schema.Validate(doc)
		gotValid := err == nil
		if gotValid != tc.wantValid {
			t.Errorf("conformance = %v, want %v\npayload: %s\nvalidate err: %v", gotValid, tc.wantValid, tc.raw, err)
		}
	}
}
