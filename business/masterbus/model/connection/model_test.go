package connection

import (
	"encoding/json"
	"testing"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestConnectionSchemaConformance(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("../../../../reference/VDA5050-2.1.0/schemas/connection.schema")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	cases := []struct {
		name      string
		conn      Connection
		wantValid bool
	}{
		{
			name: "valid ConnectionState",
			conn: Connection{
				Header: common.Header{
					HeaderID:     1,
					Timestamp:    "2026-06-29T19:18:00.123Z",
					Version:      "2.1.0",
					Manufacturer: "lionsbot",
					SerialNumber: "R3-0001",
				},
				ConnectionState: ConnectionStateOnline,
			},
			wantValid: true,
		},
		{
			name: "invalid ConnectionState",
			conn: Connection{
				Header: common.Header{
					HeaderID:     1,
					Timestamp:    "2026-06-29T19:18:00.123Z",
					Version:      "2.1.0",
					Manufacturer: "lionsbot",
					SerialNumber: "R3-0001",
				},
				ConnectionState: "meow",
			},
			wantValid: false,
		},
	}

	for _, tc := range cases {
		raw, err := json.Marshal(tc.conn)
		if err != nil {
			t.Fatalf("marshal connection: %v", err)
		}

		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal connection: %v", err)
		}

		err = schema.Validate(doc)
		gotValid := err == nil
		if gotValid != tc.wantValid {
			t.Errorf("conformance = %v, want %v\npayload: %s\nvalidate err: %v",
				gotValid, tc.wantValid, raw, err)
		}
	}
}
