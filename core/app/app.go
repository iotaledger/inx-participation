package app

import (
	"github.com/gohornet/hornet/core/gracefulshutdown"
	"github.com/gohornet/hornet/plugins/profiling"
	"github.com/gohornet/inx-participation/core/inx"
	"github.com/gohornet/inx-participation/core/participation"
	"github.com/iotaledger/hive.go/app"
)

var (
	// Name of the app.
	Name = "inx-participation"

	// Version of the app.
	Version = "0.1.0"
)

func App() *app.App {
	return app.New(Name, Version,
		app.WithInitComponent(InitComponent),
		app.WithCoreComponents([]*app.CoreComponent{
			inx.CoreComponent,
			participation.CoreComponent,
			gracefulshutdown.CoreComponent,
		}...),
		app.WithPlugins([]*app.Plugin{
			profiling.Plugin,
		}...),
	)
}

var (
	InitComponent *app.InitComponent
)

func init() {
	InitComponent = &app.InitComponent{
		Component: &app.Component{
			Name: "App",
		},
		NonHiddenFlags: []string{
			"config",
			"help",
			"version",
		},
	}
}
