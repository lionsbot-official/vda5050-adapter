package masterbus

import (
	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"testing"
)

func TestStartNodeInRangeZeroUsesDefault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		offset    float64
		tolerance *float64
		want      bool
	}{
		{"zero uses normal tolerance", 0.1, new(0.0), true},
		{"zero accepts exact position", 0, new(0.0), true},
		{"zero bounds normal tolerance", 0.6, new(0.0), false},
		{"omitted uses default", 0.1, nil, true},
		{"omitted still bounds distance", 0.6, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := vehiclestate.New()
			store.SetTelemetry(vehiclestate.Telemetry{AGVPosition: &state.AGVPosition{X: tc.offset, Y: 0, MapID: "map-1", PositionInitialized: true}})
			subject := newRobotState(store)
			node := order.Node{NodePosition: &order.NodePosition{X: 0, Y: 0, MapID: "map-1", AllowedDeviationXY: tc.tolerance}}
			if got := subject.startNodeInRange(node); got != tc.want {
				t.Fatalf("in range=%v, want %v", got, tc.want)
			}
		})
	}
}
