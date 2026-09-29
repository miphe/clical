// Command clical is a terminal calendar app.
package main

import (
	"github.com/alecthomas/kong"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

// CLI is the root command structure.
type CLI struct {
	Version   kong.VersionFlag `help:"Print version and exit."`
	Agenda    AgendaCmd        `cmd:"" help:"Print upcoming events from the local cache."`
	Sync      SyncCmd          `cmd:"" help:"Fetch the latest events for every calendar."`
	Calendars CalendarsCmd     `cmd:"" help:"Manage calendars."`
	Auth      AuthCmd          `cmd:"" hidden:"" help:"Google OAuth accounts (fallback when a secret iCal URL isn't available)."`
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
