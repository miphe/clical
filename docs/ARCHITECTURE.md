# Architecture

CLIcal has two halves with one seam between them:

- **The core** knows about calendars: feeds, recurrence, the encrypted vault,
  the SQLite cache, syncing. It knows nothing about terminals.
- **Front-ends** gather input and present output. Right now there are two:
  the CLI (`cmd/clical`) and the full-screen TUI (`internal/ui`).

The seam is **`internal/app`**: the only package front-ends call to do
anything with calendars. Replacing or adding a UI means writing a new
front-end against `app`; nothing in the core changes.

This layering is enforced by a test, `internal/archtest`, which fails `go
test ./...` if a package imports something its layer doesn't allow.

## Layers

```
                 ┌───────────────┐   ┌──────────────────────┐
  front-ends     │  cmd/clical   │──▶│     internal/ui      │
                 │ CLI, prompts, │   │ Bubble Tea TUI:      │
                 │ wiring        │   │ state, keys, render  │
                 └───────┬───────┘   └──────────┬───────────┘
                         │      only through    │
  ───────────────────────┼──────────────────────┼────────────────── the seam
                         ▼                      ▼
  application     ┌──────────────────────────────────────┐
                  │             internal/app             │
                  │ Calendars · Events · Sync · CheckFeed │
                  │ Add/Find/Remove/SetCalendarURL ·     │
                  │ Unlock · VaultExists                 │
                  └──┬──────────┬──────────┬─────────┬───┘
                     ▼          ▼          ▼         ▼
  core         internal/   internal/  internal/  internal/providers/ical
               engine ───▶ store      secrets    (feed fetch + parse)
                     │          │          │         │
                     ▼          ▼          ▼         ▼
  shared data  internal/domain (types)   internal/agenda (pure day layout)
```

| Package                     | Role                                                                                             | May import (clical packages)                                     |
| --------------------------- | ------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------- |
| `internal/domain`           | Provider-agnostic types: `Event`, `Calendar`, `Account`, `CivilDate`, `CalendarProvider`, errors | —                                                                |
| `internal/agenda`           | Pure layout shared by every front-end: group events into local days, `Item.TimeLabel`            | domain                                                           |
| `internal/secrets`          | age-encrypted vault for feed URLs                                                                | —                                                                |
| `internal/config`           | The settings file: clock, week start, sync interval                                              | —                                                                |
| `internal/store`            | SQLite cache: accounts, calendars, events, sync tokens                                           | domain                                                           |
| `internal/providers/ical`   | Fetch, retry and parse iCal feeds                                                                | domain, eventtext                                                |
| `internal/eventtext`        | Readable notes and a clean link list from an event's raw content (provider-neutral)              | domain                                                           |
| `internal/providers/google` | Google Calendar API: sign-in, calendar list, event sync                                          | domain, eventtext                                                |
| `internal/engine`           | Run a provider sync, apply it to the store                                                       | domain, store                                                    |
| `internal/app`              | **The seam.** Composes the above into operations                                                 | domain, store, secrets, engine, providers/ical, providers/google |
| `internal/ui`               | TUI: model, keys, rendering                                                                      | app, agenda, domain                                              |
| `cmd/clical`                | CLI commands, terminal prompts, starts the TUI                                                   | app, ui, agenda, domain, config                                  |

Front-ends may also use `domain` and `agenda` directly. Those are plain
data and pure functions, so sharing them doesn't couple a front-end to how
calendars are stored or fetched.

Only front-ends may import terminal/UI libraries: Charm (`charm.land/…`,
`github.com/charmbracelet/…`), `kong`, `golang.org/x/term`, `uniseg`.

There are no exemptions. (The shelved Google OAuth command was the one, until
Google sign-in moved into `app`.)

## Theming

Colors are a front-end concern, and CLIcal delegates them to the terminal.
`internal/ui/theme.go` is the only file that names colors, and only as the
terminal's 16 ANSI indices behind semantic roles. Core packages never deal
with color; the closest they get is a calendar's permanent `ColorSlot`
number, which the front-end turns into a hue and a bullet shape. Rules and
reasons are in `docs/PLAN.md` ("Theming"); enforcement is in
`internal/archtest/colors_test.go`.

## Where does new code go?

Ask: _would a different UI need this too?_

- **Yes** → core. It's calendar behaviour: a new provider, a new kind of
  query, validation, anything touching the network, disk or vault. Put it in
  the right core package, and expose it to front-ends as a method on
  `app.App`.
- **No** → the front-end. Prompts, key bindings, layout, colors, wording,
  what to show while loading.

Some concrete cases:

| Change                                                                        | Goes in                                                         |
| ----------------------------------------------------------------------------- | --------------------------------------------------------------- |
| A new feed provider (CalDAV, Outlook)                                         | `internal/providers/<name>` + registration in `app.Unlock`      |
| Recognising a new video-call or document service                              | the host table in `internal/eventtext/eventtext.go`             |
| How links are listed, selected and opened                                     | `internal/ui/detail.go`                                         |
| "Show only events matching X" as data                                         | `app` method (or `store` query exposed via `app`)               |
| Which week a day belongs to, a month's grid (`agenda.WeekStart`, `MonthGrid`) | `agenda` (pure, reusable)                                       |
| The Week view's columns, keys, colors (`ui/week*.go`)                         | `internal/ui`                                                   |
| A new CLI subcommand                                                          | `cmd/clical`, calling `app`                                     |
| How a dead feed is _worded_ on screen                                         | the front-end (`app` reports it as `SyncOutcome.Unavailable()`) |

## The contract front-ends rely on

- **`app.Open(app.DefaultPaths())`** opens the data directory. Reading works
  immediately: `Calendars`, `Events(from, to)`, `FindCalendar`, `CheckFeed`.
- **`Unlock(passphrase)`** is needed for anything touching feed URLs: `Sync`,
  `AddCalendar`, `RemoveCalendar`, `SetCalendarURL`. Before that they return
  `app.ErrLocked`. `VaultExists()` tells a front-end whether this passphrase
  will be a _new_ one, so it can ask for confirmation.
- **Results are data, errors are typed.** Front-ends decide the wording:
  `SyncOutcome.Unavailable()` (feed URL reset or deleted),
  `*app.NoCalendarError`, `*app.AmbiguousCalendarError`, `app.ErrLocked`,
  `secrets.ErrWrongPassphrase`, and `*domain.CalendarUnavailableError` from
  `CheckFeed`.
- **`app.Feed` can only come from `CheckFeed`,** so no front-end can store an
  unchecked URL.
- **`Events` never touches the network;** only `Sync`/`SyncCalendar` do.
- **Colors are slots, not colors.** `app` gives each calendar a permanent
  `ColorSlot`; each front-end decides what color a slot is.

## How the TUI stays a pure presentation layer

`internal/ui` follows the Elm architecture. `Update` is pure: it changes the
model and returns commands, and never does I/O. All I/O goes through
`ui.Source`, an interface _defined by the UI_: `Calendars`, `Events`,
`Sync`, `SyncCalendar`, `CheckFeed`, `AddCalendar` and `RemoveCalendar`. `*app.App` satisfies it, and
`cmd/clical/tui.go` passes the app straight in. Tests use a stub `Source`,
feed messages into `Update`, and snapshot `View()` against golden files.

To replace the TUI:

1. Write a new package that uses `app` (directly, or through its own small
   interface the way `ui.Source` does).
2. Add it to the layer table in `internal/archtest` as a front-end.
3. Start it from a command in `cmd/clical`.

Nothing under the seam needs to change.

## Testing by layer

- **Core:** ordinary unit tests against real SQLite and real vaults in temp
  directories. iCal fetching is tested against `httptest` servers.
- **`app`:** lifecycle tests (add → find → set URL → remove), lock guards.
- **`ui`:** golden screens plus `Update`-driven behaviour tests, with no
  terminal.
- **CLI/TUI end to end:** the built binary, with an isolated
  `XDG_DATA_HOME` and Google's public holidays feed. Use `expect` for
  prompts, and `scripts/tui_screens.py` (a `pyte` terminal emulator) to see
  real TUI screens.
- **Layering:** `internal/archtest`.
