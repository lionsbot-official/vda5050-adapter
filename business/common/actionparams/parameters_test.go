package actionparams

import (
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"testing"
)

func TestRejectInvalidParameters(t *testing.T) {
	for _, a := range []order.Action{
		{ActionType: "startCharging", ActionParameters: []order.ActionParameter{{Key: "stationName", Value: "other"}}},
		{ActionType: "startCleaning"},
		{ActionType: "startCleaning", ActionParameters: []order.ActionParameter{{Key: "zones", Value: []any{map[string]any{"name": "A", "cleaningMode": true}}}}},
		{ActionType: "startCleaning", ActionParameters: []order.ActionParameter{{Key: "zones", Value: []any{map[string]any{"name": "A"}}}, {Key: "zones", Value: []any{map[string]any{"name": "B"}}}}},
		{ActionType: "initializePosition", ActionParameters: []order.ActionParameter{{Key: "x", Value: 0.0}, {Key: "y", Value: 0.0}, {Key: "theta", Value: 4.0}, {Key: "mapId", Value: "m"}, {Key: "lastNodeId", Value: "n0"}}},
	} {
		if err := Validate(a); err == nil {
			t.Errorf("invalid parameters accepted: %+v", a)
		}
	}
}
