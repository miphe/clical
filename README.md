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
- Colors come only from your terminal's 16 ANSI colors, so clical follows
  your terminal theme (including Omarchy themes) with no configuration, and
  respects `NO_COLOR`.

## Adding a calendar

The easiest way is inside clical: open it with `clical` and press `a`. Fill
in the account (a group name such as Personal or Work), a name, and the
secret iCal URL (see step 2 below for where to find it). The URL is masked
on screen and checked before it's saved. Or use the command line:

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

## Using it

    clical                 # open the calendar (asks for the passphrase, then syncs)

| Key | Action |
|---|---|
| `tab`, `1` / `2` | Switch view: agenda / week (both stay on the same day) |
| `j` / `k`, arrows | Move between events |
| `g` / `G` | First / last event (of the day, in the week view) |
| `h` / `l` (week view) | Previous / next day |
| `enter` | Event details (`esc` or `q` to close) |
| `[` / `]` | Previous / next day in the agenda, week in the week view (the current day is marked ▸) |
| `t` | Back to today |
| `r` | Sync now |
| `h` / `l` | Focus the calendar list / back (in the week view, `h` on Monday) |
| `a` | Add a calendar |
| `d` (calendar list) | Remove the selected calendar (asks first) |
| `space` (calendar list) | Show or hide a calendar, or all of an account's calendars |
| `enter` (calendar list) | Fold or unfold an account |
| `c` (calendar list) | Collapse or expand the calendar list |
| `q` | Quit |

Without the full screen:

    clical sync            # fetch the latest events
    clical agenda          # print the next 14 days from the local cache
    clical agenda --days 30

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

    cmd/clical/                  CLI commands, terminal prompts, starts the TUI    ┐ front-ends
    internal/ui/                 Bubble Tea v2 TUI: model, view, agenda rows, keys ┘
    internal/app/                the seam: the only API front-ends call
    internal/engine/             sync engine: provider → store                     ┐
    internal/providers/ical/     iCal feed provider (primary)                      │
    internal/providers/google/   Google OAuth flow (shelved fallback)              │ core
    internal/store/              SQLite cache + sync tokens                        │
    internal/secrets/            age-encrypted secret vault                        │
    internal/agenda/             pure day-grouping of events                       │
    internal/domain/             provider-agnostic types                           ┘
    internal/archtest/           test that enforces the layering

How the layers fit, and where new code goes: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
Notes for coding agents: [`AGENTS.md`](AGENTS.md).

## Status

M0–M5 done: iCal provider with retries and dead-feed detection, calendar
management, `sync`, plain-text `agenda`, and the full-screen TUI with Agenda and Week
views, event details and calendar toggles. Next: the Month view (M7). Milestone table in
[`docs/PLAN.md`](docs/PLAN.md).
