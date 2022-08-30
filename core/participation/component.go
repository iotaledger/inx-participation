package participation

import (
	"context"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/core/app"
	"github.com/iotaledger/hive.go/core/app/core/shutdown"
	"github.com/iotaledger/hornet/v2/pkg/database"
	"github.com/iotaledger/inx-app/httpserver"
	"github.com/iotaledger/inx-app/nodebridge"
	"github.com/iotaledger/inx-participation/pkg/daemon"
	"github.com/iotaledger/inx-participation/pkg/participation"
	iotago "github.com/iotaledger/iota.go/v3"
)

func init() {
	CoreComponent = &app.CoreComponent{
		Component: &app.Component{
			Name:      "Participation",
			Params:    params,
			DepsFunc:  func(cDeps dependencies) { deps = cDeps },
			Provide:   provide,
			Configure: configure,
			Run:       run,
		},
	}
}

var (
	CoreComponent *app.CoreComponent
	deps          dependencies
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

		dbEngine, err := database.EngineFromStringAllowed(ParamsParticipation.Database.Engine)
		if err != nil {
			CoreComponent.LogErrorAndExit(err)
		}

		participationStore, err := database.StoreWithDefaultSettings(ParamsParticipation.Database.Path, true, dbEngine)
		if err != nil {
			CoreComponent.LogErrorAndExit(err)
		}

		pm, err := participation.NewManager(
			CoreComponent.Daemon().ContextStopped(),
			participationStore,
			deps.NodeBridge.ProtocolParameters,
			NodeStatus,
			BlockForBlockID,
			OutputForOutputID,
			LedgerUpdates,
		)
		if err != nil {
			CoreComponent.LogErrorAndExit(err)
		}
		CoreComponent.LogInfof("Initialized ParticipationManager at milestone %d", pm.LedgerIndex())

		return pm
	})
}

func configure() error {
	if err := CoreComponent.App.Daemon().BackgroundWorker("Close Participation database", func(ctx context.Context) {
		<-ctx.Done()

		CoreComponent.LogInfo("Syncing Participation database to disk ...")
		if err := deps.ParticipationManager.CloseDatabase(); err != nil {
			CoreComponent.LogErrorfAndExit("Syncing Participation database to disk ... failed: %s", err)
		}
		CoreComponent.LogInfo("Syncing Participation database to disk ... done")
	}, daemon.PriorityCloseParticipationDatabase); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}

func run() error {
	// create a background worker that handles the participation events
	if err := CoreComponent.Daemon().BackgroundWorker("LedgerUpdates", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting LedgerUpdates ... done")

		startIndex := deps.ParticipationManager.LedgerIndex()
		if startIndex > 0 {
			startIndex++
		}

		if err := LedgerUpdates(ctx, startIndex, 0, func(index iotago.MilestoneIndex, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) error {
			timeStart := time.Now()
			if err := deps.ParticipationManager.ApplyNewLedgerUpdate(index, created, consumed); err != nil {
				CoreComponent.LogErrorfAndExit("ApplyNewLedgerUpdate failed: %s", err)

				return err
			}
			CoreComponent.LogInfof("Applying milestone %d with %d new and %d outputs took %s", index, len(created), len(consumed), time.Since(timeStart).Truncate(time.Millisecond))

			return nil
		}); err != nil {
			CoreComponent.LogWarnf("Listening to LedgerUpdates failed: %s", err)
			deps.ShutdownHandler.SelfShutdown("disconnected from INX", false)
		}

		CoreComponent.LogInfo("Stopping LedgerUpdates ... done")
	}, daemon.PriorityStopParticipation); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	// create a background worker that handles the API
	if err := CoreComponent.Daemon().BackgroundWorker("API", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting API ... done")

		CoreComponent.LogInfo("Starting API server ...")

		e := httpserver.NewEcho(CoreComponent.Logger(), nil, ParamsRestAPI.DebugRequestLoggerEnabled)

		setupRoutes(e)
		go func() {
			CoreComponent.LogInfof("You can now access the API using: http://%s", ParamsRestAPI.BindAddress)
			if err := e.Start(ParamsRestAPI.BindAddress); err != nil && !errors.Is(err, http.ErrServerClosed) {
				CoreComponent.LogErrorfAndExit("Stopped REST-API server due to an error (%s)", err)
			}
		}()

		ctxRegister, cancelRegister := context.WithTimeout(ctx, 5*time.Second)

		if err := deps.NodeBridge.RegisterAPIRoute(ctxRegister, APIRoute, ParamsRestAPI.BindAddress); err != nil {
			CoreComponent.LogErrorfAndExit("Registering INX api route failed: %s", err)
		}

		cancelRegister()

		CoreComponent.LogInfo("Starting API server ... done")
		<-ctx.Done()
		CoreComponent.LogInfo("Stopping API ...")

		ctxUnregister, cancelUnregister := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUnregister()

		//nolint:contextcheck // false positive
		if err := deps.NodeBridge.UnregisterAPIRoute(ctxUnregister, APIRoute); err != nil {
			CoreComponent.LogWarnf("Unregistering INX api route failed: %s", err)
		}

		shutdownCtx, shutdownCtxCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCtxCancel()

		//nolint:contextcheck // false positive
		if err := e.Shutdown(shutdownCtx); err != nil {
			CoreComponent.LogWarn(err)
		}

		CoreComponent.LogInfo("Stopping API ... done")
	}, daemon.PriorityStopParticipationAPI); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}
