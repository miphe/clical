# CLIcal

Terminal calendar app for macOS and Arch Linux. Read-only in v1: it pulls your
calendars (e.g. Google Calendar, personal and work) from their secret iCal
feeds and shows them merged in one agenda/week/month view. Elm-architecture TUI
(Bubble Tea), pure-Go SQLite cache, age-encrypted secrets.

## Key decisions

Full rationale in [`docs/PLAN.md`](docs/PLAN.md).

- Go, single static binary, darwin+linux only (never Windows).
- Elm architecture: immutable model, pure `Update`, all I/O as `tea.Cmd`.
- Integration: iCal secret URLs (no setup), or Google accounts signed in
  with your own OAuth client (for calendars without a secret address, such
  as many work accounts).
- Secrets (feed URLs) live only in an age-encrypted vault, keyed by a
  passphrase held in memory for the session. No OS keychain.
- Background sync is in-process polling while the TUI runs — no daemon.
- All-day events are civil dates (no timezone), never UTC timestamps.
- Colors come only from your terminal's 16 ANSI colors, so CLIcal follows
  your terminal theme (including Omarchy themes) with no configuration, and
  respects `NO_COLOR`.

## Adding a calendar

The easiest way is inside CLIcal: open it with `clical` and press `a`. Fill
in the account (a group name such as Personal or Work), a name, and the
secret iCal URL (see step 2 below for where to find it). The URL is masked
on screen and checked before it's saved. Or use the command line:

Until there's a release, run CLIcal from the repo with `go run ./cmd/clical`
(or `go install ./cmd/clical` to get a `clical` binary on your `PATH`).

For each Google calendar you want in CLIcal:

1. Open [Google Calendar](https://calendar.google.com/) → **Settings** → pick
   the calendar under "Settings for my calendars" → **Integrate calendar**.
2. Copy **Secret address in iCal format**. Treat it like a password: anyone
   with it can read the calendar. (If it's missing on a work account, your
   Workspace admin has disabled it.)
3. Run:

       clical calendars add --account Work --name "Team"

   Paste the URL at the hidden prompt, then enter your vault passphrase (the
   first time, this sets it — remember it). CLIcal fetches the feed once to
   check it before saving.

4. `clical calendars list` shows what's been added.

## Adding a Google account (e.g. work calendars)

Use this when a calendar has no "Secret address in iCal format", for example
because your Workspace admin disabled it. Don't use the _public_ address for
those: it contains your email address, so anyone can read the calendar.

Once, in your **personal** Google account
([Google Cloud Console](https://console.cloud.google.com/)):

1. Create a project and enable the **Google Calendar API**.
2. Under _Google Auth Platform_: set the audience to **External**, and add
   the data-access scope `…/auth/calendar.readonly` (plus `openid`, `email`).
3. Create an **OAuth client** of type **Desktop app**; note its client ID and
   secret.
4. While the app is in **Testing**, add every account you'll sign in with
   under _Audience → Test users_; sign-ins then expire after 7 days. Press
   **Publish app** to stop that (the "unverified app" warning on sign-in is
   expected).

Then in CLIcal, per account: press `a`, choose **Google account**, enter the
client ID and secret (first time only), sign in in the browser that opens,
and tick the calendars to show. When a sign-in expires, the account is
marked ⚠ in the calendar list; select it and press `s`.

If a company's sign-in says access is blocked by the administrator, ask IT
to trust your OAuth client ID (Admin console → Security → API controls).

## Using it

    clical                 # open the calendar (asks for the passphrase, then syncs)

| Key                        | Action                                                                            |
| -------------------------- | --------------------------------------------------------------------------------- |
| `0`                        | Go to the calendar list                                                           |
| `1` / `2` / `3`            | Go to the main view as agenda / week / month (all stay on the same day)           |
| `tab`                      | Switch between the calendar list and the main view                                |
| `j` / `k`, arrows          | Move between events                                                               |
| `g` / `G`                  | First / last event (of the day, in the week view)                                 |
| `h` / `l` (week, month)    | Previous / next day (never changes pane)                                          |
| `j` / `k` (month)          | Previous / next week                                                              |
| `enter` (month)            | Open the selected day in the agenda                                               |
| `enter`                    | Event details (`esc` or `q` to close)                                             |
| `j` / `k`, `1`–`9` (event) | Select a link: its full address is shown at the bottom                            |
| `enter` (event)            | Open the selected link in your browser                                            |
| `PgUp` / `PgDn` (event)    | Scroll long notes                                                                 |
| `[` / `]`                  | Previous / next day in the agenda, week in the week view, month in the month view |
| `t`                        | Back to today                                                                     |
| `r`                        | Sync now                                                                          |
| `a`                        | Add a calendar (iCal link or Google account)                                      |
| `s` (calendar list)        | Sign a Google account in again (when marked ⚠)                                    |
| `u` (calendar list)        | Give an iCal calendar its new secret address (when marked ⚠)                      |
| `n` (calendar list)        | Rename the selected calendar or account (only in CLIcal)                          |
| `d` (calendar list)        | Remove the selected calendar (asks first)                                         |
| `space` (calendar list)    | Show or hide a calendar, or all of an account's calendars                         |
| `enter` (calendar list)    | Fold or unfold an account                                                         |
| `l` (calendar list)        | Back to the main view (same as `tab`)                                             |
| `c` (calendar list)        | Collapse or expand the calendar list                                              |
| `/`                        | Filter events by title or location; `esc` clears                                  |
| `?`                        | All keys and what the status line shows                                           |
| `q`                        | Quit                                                                              |

Without the full screen:

    clical sync            # fetch the latest events
    clical agenda          # print the next 14 days from the local cache
    clical agenda --days 30

The first time, CLIcal creates the vault and asks for a new passphrase twice.
It can't be recovered if forgotten; the fix is to delete
`~/.local/share/clical/vault/` and add your calendars again.

## Managing calendars

    clical calendars list
    clical calendars rename Main Family # your own name; leave it out to undo
    clical accounts rename Personal Home
    clical calendars set-url Main       # after resetting a secret address
    clical calendars remove Main        # asks y/N; --yes to skip

Calendars are referenced by name, nickname or the ID shown in `list`; add
`--account Work` if the name exists in more than one account. A rename
only changes what CLIcal shows: the calendar in Google Calendar, and the
email address a Google account signs in with, stay as they are. If a secret
address leaks, use "Reset" next to it in Google Calendar, then
`clical calendars set-url` with the new one, or press `u` on the calendar in
the TUI. A calendar whose address stopped working is marked ⚠.

## Settings

Optional, in `~/.config/clical/config.yaml`:

    time_format: 24h      # or 12h
    week_start: monday    # or sunday
    sync_interval: 5m     # time between background syncs, e.g. 1h, or off

`clical config` shows the file's path and the settings in use, and
`clical config init` writes the file with every setting at its default.
Changes apply the next time CLIcal starts; a misspelt setting or value
stops it with an error naming it.

## Log

Sync failures and recoveries, and calendar and account changes, are logged
to `~/.local/state/clical/clical.log` (names only, never addresses or
sign-ins). `clical logs` prints the end of it; `clical logs -f` keeps
following it.

## Layout

    cmd/clical/                  CLI commands, terminal prompts, starts the TUI    ┐ front-ends
    internal/ui/                 Bubble Tea v2 TUI: model, view, agenda rows, keys ┘
    internal/app/                the seam: the only API front-ends call
    internal/engine/             sync engine: provider → store                     ┐
    internal/providers/ical/     iCal feed provider (primary)                      │
    internal/providers/google/   Google accounts: sign-in, calendar API            │ core
    internal/eventtext/          readable notes and links from event content       │
    internal/store/              SQLite cache + sync tokens                        │
    internal/secrets/            age-encrypted secret vault                        │
    internal/config/             the settings file                                 │
    internal/agenda/             pure day-grouping of events                       │
    internal/domain/             provider-agnostic types                           ┘
    internal/archtest/           test that enforces the layering

How the layers fit, and where new code goes: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
Notes for coding agents: [`AGENTS.md`](AGENTS.md).

## Status

All planned milestones (M0–M11) are done: iCal and Google calendars,
background syncing, the full-screen TUI with Agenda, Week and Month views,
event details with links, filtering, help, renaming, settings and a log.
Milestone table in
[`docs/PLAN.md`](docs/PLAN.md).
