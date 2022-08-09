package participation

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pkg/errors"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/core/app"
	"github.com/iotaledger/hive.go/core/app/core/shutdown"
	"github.com/iotaledger/hornet/v2/pkg/database"
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
	ParticipationManager *participation.ParticipationManager
	NodeBridge           *nodebridge.NodeBridge
	ShutdownHandler      *shutdown.ShutdownHandler
}

func provide(c *dig.Container) error {

	type participationDeps struct {
		dig.In
		NodeBridge *nodebridge.NodeBridge
	}

	return c.Provide(func(deps participationDeps) *participation.ParticipationManager {

		dbEngine, err := database.DatabaseEngineFromStringAllowed(ParamsParticipation.Database.Engine)
		if err != nil {
			CoreComponent.LogPanic(err)
		}

		participationStore, err := database.StoreWithDefaultSettings(ParamsParticipation.Database.Path, true, dbEngine)
		if err != nil {
			CoreComponent.LogPanic(err)
		}

		pm, err := participation.NewManager(
			participationStore,
			deps.NodeBridge.ProtocolParameters,
			NodeStatus,
			BlockForBlockID,
			OutputForOutputID,
			LedgerUpdates,
		)
		if err != nil {
			CoreComponent.LogPanic(err)
		}
		CoreComponent.LogInfof("Initialized ParticipationManager at milestone %d", pm.LedgerIndex())
		return pm
	})
}

func configure() error {
	if err := CoreComponent.App.Daemon().BackgroundWorker("Close Participation database", func(ctx context.Context) {
		<-ctx.Done()

		CoreComponent.LogInfo("Syncing Participation database to disk...")
		if err := deps.ParticipationManager.CloseDatabase(); err != nil {
			CoreComponent.LogPanicf("Syncing Participation database to disk... failed: %s", err)
		}
		CoreComponent.LogInfo("Syncing Participation database to disk... done")
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
			startIndex = startIndex + 1
		}

		if err := LedgerUpdates(ctx, startIndex, 0, func(index iotago.MilestoneIndex, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) error {
			timeStart := time.Now()
			if err := deps.ParticipationManager.ApplyNewLedgerUpdate(index, created, consumed); err != nil {
				CoreComponent.LogPanicf("ApplyNewLedgerUpdate failed: %s", err)
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

		e := newEcho()
		setupRoutes(e)
		go func() {
			CoreComponent.LogInfof("You can now access the API using: http://%s", ParamsParticipation.BindAddress)
			if err := e.Start(ParamsParticipation.BindAddress); err != nil && !errors.Is(err, http.ErrServerClosed) {
				CoreComponent.LogPanicf("Stopped REST-API server due to an error (%s)", err)
			}
		}()

		if err := deps.NodeBridge.RegisterAPIRoute(APIRoute, ParamsParticipation.BindAddress); err != nil {
			CoreComponent.LogPanicf("Error registering INX api route (%s)", err)
		}

		<-ctx.Done()
		CoreComponent.LogInfo("Stopping API ...")

		if err := deps.NodeBridge.UnregisterAPIRoute(APIRoute); err != nil {
			CoreComponent.LogWarnf("Error unregistering INX api route (%s)", err)
		}

		shutdownCtx, shutdownCtxCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := e.Shutdown(shutdownCtx); err != nil {
			CoreComponent.LogWarn(err)
		}
		shutdownCtxCancel()
		CoreComponent.LogInfo("Stopping API ... done")
	}, daemon.PriorityStopParticipationAPI); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}

func newEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	return e
}
