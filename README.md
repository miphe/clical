# clical

Terminal calendar app for macOS and Arch Linux. Read-only in v1: it pulls your
calendars (e.g. Google Calendar, personal and work) from their secret iCal
feeds and shows them merged in one agenda/week/month view. Elm-architecture TUI
(Bubble Tea), pure-Go SQLite cache, age-encrypted secrets.

## Key decisions

Full rationale in [`docs/PLAN.md`](docs/PLAN.md).

- Go, single static binary, darwin+linux only (never Windows).
- Elm architecture: immutable model, pure `Update`, all I/O as `tea.Cmd`.
- Integration: iCal secret URLs — no Google Cloud project or OAuth. Google
  OAuth code is kept but shelved as a fallback.
- Secrets (feed URLs) live only in an age-encrypted vault, keyed by a
  passphrase held in memory for the session. No OS keychain.
- Background sync is in-process polling while the TUI runs — no daemon.
- All-day events are civil dates (no timezone), never UTC timestamps.
- Config and themes: YAML, hot-reloaded.

## Adding a calendar

Until there's a release, run clical from the repo with `go run ./cmd/clical`
(or `go install ./cmd/clical` to get a `clical` binary on your `PATH`).

For each Google calendar you want in clical:

1. Open [Google Calendar](https://calendar.google.com/) → **Settings** → pick
   the calendar under "Settings for my calendars" → **Integrate calendar**.
2. Copy **Secret address in iCal format**. Treat it like a password: anyone
   with it can read the calendar. (If it's missing on a work account, your
   Workspace admin has disabled it.)
3. Run:

       clical calendars add --account Work --name "Team"

   Paste the URL at the hidden prompt, then enter your vault passphrase (the
   first time, this sets it — remember it). clical fetches the feed once to
   check it before saving.
4. `clical calendars list` shows what's been added.

## Syncing and viewing

    clical sync            # fetch the latest events (asks for the passphrase)
    clical agenda          # print the next 14 days from the local cache
    clical agenda --days 30

The full-screen TUI isn't built yet; `agenda` is a plain-text stand-in.

The first time, clical creates the vault and asks for a new passphrase twice.
It can't be recovered if forgotten; the fix is to delete
`~/.local/share/clical/vault/` and add your calendars again.

## Managing calendars

    clical calendars list
    clical calendars set-url Main       # after resetting a secret address
    clical calendars remove Main        # asks y/N; --yes to skip

Calendars are referenced by name or by the ID shown in `list`; add
`--account Work` if the name exists in more than one account. If a secret
address leaks, use "Reset" next to it in Google Calendar, then
`clical calendars set-url` with the new one.

## Layout

    cmd/clical/                  entry point, CLI wiring (kong)
    internal/domain/             provider-agnostic models + CalendarProvider interface
    internal/providers/ical/     iCal feed provider (primary)
    internal/providers/google/   Google OAuth flow (shelved fallback)
    internal/store/              SQLite cache + sync-token persistence
    internal/engine/             sync engine: provider → store
    internal/agenda/             pure day-grouping of events (CLI + TUI)
    internal/secrets/            age-encrypted secret vault
    internal/config/             YAML load/merge/watch
    internal/theme/              theme structs, embedded defaults, resolver
    internal/ui/                 Bubble Tea models: app, agenda, week, month; keymap; help

## Status

M0–M4 done: domain types, vault, store, iCal provider with retries and
revoked-feed detection, `calendars add`/`list`/`remove`/`set-url`, `sync`, and
a plain-text `agenda`. Next: the TUI (M5). Milestone table in
[`docs/PLAN.md`](docs/PLAN.md).
