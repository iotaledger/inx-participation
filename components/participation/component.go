package participation

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/app/shutdown"
	hivedb "github.com/iotaledger/hive.go/kvstore/database"
	hornetdb "github.com/iotaledger/hornet/v2/pkg/database"
	"github.com/iotaledger/inx-app/pkg/httpserver"
	"github.com/iotaledger/inx-app/pkg/nodebridge"
	"github.com/iotaledger/inx-participation/pkg/daemon"
	"github.com/iotaledger/inx-participation/pkg/participation"
	iotago "github.com/iotaledger/iota.go/v3"
)

var (
	AllowedEnginesStorage = []hivedb.Engine{
		hivedb.EnginePebble,
		hivedb.EngineRocksDB,
	}

	AllowedEnginesStorageAuto = append(AllowedEnginesStorage, hivedb.EngineAuto)
)

func init() {
	Component = &app.Component{
		Name:      "Participation",
		Params:    params,
		DepsFunc:  func(cDeps dependencies) { deps = cDeps },
		Provide:   provide,
		Configure: configure,
		Run:       run,
	}
}

var (
	Component *app.Component
	deps      dependencies
)

type dependencies struct {
	dig.In
	ParticipationManager *participation.Manager
	NodeBridge           *nodebridge.NodeBridge
	ShutdownHandler      *shutdown.ShutdownHandler
}

func provide(c *dig.Container) error {

	type participationDeps struct {
		dig.In
		NodeBridge *nodebridge.NodeBridge
	}

	return c.Provide(func(deps participationDeps) *participation.Manager {

		dbEngine, err := hivedb.EngineFromStringAllowed(ParamsParticipation.Database.Engine, AllowedEnginesStorageAuto)
		if err != nil {
			Component.LogErrorAndExit(err)
		}

		participationStore, err := hornetdb.StoreWithDefaultSettings(ParamsParticipation.Database.Path, true, dbEngine)
		if err != nil {
			Component.LogErrorAndExit(err)
		}

		pm, err := participation.NewManager(
			Component.Daemon().ContextStopped(),
			participationStore,
			deps.NodeBridge.ProtocolParameters,
			NodeStatus,
			BlockForBlockID,
			OutputForOutputID,
			LedgerUpdates,
		)
		if err != nil {
			Component.LogErrorAndExit(err)
		}
		Component.LogInfof("Initialized ParticipationManager at milestone %d", pm.LedgerIndex())

		return pm
	})
}

func configure() error {
	if err := Component.App().Daemon().BackgroundWorker("Close Participation database", func(ctx context.Context) {
		<-ctx.Done()

		Component.LogInfo("Syncing Participation database to disk ...")
		if err := deps.ParticipationManager.CloseDatabase(); err != nil {
			Component.LogErrorfAndExit("Syncing Participation database to disk ... failed: %s", err)
		}
		Component.LogInfo("Syncing Participation database to disk ... done")
	}, daemon.PriorityCloseParticipationDatabase); err != nil {
		Component.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}

func run() error {
	// create a background worker that handles the participation events
	if err := Component.Daemon().BackgroundWorker("LedgerUpdates", func(ctx context.Context) {
		Component.LogInfo("Starting LedgerUpdates ... done")

		startIndex := deps.ParticipationManager.LedgerIndex()
		if startIndex > 0 {
			startIndex++
		}

		if err := LedgerUpdates(ctx, startIndex, 0, func(index iotago.MilestoneIndex, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) error {
			timeStart := time.Now()
			if err := deps.ParticipationManager.ApplyNewLedgerUpdate(index, created, consumed); err != nil {
				Component.LogErrorfAndExit("ApplyNewLedgerUpdate failed: %s", err)

				return err
			}
			Component.LogInfof("Applying milestone %d with %d new and %d outputs took %s", index, len(created), len(consumed), time.Since(timeStart).Truncate(time.Millisecond))

			return nil
		}); err != nil {
			Component.LogWarnf("Listening to LedgerUpdates failed: %s", err)
			deps.ShutdownHandler.SelfShutdown("disconnected from INX", false)
		}

		Component.LogInfo("Stopping LedgerUpdates ... done")
	}, daemon.PriorityStopParticipation); err != nil {
		Component.LogPanicf("failed to start worker: %s", err)
	}

	// create a background worker that handles the API
	if err := Component.Daemon().BackgroundWorker("API", func(ctx context.Context) {
		Component.LogInfo("Starting API ... done")

		Component.LogInfo("Starting API server ...")

		e := httpserver.NewEcho(Component.Logger(), nil, ParamsRestAPI.DebugRequestLoggerEnabled)

		setupRoutes(e.Group(APIRoute))

		go func() {
			Component.LogInfof("You can now access the API using: http://%s", ParamsRestAPI.BindAddress)
			if err := e.Start(ParamsRestAPI.BindAddress); err != nil && !errors.Is(err, http.ErrServerClosed) {
				Component.LogErrorfAndExit("Stopped REST-API server due to an error (%s)", err)
			}
		}()

		ctxRegister, cancelRegister := context.WithTimeout(ctx, 5*time.Second)

		advertisedAddress := ParamsRestAPI.BindAddress
		if ParamsRestAPI.AdvertiseAddress != "" {
			advertisedAddress = ParamsRestAPI.AdvertiseAddress
		}

		routeName := strings.Replace(APIRoute, "/api/", "", 1)

		if err := deps.NodeBridge.RegisterAPIRoute(ctxRegister, routeName, advertisedAddress, APIRoute); err != nil {
			Component.LogErrorfAndExit("Registering INX api route failed: %s", err)
		}

		cancelRegister()

		Component.LogInfo("Starting API server ... done")
		<-ctx.Done()
		Component.LogInfo("Stopping API ...")

		ctxUnregister, cancelUnregister := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUnregister()

		//nolint:contextcheck // false positive
		if err := deps.NodeBridge.UnregisterAPIRoute(ctxUnregister, routeName); err != nil {
			Component.LogWarnf("Unregistering INX api route failed: %s", err)
		}

		shutdownCtx, shutdownCtxCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCtxCancel()

		//nolint:contextcheck // false positive
		if err := e.Shutdown(shutdownCtx); err != nil {
			Component.LogWarn(err)
		}

		Component.LogInfo("Stopping API ... done")
	}, daemon.PriorityStopParticipationAPI); err != nil {
		Component.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}
