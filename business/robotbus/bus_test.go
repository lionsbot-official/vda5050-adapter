package robotbus

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

type fakeRobotClient struct {
	snapshots chan vehiclestate.Telemetry
}

func (f *fakeRobotClient) Connect() error { return nil }
func (f *fakeRobotClient) Disconnect()    {}
func (f *fakeRobotClient) Telemetry() (<-chan vehiclestate.Telemetry, error) {
	return f.snapshots, nil
}

func newTestBus(t *testing.T) (*Bus, *fakeRobotClient, *vehiclestate.Store) {
	t.Helper()
	fc := &fakeRobotClient{snapshots: make(chan vehiclestate.Telemetry)}
	store := vehiclestate.New()
	return New(fc, store), fc, store
}

func TestStart_CommitsTelemetryToStore(t *testing.T) {
	driving := true
	mode := state.OperatingModeManual
	full := vehiclestate.Telemetry{
		BatteryState:  &state.BatteryState{BatteryCharge: 42.5, Charging: true},
		SafetyState:   &state.SafetyState{EStop: state.EStopManual, FieldViolation: true},
		OperatingMode: &mode,
		Driving:       &driving,
		AGVPosition:   &state.AGVPosition{X: 1.5, Y: -2.25, Theta: 0.5, MapID: "map-1", PositionInitialized: true},
		Velocity:      &state.Velocity{Vx: 0.8, Omega: 0.1},
	}

	cases := []struct {
		name        string
		snapshots   []vehiclestate.Telemetry
		wantPower   state.BatteryState
		wantDriving bool
		wantPos     *state.AGVPosition
		wantVel     *state.Velocity
	}{
		{
			name:        "full snapshot flows through to the store",
			snapshots:   []vehiclestate.Telemetry{full},
			wantPower:   state.BatteryState{BatteryCharge: 42.5, Charging: true},
			wantDriving: true,
			wantPos:     &state.AGVPosition{X: 1.5, Y: -2.25, Theta: 0.5, MapID: "map-1", PositionInitialized: true},
			wantVel:     &state.Velocity{Vx: 0.8, Omega: 0.1},
		},
		{
			name:        "empty second snapshot retains required and omits optional",
			snapshots:   []vehiclestate.Telemetry{full, {}},
			wantPower:   state.BatteryState{BatteryCharge: 42.5, Charging: true},
			wantDriving: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, fc, store := newTestBus(t)

			done := make(chan struct{})
			go func() {
				b.Start(context.Background())
				close(done)
			}()

			for _, snap := range tc.snapshots {
				fc.snapshots <- snap
			}
			close(fc.snapshots)

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Start did not return after telemetry stream closed")
			}

			got := store.Snapshot()
			if got.BatteryState != tc.wantPower {
				t.Errorf("batteryState = %+v, want %+v", got.BatteryState, tc.wantPower)
			}
			if got.Driving != tc.wantDriving {
				t.Errorf("driving = %v, want %v", got.Driving, tc.wantDriving)
			}
			if !reflect.DeepEqual(got.AGVPosition, tc.wantPos) {
				t.Errorf("agvPosition = %+v, want %+v", got.AGVPosition, tc.wantPos)
			}
			if !reflect.DeepEqual(got.Velocity, tc.wantVel) {
				t.Errorf("velocity = %+v, want %+v", got.Velocity, tc.wantVel)
			}
		})
	}
}

func TestStart_StopsOnContextCancel(t *testing.T) {
	b, _, _ := newTestBus(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start did not return on context cancellation")
	}
}
