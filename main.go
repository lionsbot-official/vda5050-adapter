package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lionsbot-official/vda5050-adapter/app/vdaapp"
	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/executor"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/cleaningcatalog"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus"
	"github.com/lionsbot-official/vda5050-adapter/business/robotbus/infra/lionsbot"
	"github.com/lionsbot-official/vda5050-adapter/foundation/env"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("vda5050 adapter: %s", err)
	}
}

func run() error {
	slog.Info("lionsbot vda5050 adapter starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		slog.Info("signal received, shutting down")
		cancel()
		<-sig
		slog.Warn("forced shutdown")
		os.Exit(1)
	}()

	configs := env.InitConfigs()

	store := vehiclestate.New()

	robotCfg, err := lionsbot.NewConfig(configs.RobotConfig)
	if err != nil {
		return fmt.Errorf("failed to build robot config: %w", err)
	}

	// robot_id is the one identity: it drives both the upstream subscribe and the
	// downstream VDA topics (as serialNumber).
	masterBus, err := masterbus.New(configs, store, robotCfg.RobotID, "lionsbot")
	if err != nil {
		return fmt.Errorf("failed to initialize master bus: %w", err)
	}

	robotClient := lionsbot.New(robotCfg)
	robotBus := robotbus.New(robotClient, store)

	exec := executor.New(store, robotClient)
	masterBus.SetOrderAcceptedHook(exec.OrderAccepted)
	masterBus.SetInstantActionHook(exec.InstantAction)
	// publish the active map's cleaning options (zones + equalizers + docks)
	// to the fleet side whenever the driver resolves them
	robotClient.SetCatalogChangedHook(func(cat cleaningcatalog.Catalog) {
		if err := masterBus.PublishCleaningCatalog(cat); err != nil {
			slog.Error("failed to publish cleaning catalog", "err", err)
		}
	})

	return vdaapp.New(masterBus, robotBus, exec).Start(ctx)
}
