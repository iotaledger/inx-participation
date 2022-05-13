package inx

import (
	"context"

	"go.uber.org/dig"

	"github.com/gohornet/hornet/pkg/shutdown"
	"github.com/gohornet/inx-participation/pkg/daemon"
	"github.com/gohornet/inx-participation/pkg/nodebridge"
	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/configuration"
)

func init() {
	CoreComponent = &app.CoreComponent{
		Component: &app.Component{
			Name:     "INX",
			DepsFunc: func(cDeps dependencies) { deps = cDeps },
			Params:   params,
			Provide:  provide,
			Run:      run,
		},
	}
}

type dependencies struct {
	dig.In
	AppConfig  *configuration.Configuration `name:"appConfig"`
	NodeBridge *nodebridge.NodeBridge
}

var (
	CoreComponent *app.CoreComponent
	deps          dependencies
)

func provide(c *dig.Container) error {

	type inxDeps struct {
		dig.In
		AppConfig       *configuration.Configuration `name:"appConfig"`
		ShutdownHandler *shutdown.ShutdownHandler
	}

	if err := c.Provide(func(deps inxDeps) (*nodebridge.NodeBridge, error) {
		return nodebridge.NewNodeBridge(CoreComponent.Daemon().ContextStopped(),
			deps.AppConfig.String(CfgINXAddress),
			CoreComponent.Logger())
	}); err != nil {
		return err
	}

	return nil
}

func run() error {
	return CoreComponent.Daemon().BackgroundWorker("INX", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting NodeBridge")
		deps.NodeBridge.Run(ctx)
		CoreComponent.LogInfo("Stopped NodeBridge")
	}, daemon.PriorityDisconnectINX)
}
