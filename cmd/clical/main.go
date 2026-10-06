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
	Mock      bool             `help:"Open with made-up calendars, events and tasks; nothing real is read or written." hidden:""`
	TUI       TUICmd           `cmd:"" name:"open" default:"1" help:"Open the calendar (the default when no command is given)."`
	Agenda    AgendaCmd        `cmd:"" help:"Print upcoming events from the local cache."`
	Sync      SyncCmd          `cmd:"" help:"Fetch the latest events for every calendar."`
	Calendars CalendarsCmd     `cmd:"" help:"Manage calendars."`
	Accounts  AccountsCmd      `cmd:"" help:"Manage accounts, the groups calendars are listed under."`
	Config    ConfigCmd        `cmd:"" help:"Show or create the settings file."`
	Logs      LogsCmd          `cmd:"" help:"Print the end of the log: sync failures and calendar changes."`
	Write     WriteCmd         `cmd:"" help:"Notes and TODOs, global or on an event."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("clical"),
		kong.Description("A terminal calendar app."),
		kong.Vars{"version": version},
		kong.Bind(&cli),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
