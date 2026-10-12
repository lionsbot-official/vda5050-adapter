package robotbus

import (
	"context"
	"fmt"

	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
)

type RobotClient interface {
	Connect() error
	Disconnect()
	Telemetry() (<-chan vehiclestate.Telemetry, error)
}

// RobotCommander sends movement and charging commands to the robot in VDA
// terms (world-frame metres and radians) and streams the outcomes back.
// Implementations translate to vendor frames.
type RobotCommander interface {
	MoveTo(x, y, theta float64) error
	StopMovement() error
	PauseMovement() error
	ResumeMovement() error
	// StartCharging docks (dock_start); StopCharging undocks (undock_start) — the
	// inverse operation, NOT an abort of an in-flight dock.
	StartCharging() error
	StopCharging() error
	// PauseDocking / ResumeDocking / AbortDocking control an in-flight dock
	// (dock_pause / dock_continue / dock_stop). AbortDocking is distinct from
	// StopCharging: aborting the approach is not the same as undocking. Undock
	// has no pause/resume/abort equivalent.
	PauseDocking() error
	ResumeDocking() error
	AbortDocking() error
	StartCleaning(zones []ZoneRef, bypassBlackout bool) error
	StopCleaning() error
	PauseCleaning() error
	ResumeCleaning(bypassBlackout bool) error
	// Relocalize resets the robot's pose on its current map (world-frame metres
	// and radians); the driver converts to the vendor frame and emits hot_localize.
	Relocalize(x, y, theta float64) error
	Events() (<-chan ExecutionEvent, error)
}

// ExecutionEvent reports the outcome of a commanded operation; exactly one
// field is set.
type ExecutionEvent struct {
	MoveCompleted          *MoveCompleted
	StartChargingCompleted *ChargingCompleted
	StopChargingCompleted  *ChargingCompleted
	CleanCompleted         *CleanCompleted
	RelocalizeCompleted    *RelocalizeCompleted
}

// ZoneRef names a cleaning zone on the robot's current map. CleaningMode and
// Configs are optional master overrides; when empty the driver resolves the
// zone's default from the equalizer catalog, or lets the robot load its own
// default if no catalog is available.
type ZoneRef struct {
	Name         string
	CleaningMode string             // optional override; "" = catalog/robot default
	Configs      map[string]float64 // optional override; nil = catalog/robot default
}

type MoveCompleted struct {
	Success bool
	Reason  string
}

type ChargingCompleted struct {
	Success bool
	Reason  string
}

type CleanCompleted struct {
	Success bool
	Reason  string
}

// RelocalizeCompleted reports a hot_localize outcome; Score is the localization
// score the robot achieved (surfaced in the reason on failure).
type RelocalizeCompleted struct {
	Success bool
	Score   float64
}

type Bus struct {
	client RobotClient
	store  *vehiclestate.Store
}

func New(client RobotClient, store *vehiclestate.Store) *Bus {
	return &Bus{client: client, store: store}
}

func (b *Bus) Start(ctx context.Context) error {
	if err := b.client.Connect(); err != nil {
		return fmt.Errorf("robotbus: failed to connect to robot client: %w", err)
	}
	defer b.client.Disconnect()

	snapshots, err := b.client.Telemetry()
	if err != nil {
		return fmt.Errorf("robotbus: failed to open telemetry stream: %w", err)
	}

	b.StartTelemetryConsumer(ctx, snapshots)
	return nil
}

func (b *Bus) StartTelemetryConsumer(ctx context.Context, snapshots <-chan vehiclestate.Telemetry) {
	for {
		select {
		case t, ok := <-snapshots:
			if !ok {
				return
			}
			b.store.SetTelemetry(t)
		case <-ctx.Done():
			return
		}
	}
}
