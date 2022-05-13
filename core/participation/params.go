package participation

import (
	flag "github.com/spf13/pflag"

	"github.com/iotaledger/hive.go/app"
)

const (
	// CfgParticipationDatabasePath the path to the database folder.
	CfgParticipationDatabasePath = "participation.databasePath"

	// CfgParticipationBindAddress bind address on which the Participation HTTP server listens.
	CfgParticipationBindAddress = "participation.bindAddress"
)

var params = &app.ComponentParams{
	Params: func(fs *flag.FlagSet) {
		fs.String(CfgParticipationDatabasePath, "database", "the path to the database folder")
		fs.String(CfgParticipationBindAddress, "localhost:9892", "bind address on which the Participation HTTP server listens")
	},
	Masked: nil,
}
