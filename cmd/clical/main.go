// Command clical is a terminal calendar app.
package main

import (
	"github.com/alecthomas/kong"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

// CLI is the root command structure. Subcommands (auth, calendars, sync, ...)
// are added as Phase 1 progresses.
type CLI struct {
	Version kong.VersionFlag `help:"Print version and exit."`
	Auth    AuthCmd          `cmd:"" help:"Manage connected accounts."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("clical"),
		kong.Description("A terminal calendar app."),
		kong.Vars{"version": version},
	)
	ctx.FatalIfErrorf(ctx.Run())
}
