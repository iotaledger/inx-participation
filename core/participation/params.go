package participation

import (
	"github.com/iotaledger/hive.go/core/app"
)

type ParametersParticipation struct {
	Database struct {
		// Engine defines the used database engine (pebble/rocksdb/mapdb).
		Engine string `default:"rocksdb" usage:"the used database engine (pebble/rocksdb/mapdb)"`
		// Path defines the path to the database folder.
		Path string `default:"database" usage:"the path to the database folder"`
	} `name:"db"`
}

// ParametersRestAPI contains the definition of the parameters used by the Participation HTTP server.
type ParametersRestAPI struct {
	// BindAddress defines the bind address on which the Participation HTTP server listens.
	BindAddress string `default:"localhost:9892" usage:"the bind address on which the Participation HTTP server listens"`

	// AdvertiseAddress defines the address of the Participation HTTP server which is advertised to the INX Server (optional).
	AdvertiseAddress string `default:"" usage:"the address of the Participation HTTP server which is advertised to the INX Server (optional)"`

	// DebugRequestLoggerEnabled defines whether the debug logging for requests should be enabled
	DebugRequestLoggerEnabled bool `default:"false" usage:"whether the debug logging for requests should be enabled"`
}

var ParamsParticipation = &ParametersParticipation{}
var ParamsRestAPI = &ParametersRestAPI{}

var params = &app.ComponentParams{
	Params: map[string]any{
		"participation": ParamsParticipation,
		"restAPI":       ParamsRestAPI,
	},
	Masked: nil,
}
