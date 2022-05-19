package participation

import (
	"github.com/iotaledger/hive.go/app"
)

type ParametersParticipation struct {
	DatabasePath string `default:"database" usage:"the path to the database folder"`
	BindAddress  string `default:"localhost:9892" usage:"bind address on which the Participation HTTP server listens"`
}

var ParamsParticipation = &ParametersParticipation{}

var params = &app.ComponentParams{
	Params: map[string]any{
		"participation": ParamsParticipation,
	},
	Masked: nil,
}
