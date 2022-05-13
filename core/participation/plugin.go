package participation

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pkg/errors"
	"go.uber.org/dig"

	"github.com/gohornet/hornet/pkg/database"
	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/gohornet/hornet/pkg/shutdown"
	"github.com/gohornet/inx-participation/pkg/daemon"
	"github.com/gohornet/inx-participation/pkg/nodebridge"
	"github.com/gohornet/inx-participation/pkg/participation"
	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/configuration"
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
	AppConfig            *configuration.Configuration `name:"appConfig"`
	ParticipationManager *participation.ParticipationManager
	NodeBridge           *nodebridge.NodeBridge
	ShutdownHandler      *shutdown.ShutdownHandler
}

func provide(c *dig.Container) error {

	type participationDeps struct {
		dig.In
		AppConfig  *configuration.Configuration `name:"appConfig"`
		NodeBridge *nodebridge.NodeBridge
	}

	return c.Provide(func(deps participationDeps) *participation.ParticipationManager {

		participationStore, err := database.StoreWithDefaultSettings(deps.AppConfig.String(CfgParticipationDatabasePath), true, database.EngineRocksDB)
		if err != nil {
			CoreComponent.LogPanic(err)
		}

		pm, err := participation.NewManager(
			participationStore,
			deps.NodeBridge.ProtocolParameters,
			deps.NodeBridge.NodeStatus,
			deps.NodeBridge.MessageForMessageID,
			deps.NodeBridge.OutputForOutputID,
			deps.NodeBridge.LedgerUpdates,
		)
		if err != nil {
			CoreComponent.LogPanic(err)
		}
		CoreComponent.LogInfof("Initialized ParticipationManager at milestone %d", pm.LedgerIndex())
		return pm
	})
}

func newEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	return e
}

func configure() error {
	if err := CoreComponent.App.Daemon().BackgroundWorker("Close Participation database", func(ctx context.Context) {
		<-ctx.Done()

		CoreComponent.LogInfo("Syncing Participation database to disk...")
		if err := deps.ParticipationManager.CloseDatabase(); err != nil {
			CoreComponent.LogPanicf("Syncing Participation database to disk... failed: %s", err)
		}
		CoreComponent.LogInfo("Syncing Participation database to disk... done")
	}, shutdown.PriorityCloseDatabase); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}

func run() error {
	// create a background worker that handles the participation events
	if err := CoreComponent.Daemon().BackgroundWorker("LedgerUpdates", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting LedgerUpdates ... done")

		if err := deps.NodeBridge.LedgerUpdates(ctx, deps.ParticipationManager.LedgerIndex()+1, func(index milestone.Index, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) bool {
			timeStart := time.Now()
			if err := deps.ParticipationManager.ApplyNewLedgerUpdate(index, created, consumed); err != nil {
				CoreComponent.LogPanicf("ApplyNewLedgerUpdate failed: %s", err)
			}
			CoreComponent.LogInfof("Applying milestone %d with %d new and %d outputs took %s", index, len(created), len(consumed), time.Since(timeStart).Truncate(time.Millisecond))
			return true
		}); err != nil {
			CoreComponent.LogWarnf("Listening to LedgerUpdates failed: %s", err)
			deps.ShutdownHandler.SelfShutdown("disconnected from INX")
		}

		CoreComponent.LogInfo("Stopping LedgerUpdates ... done")
	}, daemon.PriorityStopParticipation); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	// create a background worker that handles the API
	if err := CoreComponent.Daemon().BackgroundWorker("API", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting API ... done")

		bindAddr := deps.AppConfig.String(CfgParticipationBindAddress)
		e := newEcho()
		setupRoutes(e)
		go func() {
			CoreComponent.LogInfof("You can now access the API using: http://%s", bindAddr)
			if err := e.Start(bindAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
				CoreComponent.LogWarnf("Stopped REST-API server due to an error (%s)", err)
			}
		}()

		if err := deps.NodeBridge.RegisterAPIRoute(APIRoute, bindAddr); err != nil {
			CoreComponent.LogWarnf("Error registering INX api route (%s)", err)
		}

		<-ctx.Done()
		CoreComponent.LogInfo("Stopping API ...")

		if err := deps.NodeBridge.UnregisterAPIRoute(APIRoute, bindAddr); err != nil {
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
