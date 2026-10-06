# clical — Phase 1 (MVP) design & implementation plan

The living design doc for Phase 1, kept in the repo so the project can be
resumed from a fresh session. Update this file, not just chat history, when
decisions change.

## Context

`clical` is a terminal calendar app (Go, Bubble Tea/Elm architecture), read-only
in v1. It shows calendars from several sources — today, Google Calendar
personal and work accounts — merged into one agenda/week/month view.

The integration is **iCal secret URLs**: every Google calendar exposes a private
read-only feed ("Secret address in iCal format"). clical fetches that over
HTTPS. No Google Cloud project, no OAuth, no token expiry. Other providers
(Outlook, iCloud, Fastmail, CalDAV) publish the same kind of feed, so the same
provider covers them. Google accounts can also sign in with OAuth, for
calendars without a usable secret address (see "Google accounts").

Phase 1: add calendars → sync feeds into SQLite → agenda/week/month TUI →
multi-account merge → config/themes → in-process background polling.

## Status (update this as milestones land)

| # | Milestone | Status |
|---|---|---|
| M0 | Domain skeleton | Done |
| M1 | Vault + account/calendar store | Done |
| M2 | iCal provider + `calendars add` / `calendars list` | Done — live-checked with a real secret URL |
| M3 | `clical sync` into SQLite + plain-text `clical agenda` | Done |
| M4 | Feed errors, backoff, `calendars remove`/`set-url`, new-vault passphrase confirmation | Done |
| M5 | TUI shell, Agenda view, event detail, calendar toggles | Done |
| — | Application layer `internal/app` + enforced layering (`internal/archtest`) | Done |
| M6 | Week view | Done |
| — | Many calendars: scrolling/foldable list, permanent colors, add/remove in the TUI | Done |
| — | Theming: terminal ANSI colors only (Omarchy-compatible), semantic roles, enforced | Done |
| M7 | Month view | Done |
| — | Event links: extraction, readable notes, select-and-open with the full address shown | Done |
| — | Google accounts via OAuth (sign-in, calendar picker, sync, re-sign-in) | Done — live-checked with real accounts |
| M10 | Background polling every 5 minutes; failures shown and retried | Done |
| M8 | `?` help screen, footer keeps the most important keys when narrow, `/` filter | Done |
| M9 | Settings file: 12/24-hour clock, week start, sync interval | Done |
| M11 | Dead iCal feeds fixed from the TUI, log file and `clical logs` | Done |
| M12 | Settings dialog (`S`), event pulse before start | `internal/ui/settings.go`, `internal/ui/pulse.go` | Done |
| M13 | Write: notes and TODOs in event and global scopes, archive, `clical write` | `internal/write/`, `internal/ui/write*.go`, `cmd/clical/write.go` | Done (M13d, the TUI archive view, is deferred) |
| — | Release pipeline: GoReleaser, GitHub releases, PolyForm Noncommercial license | `.goreleaser.yaml`, `LICENSE.md` | Done (v0.0.1) |
| M14 | Focus view (`4`): slim agenda, tasks panel, completion heatmap | `internal/write/`, `internal/ui/focus*.go`, `internal/ui/heatmap.go` | Done (v0.0.2 candidate) |

Work (Google Workspace) calendars, where admins disabled secret iCal
addresses, connect as Google accounts via OAuth (revived 2026-10-02; see
"Google accounts").

## Decisions

### Integration
- **iCal secret URLs are the primary integration** (decided 2026-09-29).
  Google OAuth required a Cloud project, consent screen and OAuth client per
  user, plus 7-day refresh-token expiry in "Testing" mode — too much setup.
  Trade-offs accepted: one URL per calendar, no incremental sync, recurrence
  expanded client-side, and read-only forever on this path (writes would need
  OAuth or CalDAV).
- **`CalendarProvider` is slimmed to `SyncEvents` only.** `Authenticate` and
  `ListCalendars` were dropped from the interface: iCal has no login and no
  calendar discovery, so they'd be no-op stubs. Onboarding (adding a
  calendar, logging in) is a provider-specific CLI flow.
- **Accounts are user-named groups** ("Personal", "Work"). A calendar is one
  feed URL under a group. `Account.ID = NewAccountID("ical", label)`;
  `Calendar.ID = sha256(normalized URL)[:16]`, so re-adding the same feed is
  recognized and the ID never reveals the URL.
- **The feed URL is a credential.** It's stored only in the age vault (key
  `ical-url-<calendarID>`), never in the DB, config, logs or errors
  (`ical.redact` strips it from HTTP client errors). `calendars add` reads it
  from a hidden prompt, not a flag, so it stays out of shell history. Only
  `https://` (or `webcal://`, rewritten to https) is accepted.
- **Sync = full download, skipped when unchanged.** Google's feeds send
  neither `ETag` nor `Last-Modified`, so conditional GET never applies to
  them. The sync token (JSON) records `day`, `etag`, `lastModified` and a
  sha256 `hash` of the *parsed events* (ID, title, span, location, notes):
  the same events on the same day is a no-op (no DB rewrite, no UI refresh),
  though it still costs a download. It hashes events, not the raw body,
  because Google rewrites every event's `DTSTAMP` to the download time — a
  body hash never matches. A new day forces re-expansion because the window
  slides forward.
- **Expansion window**: occurrences within now −30 days … now +365 days.
  Every changed fetch returns `FullResync: true`, replacing the calendar's
  events in the store.
- **Event IDs** are `UID + "/" + start` (RFC 3339 UTC for timed, `YYYY-MM-DD`
  for all-day), since every occurrence of a recurring event shares one UID.
- **All-day end dates are exclusive.** gocal reports an all-day event's end as
  the last second of its final day (inclusive); `parse.go` converts that to
  the exclusive `CivilDate` convention. Caught by the multi-day fixture test.
- **Cancelled events** (`STATUS:CANCELLED`) are dropped at parse time.
- **Library**: `github.com/apognu/gocal` v0.9.1 — parses feeds, expands
  RRULEs within a window, honors EXDATE and RECURRENCE-ID overrides.
- **Recurrence guard** (`guard.go`): feeds are untrusted, and gocal loops
  forever on some rules (`INTERVAL=0`, a `COUNT` that BYDAY never reaches).
  Every RRULE is rewritten before gocal sees it so it always advances and ends
  just past the window, and feeds that would still cost more than about two
  seconds to expand are rejected. The costs follow what gocal actually does,
  so a calendar with years of history isn't penalized for it. The rewrite
  also fixes gocal's `UNTIL`, which it treats as exclusive (RFC 5545 makes it
  inclusive), reads as the day before when date-only, and compares across
  zones as wall-clock strings.

### Carried over from the original plan
- **Vault KDF**: age's native scrypt passphrase mode; no separate argon2id
  layer (redundant). Files stay openable with the standard `age` CLI.
- **Passphrase caching scope** = one long-running TUI process. Short-lived
  CLI subcommands re-prompt each time. No agent/daemon.
- **Dependencies**: `github.com/samber/lo` is fine for slice ops over events;
  `github.com/samber/mo` was rejected (fights idiomatic Go error handling).
- **Commits**: authored solely by the user; never any AI attribution or
  trailers. Conventional Commits (Commitizen) format.

## Domain model (`internal/domain`) — done

```go
type Account struct {
    ID       string // NewAccountID(provider, label): sha256("provider:label")[:16], hex
    Provider string // "ical" or "google"
    Label    string // user-chosen group name, e.g. "Work"
}

type Calendar struct {
    ID, AccountID, Name, Color string // Color empty until M8 assigns from theme
}

// CivilDate: timezone-less date for all-day events. Never time.Time.
type CivilDate struct{ Year, Month, Day int }

// Event: unexported time fields + constructors + ok-returning accessors.
func NewTimedEvent(id, calID, title string, start, end time.Time, loc, notes string) Event
func NewAllDayEvent(id, calID, title string, start, end CivilDate, loc, notes string) Event
func (e Event) TimeRange() (start, end time.Time, ok bool)
func (e Event) DateRange() (start, end CivilDate, ok bool) // end exclusive

type CalendarProvider interface {
    SyncEvents(ctx context.Context, cal Calendar, token string) (SyncResult, error)
}

type SyncResult struct {
    Upserted   []Event
    DeletedIDs []string
    NextToken  string
    FullResync bool // replace all of cal's events rather than apply a delta
}
```

`errors.go`: `CalendarUnavailableError{CalendarID, Reason}` — the source
refuses access or no longer exists (for iCal: HTTP 401/403/404/410, i.e. the
secret URL was reset or the calendar deleted). Retrying won't help; the fix is
`calendars set-url` or `remove`. `ReauthRequiredError` is the shelved OAuth
path's equivalent.

## iCal provider (`internal/providers/ical`) — done

- `url.go`: `NormalizeURL` (trim, webcal→https, https only), `CalendarID`,
  `SecretKey`.
- `parse.go`: gocal → `domain.Event`, all-day detection via `VALUE=DATE`,
  inclusive→exclusive end conversion, cancelled events skipped.
- `provider.go`: `New(lookupURL func(calendarID) (string, error))` — the
  provider never holds URLs itself; the caller resolves them from the vault.
  `SyncEvents` (conditional GET + hash no-op + full parse); `Probe` (fetch and
  count occurrences, used to validate a URL before saving). 30s HTTP timeout,
  64 MiB body limit.
- Retries (`github.com/cenkalti/backoff/v5` — the current major version,
  in place of the v4 the original spec named): network errors, 429 and 5xx
  are retried with jittered exponential backoff (1s initial, 10s max, 4
  attempts in all); 401/403/404/410 return `CalendarUnavailableError`
  immediately; other statuses fail without retry; a cancelled context stops
  retrying at once. `Retry-After` is not honored yet.
- Tests: fixture feed (TZID, single- and multi-day all-day, weekly RRULE with
  EXDATE and a RECURRENCE-ID override, cancelled event), httptest server for
  304 / no-validator hash no-op / new-day refetch / URL never in errors.
  Also verified manually against Google's public US-holidays feed.

## Google accounts (OAuth) — done (revived 2026-10-02)

For calendars without a usable secret iCal address, e.g. work accounts whose
Workspace admin disabled them. A *public* iCal address is the wrong fix for
those: it contains the email address, so anyone can guess and read it, and
it may be limited to free/busy anyway.

**Who sets up what:** the user creates one OAuth client ("Desktop app") in
a Google Cloud project under their *personal* account. The project owner
gets no calendar access; each calendar account signs in separately with
read-only scope. While the app is in "Testing", every account must be added
as a test user, and sign-ins expire after 7 days (publishing to "In
production" removes both; the unverified-app warning is expected). A
company that blocks unconfigured third-party apps can trust the client ID
in its Admin console.

**Provider** (`internal/providers/google`):
- Calls the Calendar REST API directly (`users/me/calendarList`,
  `calendars/{id}/events`, `singleEvents=true`, only the needed `fields`)
  instead of Google's large generated client library.
- Sync = the whole −30/+365-day window every time, no-op'd by an events
  hash, like iCal. Google's incremental sync tokens were skipped (window
  drift, 410 handling); revisit if quota or speed ever matter.
- Sign-in: installed-app loopback flow with PKCE, split into `BeginSignIn`
  (returns the address, so the TUI can show it) and `Wait`. `login_hint`
  preselects the account when signing in again. The callback checks
  `state`.
- Tokens: refreshed tokens are saved back to the vault; a refused refresh
  (`invalid_grant` — revoked, or the 7-day Testing expiry) or a 401 becomes
  `ReauthRequiredError`. 404/410 → `CalendarUnavailableError`. 429, 5xx and
  403 rate limits are retried; other 403s keep Google's message (e.g. "access
  blocked by your administrator", "Calendar API has not been used in
  project…").
- Events: cancelled and self-declined events are left out; an empty title
  becomes "(No title)"; Google's exclusive all-day end matches ours. Links:
  `hangoutLink` and video `conferenceData` entry points, Drive
  `attachments` (as documents, titled), and the event's `htmlLink` as "Open
  in Google Calendar" (last). Descriptions go through `eventtext` like iCal's.
- Calendar IDs are `<accountID>/<Google calendar ID>`, so a shared calendar
  seen from two accounts stays two calendars.

**Link/notes extraction** moved from `providers/ical` into a
provider-neutral core package, `internal/eventtext` (`Describe(Content)`),
used by both providers.

**App** (`internal/app/google.go`): `SavedGoogleClientID` (the secret is
never handed out), `ValidateGoogleClient` (pure; the ID must end in
`.apps.googleusercontent.com`), `BeginGoogleSignIn(expect, client)` returning
an `app.SignIn` interface (`URL`, `Wait`, `Cancel`). `Wait` rejects a
different account than expected and saves the token. **A typed-in client is
saved only when a sign-in with it succeeds**: the first version saved it as
soon as the form was submitted, so a typo followed by `esc` stuck, and the
form never came back. Google refusing the client at the code exchange
(`invalid_client`) surfaces as `app.ErrGoogleClientRejected`. Also `GoogleCalendars` (marks already-added and
free/busy-only ones) and `AddGoogleCalendars`. Removing a Google account's
last calendar also deletes its token. `SetCalendarURL` refuses non-iCal
calendars. `SyncOutcome.NeedsSignIn()`.

**TUI** (`internal/ui/googleflow.go`): `a` opens a source chooser (iCal link
/ Google account). Google: with no saved client, a client ID + secret form
(secret masked, paste works, validated before signing in). Then the browser
opens on Google's sign-in page while the popup shows the full address (host
bold, clickable; `o` opens it again). `esc` at any step discards a typed-in
client. If Google rejects it, the form comes back with the ID kept and the
secret cleared. A wrong ID can't be detected (Google shows its error page
and never redirects back), so the popup says to press `c`, which reopens
the form prefilled with the client ID in use; that also replaces a saved
client (rotated secret, other project).
Then a calendar picker: primary pre-ticked, already-added ones shown but not
tickable, `space` ticks, `enter` adds and syncs them. An account whose
access expired is flagged `⚠` in the sidebar and in the status line; `s`
on it signs it in again (preselected) and resyncs it. `esc` cancels any
step; late results are ignored. `clical sync` prints "needs signing in
again" for such accounts.

The shelved `clical auth` command was removed, and with it the only
`internal/archtest` exemption.

## Secrets vault (`internal/secrets`) — done

One age-encrypted file per key under `$XDG_DATA_HOME/clical/vault/` (dir
`0700`, files `0600`). Keys are restricted to `[A-Za-z0-9_-]` (no path
traversal).

```go
func Open(dir string, prompt func() (string, error)) (*Vault, error) // ErrWrongPassphrase if it can't decrypt an existing secret
func (v *Vault) SaveSecret(key string, data []byte) error
func (v *Vault) LoadSecret(key string) ([]byte, error)
func (v *Vault) DeleteSecret(key string) error
// SaveToken/LoadToken/DeleteToken: JSON oauth2.Token wrappers, shelved path only.
```

## Local cache (`internal/store`) — SQLite

Accounts and calendars CRUD done (`UpsertAccount`, `ListAccounts`,
`GetAccount`, `UpsertCalendar` — keeps `sync_token` on rename,
`ListCalendars` — ordered by account label then name), plus `SyncToken`,
`ApplySync` and `EventsInRange(from, to)` (timed events overlapping the
window, zero-length ones starting in it; all-day events overlapping the
window's local dates).

```sql
CREATE TABLE accounts (id TEXT PRIMARY KEY, provider TEXT NOT NULL, label TEXT NOT NULL);
CREATE TABLE calendars (
    id TEXT NOT NULL, account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL, color TEXT NOT NULL,
    sync_token TEXT, last_synced_at INTEGER,
    PRIMARY KEY (account_id, id)
);
CREATE TABLE events (
    id TEXT NOT NULL, calendar_id TEXT NOT NULL, account_id TEXT NOT NULL,
    title TEXT NOT NULL, all_day INTEGER NOT NULL,
    start_utc INTEGER, end_utc INTEGER,   -- NULL if all_day
    start_date TEXT, end_date TEXT,       -- "YYYY-MM-DD", NULL if !all_day
    location TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (account_id, calendar_id, id),
    FOREIGN KEY (account_id, calendar_id) REFERENCES calendars(account_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_events_range       ON events (calendar_id, start_utc, end_utc);
CREATE INDEX idx_events_alldayrange ON events (calendar_id, start_date, end_date);
```

DSN (modernc.org/sqlite pragma syntax):
`file:<path>?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)`.
All writes go through `Store.writeMu` + one transaction per sync result, so
the delta and the sync-token update commit atomically:

`ApplySync`: on `FullResync` delete all of the calendar's events, else
delete `DeletedIDs`; `INSERT OR REPLACE` `Upserted`; update `sync_token` and
`last_synced_at`. A no-op result only updates the last two.

## Application layer (`internal/app`) — done (2026-10-02)

The seam between calendar logic and front-ends; full description and rules
in `docs/ARCHITECTURE.md`. Added after M5 so a future UI can be swapped in
without touching the core: logic that had grown in `cmd/clical` (feed check,
add/remove/set-url flows, calendar lookup, provider↔vault wiring, the TUI's
store adapter) moved into `app`, and `internal/ui` no longer imports
`engine`. `*app.App` implements `ui.Source` directly. `internal/archtest`
fails the build on a layering violation or an unclassified new package; the
only exemption was the shelved `cmd/clical/auth.go`, removed when Google
sign-in moved into `app`; there are none now.

## Sync engine (`internal/engine`) — done; front-ends reach it via `app.Sync`

Named `engine`, not `sync`, to avoid clashing with the stdlib package.
`engine.New(store, providers map[provider name]CalendarProvider)`;
`SyncOne(ctx, acct, cal) Result` reads the calendar's token, calls the
provider, applies the result via `ApplySync`; `SyncAll` does every calendar
sequentially, one failure not stopping the rest. `Result{Account, Calendar,
Changed, Events, Err}`.

## Agenda layout (`internal/agenda`) — done

Pure `Build(events, from CivilDate, days int, loc) []Day`: each event on every
local day it overlaps (all-day by date range; timed by local-day overlap;
zero-length on its start day; an event ending exactly at midnight does not
spill into the next day). Within a day: all-day first, then by start, then
title. Used by `clical agenda` now and by the TUI Agenda view in M5.

## CLI (`cmd/clical`)

- `calendars add --account --name` (hidden URL prompt, probe, save),
  `calendars list` (no passphrase).
- `calendars remove <name|id> [--account] [--yes]`: y/N confirmation, then
  deletes the calendar, its events, its stored URL, and its account if that
  leaves it empty. Store first, vault second — a leftover encrypted URL is
  harmless; a calendar without its URL would fail every sync.
- `calendars set-url <name|id> [--account]`: hidden prompt, probe, replace the
  stored URL, reset the sync token. The calendar keeps its ID (so it stays
  derived from the *original* URL; re-adding the new URL with `add` would
  create a duplicate).
- Calendars are referenced by ID or case-insensitive name or nickname; an
  ambiguous name lists the matches and asks for the ID or `--account`.
- New vault: the first command that needs the vault warns that the
  passphrase can't be recovered and asks for it twice (must match, must be
  non-empty). `sync` skips the passphrase entirely when there are no
  calendars.
- Rejected URLs in `add`/`set-url` produce advice ("check you copied the whole
  secret address, or that it hasn't been reset") rather than a raw HTTP error;
  `sync` points at `calendars set-url <id>`.
- `sync`: unlocks the vault, syncs everything, prints per calendar "N events"
  / "unchanged" / error; exits non-zero if any calendar failed.
- `agenda [--days 14]`: plain-text upcoming events from the cache (no
  passphrase, no network). Times render as `HH:MM–HH:MM`, with `…` where an
  event continues from or into another day, `24:00` for a midnight end.
  Columns padded by display width (`uniseg`) so emoji/CJK titles align.
- Google accounts are added in the TUI (`a` → Google account); there's no
  CLI command for it.

## Settings — M9 done

`~/.config/clical/config.yaml` (`internal/config`), read once at startup by
`cmd/clical` and handed to the TUI as `ui.Settings`; the TUI never reads
files. Settings: `time_format` (24h/12h), `week_start` (monday/sunday),
`sync_interval` (a duration of at least 1m, or off). Missing file = defaults;
unknown keys and bad values are errors naming the file and setting, shown
before the passphrase prompt. `clical config` shows the path and values,
`clical config init` writes a commented file with the defaults.

Simpler than first planned: no hot-reload (fsnotify) and no mergo. With
three settings, defaults are a struct literal, and a restart is a fine way
to apply a change.

The clock lives in `agenda.Clock`, used by `Item.TimeLabel`, so the TUI and
`clical agenda` format times the same way; `Clock.Width()` sizes the time
column (11 cells for 24h, 15 for 12h). This fixed the CLI agenda's time
column, which measured "00:00–00:00" in bytes (13) instead of cells (11).

## Dead feeds and the log — M11 done

- An iCal calendar whose sync fails with `CalendarUnavailableError` is
  marked ⚠ in the list until it syncs again. `u` on any iCal calendar opens
  a hidden URL field; the address is checked (`CheckFeed`), saved
  (`SetCalendarURL`) and the calendar synced. The status line's failure
  says `(u on the calendar)`.
- `internal/app/log.go`: a `slog` text log at
  `$XDG_STATE_HOME/clical/clical.log` (0600), rotated to `.1` past 1 MiB on
  opening. The app layer logs sync failures, recoveries and changes (quiet
  syncs aren't logged), calendars added, removed, renamed or given a new
  address, accounts renamed, and Google sign-ins. Names only: addresses and
  tokens never reach it (`TestLogRecordsChangesNotSecrets`).
- `clical logs [-n 50] [-f]` prints the end of the log and can follow it,
  starting over when it's rotated.

## Background polling — M10 done

One sync of every calendar runs at startup and then **5 minutes after the
previous one finished** (`pollInterval`, configurable in M9). Scheduling from
the finish, not on a fixed beat, means syncs never overlap however slow a
feed is. `r` syncs at once and the next scheduled sync counts from it.

- Simpler than first sketched: one schedule for all calendars, running
  `app.Sync` (the engine syncs them one after another), instead of a timer
  and a jittered phase per calendar. With iCal feeds and a handful of
  Google calendars there's nothing to spread out; per-calendar timers can
  come back if many calendars on one server ever need it.
- Each schedule carries a sequence number (`pollMsg{seq}`); `r` or a newer
  schedule bumps it, so a replaced timer that still fires does nothing. A
  scheduled sync that finds another sync running (a calendar just added)
  waits another interval.
- Go's timers stop while macOS sleeps, but the wall clock doesn't: the
  30-second clock tick starts a sync that came due during sleep.
- **Failures don't stop anything.** Each calendar's last failure is kept
  (`syncFailures`, by calendar ID) until that calendar syncs cleanly again;
  the calendar keeps working from the cache meanwhile. The status line
  shows the first failure in sidebar order, `(+N more)`, and
  `· retry HH:MM`, e.g. `Personal · Main: feed URL no longer works · retry
  14:39`. Syncing one calendar alone (just added, or after signing in
  again) updates only its own entry. Failures of removed calendars are
  dropped. A dead feed or revoked access is retried like any other
  failure: harmless, and it recovers by itself if the cause goes away.

## TUI (`internal/ui`) — M5 done

`clical` with no command opens it (kong default command `open`). The vault is
unlocked with a plain prompt *before* the TUI takes the screen; the passphrase
then lives in memory for the session. No calendars → a hint instead of an
empty screen. One sync runs at startup and then every 5 minutes (M10); `r`
syncs at once. The footer shows
"Syncing…", "Synced HH:MM", or the first failure (a dead feed reads
"Personal · Main: feed URL no longer works").

Implementation decisions:
- **Bubble Tea v2** (`charm.land/bubbletea/v2` v2.0.10, `lipgloss/v2`,
  `bubbles/v2`) — the current stable line; the spec predates it. v2's `View()`
  returns `tea.View` (alt screen and window title are fields on it) and keys
  arrive as `KeyPressMsg`.
- **Pure Update, I/O via `ui.Source`** (`Calendars`, `Events(from, to)`,
  `Sync`), called only from `tea.Cmd`s. `*app.App` implements it, so
  `cmd/clical/tui.go` passes the app straight in. The clock is model state
  advanced by a 30s tick, so tests are deterministic.
- **Golden tests call `View()` directly** after feeding messages to `Update`,
  instead of `teatest`: the Elm split makes the program runtime unnecessary
  for snapshots, and it avoids timing flakiness. Goldens (`testdata/*.golden`,
  ANSI stripped, refresh with `go test ./internal/ui -update`) cover the agenda
  at 100×24 and 56×16, the focused and collapsed sidebar, and the detail view.
  A separate test asserts every line is exactly the window width.
- **Rows** (`rows.go`, pure): *every* day in the window gets a header, then
  its events or a selectable "nothing scheduled" line; today also gets a
  `── now HH:MM ──` marker before its first event starting after now. The
  selection is keyed by date+event ID (empty ID for an empty day), so it
  survives reloads.
- **Current day** = the selected row's day. Its header is marked `▸` and
  underlined; today keeps its own blue "· today" label so the two stay
  distinguishable. Default selection: today's first event after now, else
  today's last row (so late in the evening you stay on today).
- **Day navigation**: `]`/`[` select the first row of the day after/before the
  *current day* — not the agenda's start, which was the M5 first-cut bug
  (after scrolling to Thursday, `]` jumped back to Wednesday, and empty days
  were invisible so steps looked like no-ops). Stepping past the loaded
  window's start moves the start back a day; past its end loads 30 more days;
  either way the target day is selected when the load lands. `t` reloads from
  today. Rolling window: 30 days, `j` past the last row loads 30 more. Loads
  carry their window, so a stale result is dropped.
- **Calendar toggles** (planned for M8) landed now: without them the sidebar
  had nothing to do. Hidden calendars show `○` and dimmed; their events are
  filtered in `rebuild`, not re-queried.
- **Styling** follows the theming rule (see "Theming"): terminal ANSI colors
  only. Selected row: reverse video (underline while another panel has
  focus); ended events dimmed.
- **Widths**: `x/ansi` truncation/width everywhere (grapheme-aware), labels in
  a fixed column sized to the longest label (max 28), label column dropped
  under 60 columns, sidebar starts collapsed under 70 columns.
- **Event detail**: full-screen box — title, date/time with duration,
  calendar, location, wrapped notes; `esc`/`q` close. The "open in Google
  Calendar" action is dropped: the iCal feed carries no per-event link.
- `agenda.Item.TimeLabel` is shared by the TUI and `clical agenda`.

## Week view (`internal/ui/week.go`, `weekview.go`) — M6 done

- **Monday-first** weeks (`weekFirstDay`; configurable in M9). While the week
  view is open, its tab shows the ISO week number, `[2] Week 45`, since
  talking about weeks by number is common in Sweden (and much of Europe).
  The panel's top-right shows the dates, `2 Nov – 8 Nov 2026`. On a narrow
  screen the dates drop first; the number stays in the tab.
- Seven day columns of events in time order; each event takes two lines: a
  calendar-colored bullet with its time (`all day`, start `HH:MM`, or
  `…HH:MM` for one continuing from the day before), then the title. Under 9
  columns per day the bullet is dropped and the time itself carries the
  color. Under 6 the view shows a wrapped "too narrow, press 1" message.
  Overflow shows `+N more`; the selected column scrolls to keep its
  selection visible. Empty days show `–`, or `nothing` when selected.
- Headers mark today in blue and the selected day with `▸` and underline,
  as in the agenda.
- Keys: `h`/`l` move between days, crossing into the previous/next week at
  the ends (originally `h` on Monday moved focus to the sidebar; see "Pane
  keys"), `j`/`k`/`g`/`G` within a day, `[`/`]` a week back/forward keeping
  the weekday, `t` this week on today's next event, `enter` details.
- **Views share the current day.** `tab` toggles Agenda ↔ Week (`1`/`2` jump
  directly; `3` reserved for Month). Switching carries the selected day and,
  if it exists in the other view, the selected event. The agenda reloads
  from that day if it's outside its window.
- The week loads its own 7-day window (`weekLoadedMsg`, stale loads
  dropped) and reloads after a sync that changed something, keeping the
  selected event. Hidden calendars are filtered as in the agenda.
- Core addition: `agenda.WeekStart(day, first)` and `CivilDate.Weekday()`.
  Grouping a week is just `agenda.Build(events, start, 7, loc)`.

## Event links and readable notes — done

**Core** (`internal/providers/ical/links.go`, stored as JSON in
`events.links`, added to old databases by a small column migration in
`store.migrate`). An event's content is untrusted input from whoever sent
the invite, and links arrive inconsistently, so one extractor gathers them:
- Sources: `X-GOOGLE-CONFERENCE`, `X-MICROSOFT-SKYPETEAMSMEETINGURL`, `URL`,
  URLs in `LOCATION`, `<a href>` in HTML descriptions (Google writes HTML
  into `DESCRIPTION` despite the spec; Outlook adds `X-ALT-DESC`), and bare
  URLs in plain text.
- Only http(s). Google's `google.com/url?q=` tracker and Outlook Safe Links
  are unwrapped, so the real destination is what's shown and judged.
  Deduplicated by destination, then ordered video calls → documents →
  other.
- Kinds and labels come from a host table: Meet, Zoom, Teams, Webex,
  Whereby, Jitsi, GoTo, Chime, FaceTime → "Join … meeting". Google
  Docs/Sheets/Slides/Drive, Notion, Confluence/Atlassian, SharePoint,
  OneDrive, Dropbox, GitHub/GitLab, Figma, Miro → documents. An anchor's own
  text becomes the label when it isn't just the URL.
- Notes become readable plain text: HTML converted with `x/net/html` (line
  breaks, `• ` list items, entities; scripts/styles dropped), each link
  written as `label [n]` matching the list, and Google's "Join with Google
  Meet… Please do not edit this section" block removed (its Meet link is
  kept if `X-GOOGLE-CONFERENCE` is missing).
- **Found while testing:** gocal decodes `\\ \; \,` but not `\n`, so any
  multi-line description used to show literal backslash-n.
  `parse.go` now decodes line breaks in summary, location and descriptions.
- Links are part of the sync change-hash, so already-cached events get
  their links on the next sync.

**Event view** (`internal/ui/detail.go`):
- A numbered **Links** list (`▸ 1 ▶ Join Google Meet   meet.google.com`),
  video calls first and selected by default. `j`/`k` or `1`–`9` select;
  `enter` (or `o`) opens in the default browser via `open`/`xdg-open`, with
  the URL as a single argument and no shell. `PgUp`/`PgDn` scroll long
  notes, with the position on the bottom border.
- **Links never open blind:** the selected link's **full address** is
  always shown in a fixed "Opens" strip at the bottom. It's hard-wrapped
  over as many lines as it needs, never truncated, with the host in bold
  (the part that matters for spotting `docs.google.com.evil.example`).
  If the window can't fit the whole address, the strip says so and the
  link won't open until the window is enlarged.
- Only http(s) opens, checked again in the UI and in `openInBrowser`.
- The address is also an OSC 8 hyperlink, so terminals that support it
  (iTerm2, kitty, WezTerm, Ghostty, recent Alacritty) can click it.
- The activity slot confirms with `↗ Opened`; failures show on the status
  line.
- Not done: a `J` "join from anywhere" shortcut (deferred by request).

## Month view (`internal/ui/month.go`, `monthview.go`) — M7 done

- A grid of whole Monday-first weeks covering the month (4–6 rows, from the
  pure `agenda.MonthGrid`), with a muted column of ISO week numbers on the
  left, as on Swedish wall calendars.
- While it's open, the tab shows the month, `[3] November 2026`, and the
  top-right shows the grid's week span, `Weeks 44–49`. Like the week tab,
  the name survives narrow screens because it's in the tab.
- **Cells:** the date (the month too on the 1st and the grid's first cell,
  so leading and trailing days are placeable), then events as `●Title`, with
  the start time in front when the cell is at least 14 wide. Overflow shows
  `+N more`, or `N events` when no event fits at all, shortened to
  `+N`/`N` in narrow cells. Days outside the month and ended events are
  muted. Today's date is accent; the selected day is reversed (underlined
  while another panel has focus).
- **Rows:** a rule between week rows when every row still gets at least
  three lines. Leftover lines go to the weeks whose busiest day has the
  most events, not just the first rows.
- **The month view selects days, not events.** `h`/`l` move a day (`h` on a
  Monday moves into the sidebar, as in the week view), `j`/`k` a week,
  `g`/`G` the first/last of the month, `[`/`]` a month (clamped: 31 Jan →
  28 Feb), `t` today. `enter` opens the selected day in the agenda. Moving
  out of the shown month loads the new one.
- `1`/`2`/`3` switch views (`tab` cycled them at first; it now switches
  pane, see "Pane keys"). Every switch carries the current day (and,
  between Agenda and Week, the selected event).
- Core additions: `CivilDate.AddMonths` (clamping), `CivilDate.DaysInMonth`,
  `agenda.MonthGrid`.
- Under 4 columns per day the view shows a wrapped "too narrow, press 1"
  message.

## Theming (`internal/ui/theme.go`) — done

clical has **no color palette of its own**. Every color is one of the
terminal's 16 ANSI colors, referenced by index, so the app follows the
terminal's theme. Omarchy, which themes apps by retinting those 16 colors,
recolors clical with no per-app config. Unstyled text uses the terminal's
default foreground; nothing sets a background. There is deliberately no
in-app theme picker or color config (the `T` key and YAML themes from the
original spec are dropped). A later, optional override layer could read
Omarchy's `colors.toml`; that's out of scope.

- **One mapping**, in `theme.go`, from semantic roles to ANSI indices:
  accent → 4 (blue): focus, active tab, today, busy. success → 2 (green).
  error → 1 (red). now line → 1 (red, by calendar convention, as its own role).
  warning → 3 (yellow, reserved; unused). Plus attribute styles: bold, muted
  (dim), selected (reverse), inactive selection (underline).
- **Muted is the dim attribute, not ANSI 8.** Bright black is nearly the
  background in some themes (Solarized Dark), and dim stays relative to the
  text color.
- **Measured, not guessed:** WCAG contrast of each role against Tokyo Night,
  Catppuccin Mocha/Latte, Gruvbox dark/light, Solarized dark/light, macOS
  Terminal Basic and xterm. Blue reads as text everywhere (≥3.4:1), so it
  carries everything routine; the first cut used green for focus and yellow
  for "busy", which fail on light themes (yellow 1.7–2.3:1). Green and red
  only color symbols (`✓`, `✗`) or the thin now-line; the words beside them
  are the default color. Errors are bold red.
- **Calendar colors only color bullet glyphs, never text.** Six normal hues
  (blue, magenta, green, cyan, red, then yellow, the weakest, last) × four
  shapes (`●` `◆` `■` `▲`, hollow when hidden) = 24 distinct calendars. Bright
  variants (9–14) aren't used: many themes make them identical to the normal
  ones, and Solarized makes several of them grays. The truecolor hues
  generated past 12 calendars (go-colorful) are gone.
- **Enforced:** `internal/archtest/colors_test.go` fails the build if
  `lipgloss.Color` appears outside `theme.go`, with anything but an index
  0–15, if a hex color literal or RGB literal appears anywhere, or if
  go-colorful is imported. `internal/ui` tests render real escape codes and
  check that only basic ANSI foregrounds are emitted — never 256-color, RGB or
  background — and that under `NO_COLOR` no color is emitted while
  selection, focus and the current day stay visible through bold, reverse
  and underline. That test caught the text input's built-in 256-color
  placeholder gray, now overridden in `inputStyles`.
- **`NO_COLOR`, `TERM=dumb`, limited terminals:** handled by Bubble Tea v2's
  color-profile detection (charmbracelet/colorprofile), which downgrades
  output; the tests above pin the result.

## Many calendars, and managing them in the TUI — done

**Scaling the list** (`internal/ui/sidebar.go`):
- The calendar list is rows of two kinds: account headers (`▾ Personal (3)`)
  and calendars. It scrolls to keep the cursor visible, and the bottom
  border shows the position (`12 of 22`) when the list overflows.
- `enter` on a header folds or unfolds that account. `space` on a header
  shows or hides all its calendars (if any is shown, all are hidden). On a
  calendar row, `space`/`enter` toggle just that one.
- The cursor is keyed by row, not index, so it survives reloads, folding
  and collapsing (falling back to the calendar's account header).

**Permanent colors:**
- `domain.Calendar.ColorSlot` (1-based; 0 = unassigned) replaces the unused
  `Color` hex field. It's stored as decimal text in the existing `color`
  column (`''` = unassigned), so no migration was needed.
- `app` assigns slots: the lowest free one when adding, kept when a feed
  is re-added, freed on removal (and reused first). Calendars from before
  slots existed are backfilled in list order on `app.Open`.
- Before, colors followed list order, so adding a calendar that sorted
  earlier shifted every color below it.
- The UI maps slots to a hue and a bullet shape (see "Theming"): six ANSI
  hues × `●` `◆` `■` `▲` = 24 distinct calendars.

**Adding and removing in the TUI** (`internal/ui/form.go`, `overlay.go`):
- `a` (anywhere outside a popup) opens an "Add calendar" popup over the
  screen: account (prefilled from the sidebar cursor), name, and secret URL.
  The URL is masked with `•`, and pasting works (bracketed paste routes to
  the form). The popup takes every key while open, so typing `q` types `q`.
- `enter` advances fields and submits from the last one. The feed check and
  save run in a command (activity slot: `Checking`); failures show in the
  form with the same advice as the CLI, so the user can fix the URL.
- On success the popup closes, the new calendar is selected in the sidebar
  and synced on its own via the new `app.SyncCalendar`.
- `d` on a calendar row opens a y/N popup; `y` removes it (activity:
  `Removing`) and reloads. A failure shows on the right of the status line.
- `clical` now opens with no calendars at all; the sidebar says
  `No calendars yet. / a add one`.
- `ui.Source` gained `SyncCalendar`, `CheckFeed`, `AddCalendar` and
  `RemoveCalendar`, all implemented by `*app.App`.

Not done (yet): replacing a calendar's URL from the TUI (the CLI has
`calendars set-url`). Renaming is done; see "Nicknames" below.

## Nicknames — done

Calendars and accounts can be given the user's own name, shown everywhere
in place of their own (sidebar, event rows, status line, CLI output).

- Stored separately, in `nickname` columns on `accounts` and `calendars`
  (`''` = none; added to older databases on open), never in place of the
  label or name: a Google account's label is the email address signing in
  again is checked against, an iCal account's ID is derived from its label,
  and a Google calendar's name is refreshed when its account is added again.
  Upserts from providers leave nicknames alone.
- `domain.Account.DisplayName()` / `domain.Calendar.DisplayName()` pick the
  nickname when set; `app.CalendarInfo.Label()` uses them. Lists are sorted
  by the names shown.
- `app.RenameCalendar` / `app.RenameAccount` collapse whitespace and drop
  control characters; an empty name, or the item's own name, removes the
  nickname. No secrets are involved, so renaming works while locked.
- Lookups (`FindCalendar`, `--account`, `FindAccount`, the account typed in
  the add form) match the own name or the nickname. Re-adding an iCal feed
  under a name clears its nickname: naming it is an explicit choice.
- TUI: `n` on a calendar or account row opens a one-field popup with the
  current name (activity: `Saving`); the own name is the placeholder, and
  the popup says that leaving the field empty brings it back.
- CLI: `calendars rename <calendar> [name] [--account]`,
  `accounts rename <account> [name]`; `calendars list` shows
  `Nickname (own name)`.

## Panels (`internal/ui/panel.go`) — done

lazygit-style panels: the calendar list, the main view and the event detail
are rounded boxes with their titles set into the top edge. The bottom key
hints and status line stay unboxed, as in lazygit. Boxing them would cost
two rows and make them compete with the panels.

- **Focus:** the focused panel's border and active title are green and bold;
  the other panel's border is faint.
- **Tabs:** the main panel's title is its view tabs, `[1] Agenda ─ [2] Week`
  (the numbers are the keys that select them). Month joins in M7.
- **Top-right context:** the agenda's loaded span (`2 Oct – 31 Oct 2026`) or
  the shown week's dates (`12 Oct – 18 Oct 2026`; the week number is in the
  tab). The old "Calendars"
  heading and week title rows moved into the borders, so the content
  didn't lose height to the borders.
- **Degrading:** when the top edge is too narrow, the right-hand text goes
  first, then titles are truncated; a collapsed sidebar has no title.
- `panel(width, height, frame{focused, tabs, right, bottom}, content)` is
  the one helper (`bottom` is a bottom-right label, e.g. a list position);
  views render only their inside area.

## Activity indicator (`internal/ui/activity.go`) — done

Pressing `r` always synced, but the only sign was a dim "Syncing…" at the
far right of the footer for a fraction of a second, followed by the same
"Synced HH:MM" as before — easy to miss, and that spot also held errors.
Now the footer's bottom-left corner is a fixed 14-column activity slot, the
one place to look:

- **Busy:** animated spinner + `Syncing` (takes priority) or `Loading`
  (agenda or week reading the cache), bold yellow.
- **Just finished:** `✓ Synced` (green) or `✗ Sync failed` (red) for 3s
  (`flashDuration`), so even a sub-second sync is noticed. Each flash
  carries its own end time, so an old timer can't clear a newer result.
- **Idle:** dim `◦ Idle`.

The right side keeps the last-sync time and failure details. The spinner
(`bubbles/spinner`, MiniDot) ticks only while something is busy:
`Update` wraps the real update and restarts the tick loop when work begins.
A tick arriving with nothing busy ends the loop.

## Settings dialog and event pulse — M12 done

Two narrow pieces, built in this order. Nothing else rides along.

### 1. Settings dialog (`internal/ui/settings.go`)

`S` opens a modal that looks like the help screen (same panel, same
open/close rules: from the main screen and the event view, `esc` or `S`
closes). `S` is free; `,` is the other common convention (preferences) and is
bound as a second key. `s` is already taken by re-sign-in.

- A short list of rows: `[x] Pulse before events`, `Lead time: 5m`.
  `j k` move, `space`/`enter` toggles or edits. Hints on the bottom line.
- Lead time row: `enter` cycles 10m, 5m, 1m, or picks "custom", which opens a
  one-line input in seconds (digits only, 1–3600).
- Every setting is a row of choices with the active one bracketed, stepped
  with enter/`l` and `h`; "custom" asks for a number. Rows: time format, week
  start, sync interval (5m, 10m, 30m, off, custom minutes), pulse on/off,
  pulse lead (10m, 5m, 1m, custom seconds), pulse grace (10m, 5m, 1m, whole
  event, custom minutes). All apply at once and are saved to `settings.yaml`,
  which is also what `clical config` reads.
- Persistence: `config.yaml` is hand-written with comments, so the dialog
  never rewrites it. Changes go to `settings.yaml` beside it (same dir,
  mode 0600, written atomically like the vault), a flat file of the keys the
  dialog owns. Load order: defaults, then `config.yaml`, then `settings.yaml`.
  A missing or bad `settings.yaml` falls back to defaults with a log line, so
  it can't stop startup.
- Seam: `ui.Source` gains `SaveSettings(ui.Prefs) error`; the file I/O lives
  in `internal/config` (`LoadPrefs`, `SavePrefs`). `Update` stays pure.
- Keys listed in the help screen and the footer hint (`S settings`, low
  priority).

### 2. Event pulse (`internal/ui/pulse.go`, one file)

**Decision: ANSI only, no true color.** A smooth fade needs RGB values, and
clical deliberately has none: it names palette indices 0–15 and lets the
terminal's theme define them. Blending would need the theme's actual RGB,
which means querying the terminal (OSC 4/10/11) and re-querying when the user
changes theme, with no reliable signal when it happens. That is the opposite of
"simple and isolated", and a stale blend would break theme-following. So the
pulse steps through attributes instead, which any theme and any terminal
renders correctly.

- **Trigger:** an event that starts within the lead time (default off). The
  lead time is 10m, 5m, 1m or custom seconds. Applies to timed events of
  shown calendars; all-day events never pulse.
- **Look:** the event's line in agenda/week/month cycles through
  normal → bold → bold+reverse → bold → normal over ~1.6 s (4 frames at
  400 ms), in the accent color. The pulse runs from the lead time before the start until a configurable
  grace period after it (10m, 5m, 1m, the whole event, or minutes; default
  1m), never past the event's end, for late arrivals. A start inside the lead
  time pulses at once.
- **Seen from anywhere:** the pulse isn't tied to the event being on screen.
  While it's active, the event's detail view pulses its title, and the footer's
  bottom line shows a pulsing `● Title in 0:30` in every state: other views,
  another event's detail, help, the settings dialog, a filter in progress.
  Events of hidden calendars don't pulse; a filter doesn't affect it.
- **Stopping:** any interaction with that event stops the animation for it,
  and only it: selecting it, or any key pressed while its event view is
  open. Opening the event view doesn't stop it, and a view left open starts
  pulsing when the lead time arrives. (Moving the selection onto it counts; moving past it
  doesn't.) Pressing keys elsewhere doesn't stop it. A stopped event doesn't
  start again, even if the selection later leaves and returns.
- **Timing:** one `pulseMsg` tick, armed only while an event is inside its
  window; no pulse running means no extra ticks. The window check runs on the
  existing clock tick, shortened to 5 s while the feature is on and an event
  is within the next lead time + 5 s.
- **Isolation:** `pulse.go` holds the state (`pulseState`, `pulseMsg`,
  `pulseStyle(frame)`), one `Update` hook, and a single call in the event-line
  renderer: `style = pulseFor(e, style)`. With the setting off, `pulseFor`
  returns its input, so reverting is deleting that call and the file.
- **Tests:** frame selection and window edges as pure functions with a fake
  clock; the pulse shows in the footer while help/settings/another detail is
  open; each way of interacting stops it, an unrelated key doesn't; a golden for one frame; off-by-default test; custom seconds
  validation.

### Done when

`gofmt`, `go vet`, `go test -race ./...` and the four-target cross-compile
are clean; the dialog toggles and persists across restarts in the real binary
(checked with `scripts/tui_screens.py`); README, help and this section updated.

## Write: notes and TODOs — M13 (M13a–c done, M13d deferred)

A note and task surface in two scopes: **event** (one occurrence of one
event) and **global**. Every panel for it is titled "Write". Completed TODOs
are archived to a pool that a later heatmap will read. The feature's
definition lives with the requester; this section is how it is built here,
and where it departs from that definition.

### Decisions

- **Notes belong to a single occurrence**, not to a series. A TODO carries its
  origin when moved to global, so nothing is lost by the choice.
- **Separate from the cache.** The SQLite cache can be rebuilt (a full resync
  deletes events); notes can't. They live in their own files, in a new core
  package `internal/write` (added to `archtest`; knows nothing of the TUI or
  providers).
- **Naming in the UI:** the event's feed text is **Description** (it is
  "Notes" today), the new surface is **Write**.
- **Notes aren't encrypted** (unlike feed URLs and tokens), they're plain
  text so they can be edited and synced. Files are 0600 in a 0700 directory;
  the README says so. `write_dir` in the settings file moves them, e.g. into a
  synced folder.
- **Unarchive and the archive view are deferred** (M13d), but archiving is
  not shipped without a way to get an item back: until M13d, archived items
  stay in `archive.jsonl` and `clical write archive` (M13a) lists them.

### Event identity (`domain.Event.Key`)

`Event.ID` isn't stable enough: for iCal it is `UID/start` for every event
(`providers/ical/parse.go`), so rescheduling an event changes its ID and
would orphan its notes.

- `domain.Event` gains `Key`, set by the provider, stable across a reschedule:
  - iCal: the UID for a one-off event; UID + the occurrence's *original*
    start for a recurring one. A moved single occurrence (RECURRENCE-ID) keeps
    the key of the occurrence it replaced. The provider needs to tell
    one-off from recurring, which the current ID doesn't say.
  - Google: the event or instance ID, which already holds the original start.
- Scoped by calendar: the identity of a note target is `(calendar ID, Key)`.
- Feed values are untrusted: a key is **never** used in a file name, only its
  hash (`sha256`, first 16 hex).
- Each event's file stores a **snapshot** (title, start, calendar name) from
  when notes were last written. It labels origins and orphans after the event
  has gone.
- Orphans (an event's key no longer in the cache, past the sync window or
  deleted) are never deleted. They are listed under "Unattached" in the global
  Write panel, from their snapshot, and can be moved to global or deleted.

### Storage (`internal/write`)

Under `$XDG_DATA_HOME/clical/write/` (or `write_dir`):

    global.md              the global list
    events/<hash>.md       one file per event occurrence, snapshot in a header
    archive.jsonl          archived TODOs, append-only

- Active lists are markdown task lists. A TODO is `- [ ] text` or
  `- [x] text`; any other line is a note. Provenance rides in a trailing
  `<!-- id:k3f9 origin:<scope> last:<key> done:<RFC3339> -->` comment, so a
  file reads cleanly in any editor.
- **Lossless round trip:** a line the parser doesn't understand is kept as a
  note, byte for byte; hand edits survive a load and a save. Tested with
  hand-written files.
- Every item has a short random `id`, so moves keep identity.
- **Origin** is set once. **Last event** is set when an item is moved into an
  event. Both store `(calendar ID, Key)` with the snapshot's title.
- **Completion time** is the latest check, RFC 3339 *with* the UTC offset (a
  heatmap buckets by the local day the user was in); unchecking clears it.
- The archive is JSONL, one record per TODO: text, completed, archived,
  origin, last event, with titles. Plain text, append-friendly, easy for a
  heatmap to read.
- Writes are atomic (temp file + rename, like the vault) and re-check the
  file's mtime first, so an edit made in another editor, or by
  `clical write`, isn't overwritten: the file is re-read and the change
  re-applied, or the user is told.
- Text from files goes through `domain.SafeText` before it's drawn.

### Layers

- `internal/write`: pure model (items, move, check, archive rules) and file
  I/O behind a small `Store`. No I/O in the model functions; table-driven
  tests.
- `internal/app` exposes it; the UI gets its own `WriteSource` interface rather
  than growing `Source`.
- `internal/ui` keeps `Update` pure: it changes the in-memory list at once and
  the file write runs as a command; a failure shows in the status line and
  triggers a reload. A file changed on disk is noticed on the clock tick.
- `cmd/clical` gets `clical write` (below).

### UI

- **Event view:** the event details (with Description and links) and the event
  Write panel side by side; stacked above/below when the terminal is too
  narrow or short. The Write panel is the smaller share, since the link strip
  needs room: the rule that a link whose address doesn't fit can't be opened
  still holds, and the layout must keep that space.
- **Global Write panel:** right of the main pane, collapsible like the
  calendar list (a thin strip when collapsed), still reachable in the event
  view, where it starts collapsed. Narrow terminals start it collapsed.
- **Keys** (all new; `1 2 3` stay views). Keys act within the focused panel:
  `w` focuses the event's Write panel (event view only),
  `W` collapses or expands the global panel. Inside Write: `j k` move,
  `a` add a TODO, `A` add a note (inline input, no popup; `?` and other keys
  are text while typing), `e` edit, `space` check or uncheck, `x` delete
  (asks), `m` move to the other scope (global → the open event only while an
  event is open), `z` archive a checked TODO. A mode label in the panel title
  shows INSERT while typing.
- **Checked TODOs stay in place** (struck through) until archived; no
  reordering.
- **Origin** shows on each TODO that was moved ("from Weekly sync"), from the
  snapshot when the event is gone.
- **Flash:** a collapsed panel whose content changes flashes its border once
  (about 600 ms, reversed). Reuse the flash and frame-tick code from
  `activity.go` and `pulse.go`; a settings-dialog switch ("Panel flash")
  turns it off.
- **Event pulse:** a key pressed in an event's Write panel counts as
  interacting with that event and ends its pulse.
- Help screen and footer hints gain a Write section.

### CLI: `clical write`

    clical write [list] [-e EVENT] [--on DATE] [--all]  # global by default
    clical write add TEXT... [--note] [-e EVENT]        # a TODO, or a note
    clical write done|undo|rm ID                        # check, uncheck, delete
    clical write move ID [-e EVENT]                     # to an event; to global without -e
    clical write archive ID                             # file a checked TODO away
    clical write archived                               # list the archive
    clical write unarchive ID                           # restore, checked, to global
    clical write path                                   # where the files are

`-e` takes part of a title or a key, searched two weeks back and two months
ahead; `--on` narrows it to a day. IDs can be abbreviated.

Works without unlocking the vault (it needs no secrets) and is safe to run
while the TUI is open (mtime check above).

### Milestones

| # | Deliverable |
|---|---|
| M13a (done) | `Event.Key` (iCal and Google), `internal/write`: model, markdown round trip, archive JSONL, `archtest`, `clical write` (list, add, done, archive, unarchive, path). Tested against hand-edited files. |
| M13b (done) | Event Write panel in the event view: layout with the stacked fallback, Description rename, add, edit, check, delete, mode label, help and footer. |
| M13c (done) | Global Write panel: collapse, move in both directions, origin display, Unattached list, collapsed-panel flash and its setting. |
| M13d | Archive view in the TUI and a key to reach it, unarchive from it. Deferred until the rest is in use. |

### As built (M13b, M13c)

- `internal/ui/write.go` (state, commands, keys) and `writeview.go` (layout,
  drawing); the panels read and change the files through a small `WriteSource`
  that `*app.App` implements. `Update` stays pure: keys start commands, and a
  panel shows the list the command read back.
- The event view puts the details and the Write panel side by side from 100
  columns, else Write under the details (a third of the height, 5–12 rows).
  The global panel is a 3-column strip showing "Write" and the number of open
  TODOs; opened it is a quarter of the width, 28–40 columns, and when that
  would leave the main pane under 40 columns (the event view under 60) it
  stays a strip.
- Keys, as planned, with these choices: `w` focuses the event's panel (only in the
  event view; it does nothing elsewhere), `W` opens and focuses the global panel and collapses
  it again; `e`/`enter` edit, `space` checks, `d` deletes (asks), `m` moves,
  `z` archives. Adding stays open for the next item.
- `Z` archives every checked TODO in the focused panel; `tab` cycles the
  panels (list, view, global Write; in the event view: details, the event's
  Write, global Write), skipping a collapsed strip.
- A "Write" setting (`write`, on by default) switches the whole feature off in
  the TUI: no panels, no keys, no reading of the files. The CLI is the files'
  own tool and stays.
- A key pressed in the event's Write panel, being a key in the event view,
  ends that event's pulse.
- Reloads: after every change, and on the clock tick, so edits made with
  `clical write` or in an editor show up; a collapsed global panel whose list
  changed flashes (`panel_flash`, on in the settings dialog).
- Left for M13d: the archive view and unarchive in the TUI; the "Unattached"
  list for notes whose event is gone. Both work from the CLI (`write list
  --all`, `write unarchive`).

### Open

- Whether events outside the sync window (so not in the cache) can still be
  opened for notes: they can't be reached from any view, so for now only
  through their Unattached entries.
- Whether `write_dir` should also move the existing files when changed (no:
  just document it).

## TUI / UX design (M7–M11)

**Layout**: persistent left sidebar + main pane + two-line footer
(lazygit-style).
- Sidebar: tree of accounts (groups) → calendars, one color swatch + toggle
  per calendar to show/hide it in the merged view. Collapsible while focused
  down to a narrow strip of color swatches only.
- Main pane: exactly one of Agenda / Week / Month at a time.
- Footer: top line = context-sensitive keybinding hints; bottom line =
  date/time, sync status ("Synced 12s ago" / spinner), and a banner when a
  calendar's feed is revoked.

**Agenda**: rolling window forward from today (default e.g. 14 days) grouped
under day headers; scrolling past the end extends it; jump-to-date; a subtle
"── now ──" marker among today's events, re-rendered on a UI-only ticker.

**Week**: 7 day-list columns, not an hourly grid — each column is that day's
events in order with a leading time.

**Month**: fixed-width grid with "+N more" overflow (built last). Enter on a
day switches to Agenda focused on that day.

**Event detail**: Enter on an event opens a full-screen popup — title, time
range, location, notes, calendar/account. (The original "open in Google
Calendar web UI" action depends on the feed exposing a URL per event; drop it
if it doesn't.) Esc closes any popup.

**Colors**: calendars get a permanent hue + shape slot (not Google's own
colors, which the iCal feed doesn't carry reliably); everything comes from the
terminal's ANSI palette — see "Theming".

**Time format**: 24-hour default, 12-hour configurable (M9).
**Filter** (done, `internal/ui/filter.go`): `/` opens an input in place of
the footer's hint line. Every view narrows as you type to events whose
title or location contains each word, case-insensitively; hidden calendars
stay hidden. `enter` keeps the filter (shown first in the footer, `esc`
clears it), `esc` while typing clears it, `/` edits it. Deviations from the
first sketch, to make it useful for finding things:
- One filter for all views, kept when switching, like calendar toggles.
- The agenda loads a year ahead while filtering and leaves out days without
  matches; `[`/`]` step between days with matches. No jump on `enter`: the
  selection is already on the first match, and stays on the event found
  when the filter is cleared, so its surroundings show.
- Past events aren't searched (the agenda starts at its first day).
**Mouse**: none in v1 (scroll wheel works via `bubbles/viewport`).
**Help** (done): "?" opens a full-screen list of every key and interaction
(`internal/ui/help.go`), grouped by where it works, in two columns when they
fit and scrollable otherwise. It opens from the main screen and the event
view (popups take `?` as text) and `esc`/`q`/`?` close it back to where it
was. Written out by hand rather than `bubbles/help`, so each entry can say
what a key does in that context.

**Footer hints** (done): every hint has a priority. When the line is too
narrow, the least important are left out, rightmost first among equals,
instead of the line being cut; `? help` is never left out, so the full list
is always one key away.

**Pane keys** (decided 2026-10-02): every pane is reached the same way from
anywhere outside a popup: `0` the calendar list, `1`/`2`/`3` the main view as
Agenda/Week/Month, and `tab` switches between the two panes. Each pane's
number is in its title (`[0] Calendars`, `[1] Agenda`…). Keys inside a pane
only act within it: `h`/`l` never change pane (in Week they now cross into
the adjacent week, like Month). Before this, the only way into the list was
`h`, which in Week/Month only worked from the Monday column, and `tab`
cycled views. Every footer starts with the same pane keys so they're never
cut off. In the list, `space` shows/hides and `enter` only folds (it used to
do both).

**Keymap sketch** (`internal/ui/keymap.go` owns exact bindings; remappable in M9):

| Key(s) | Action |
|---|---|
| `0` | Focus the calendar list (done) |
| `tab` | Switch pane: calendar list ↔ main view (done) |
| `h` / `l` (or Left/Right) | Within a pane: previous/next day (Week, Month); `l` in the list = back (done) |
| `g` / `G` (or Home/End) | First / last event (done) |
| `j` / `k` (or Down/Up) | Move selection in the focused pane |
| `[` / `]` (or PgUp/PgDn) | Previous/next period (Agenda: day before/after the current day — done) |
| `1` / `2` / `3` | Main view as Agenda / Week / Month, focusing it (done) |
| `c` (sidebar focused) | Collapse/expand sidebar |
| `Space` or `Enter` (calendar row) | Toggle calendar visibility |
| `Enter` (revoked calendar row) | Prompt for a new feed URL |
| `Enter` (event, main pane) | Open event detail |
| `t` | Jump to today |
| `/` | Live filter |
| `r` | Sync now |
| `?` | Help |
| `Esc` | Close popup/filter |
| `q` | Quit (closes a popup instead if one is open) |

## Milestones

| # | Deliverable | Representative files | Status |
|---|---|---|---|
| M0 | Domain types + tests, `clical --version` | `internal/domain/` | Done |
| M1 | Vault (generic secrets), account/calendar store | `internal/secrets/vault.go`, `internal/store/` | Done |
| M2 | iCal provider; `calendars add` (hidden URL prompt, probe, save) / `calendars list` | `internal/providers/ical/`, `cmd/clical/calendars.go` | Done |
| M3 | `clical sync` into SQLite via `ApplySync`; `EventsInRange`; plain-text `clical agenda` | `internal/store/events.go`, `internal/engine/`, `internal/agenda/`, `cmd/clical/sync.go`, `cmd/clical/agenda.go` | Done |
| M4 | Feed errors: `CalendarUnavailableError` for 401/403/404/410, backoff on 429/5xx/network, `calendars remove`, `calendars set-url`; confirm-passphrase prompt when creating a new vault | `internal/domain/errors.go`, `internal/providers/ical/provider.go`, `internal/store/calendars.go`, `cmd/clical/calendars.go`, `cmd/clical/prompt.go` | Done |
| M5 | Bubble Tea shell: sidebar + main pane + footer; Agenda; event detail; calendar toggles; golden snapshots | `internal/ui/model.go`, `view.go`, `rows.go`, `keymap.go`, `styles.go`, `cmd/clical/tui.go` | Done |
| M6 | Week view (day-list columns), view switching that keeps the current day | `internal/ui/week.go`, `internal/ui/weekview.go`, `internal/agenda` (`WeekStart`) | Done |
| M7 | Month view (week-number column, "+N more", Enter → Agenda) | `internal/ui/month.go`, `internal/ui/monthview.go`, `internal/agenda` (`MonthGrid`) | Done |
| M8 | "/" filter, "?" help (multi-account merge, toggles, permanent colors and theming already done) | `internal/ui/filter.go`, `internal/ui/help.go` | Done |
| M9 | YAML config for non-color settings (time format, first day of week, poll interval). No colors or themes: theming is delegated to the terminal | `internal/config/` | Done |
| M10 | In-process polling every 5 min, one schedule for all calendars | `internal/ui/model.go` | Done |
| M11 | Revoked-feed flow in the TUI, `clical logs`, cross-compile check | `internal/ui/feedurl.go`, `internal/app/log.go`, `cmd/clical/logs.go` | Done |
| M12 | Settings dialog (`S`), event pulse before start | `internal/ui/settings.go`, `internal/ui/pulse.go` | Done |
| M13 | Write: notes and TODOs in event and global scopes, archive, `clical write` | `internal/write/`, `internal/ui/write*.go`, `cmd/clical/write.go` | Done (M13d, the TUI archive view, is deferred) |

## Focus view and completion heatmap — M14 (done)

A fourth main view on `4`, a dashboard for "what's next" and "what did I do".
The calendar list stays on the left and the global Write panel on the right.
The main panel is split in three:

    ┌ calendars ┐┌ agenda ─┐┌ completed tasks ─────────┐┌ Write ┐
    │           ││ next    ││ of the selected days     ││ global│
    │           │└─────────┘├──────────────────────────┤│       │
    │           │ heatmap: one cell per day, full width││       │
    └───────────┘└────────────────────────────────────┘└───────┘

### Decisions

- **The agenda is the existing agenda, slimmer.** Its state (`rows`, `sel`,
  `offset`, the loaded window, the filter, the pulse) lives on `Model`, and
  `renderAgenda`/`renderRow` already take a width and height. The slim panel
  calls them with a smaller rectangle (the calendar list's width, 26), so it
  moves, opens events (`enter`), filters and pulses exactly like `1`. The one
  change: `agendaHeight()` (scroll clamp) becomes view-dependent. Pressing
  `4` jumps to today, like `t`, so it answers "what's next". The event view
  opened from it returns here.
- **Only archived tasks count**, bucketed by their *completion* day, not the
  archive day. A checked task that isn't archived yet is not counted. The
  bucket is the day in the record's own UTC offset (`Archived.Completed` keeps
  the offset it was checked in), as decided for M13.
- **Heatmap shape:** weeks are columns, and the rows are always Sunday on top
  to Saturday at the bottom, whatever `week_start` says, so the working days
  (where the activity is) sit in the middle rows. The setting still decides
  what a selected "week" is, so a Monday-first week spans two columns. A cell
  is two columns wide; a blank column separates months (no month labels).
  Weekday letters on the left (`S M T W T F S`). Today is the last cell; later
  days are blank. It shows as many recent weeks as fit and scrolls back.
- **Empty days can be selected.** The cursor moves through every day, a year
  and a week back from today (or to the oldest task, if older), so the whole
  year can be walked with the arrow keys.
- **Flat and one hue.** A day without tasks is a dot (`·`); a day with tasks
  is a solid square in the accent color whose size grows with the count: `▪`
  for 1–3, `■` for 4–6, `█` for 7 or more (fixed steps, so a day means the same
  whatever else is in the archive). Size, not shade, carries the intensity, so
  it reads on terminals that ignore faint text and under `NO_COLOR`; faint and
  bold only echo it. No shade glyphs (`░▒▓`, which looked like a shadow), no
  background colors. A selected day keeps its color and shows `▣` (a square
  with a dot) in bold; no inverse video, no underline.
- **Selection** is a range of days, always at least the cursor's day, and
  works like selecting in an OS list:
  - `j`/`k`/↓/↑ move a day (wrapping into the next or previous week),
    `h`/`l`/←/→ move a week. `t` goes to today.
  - A plain move selects the cursor's day alone. Holding `shift` while moving
    (shift+arrows, or `H J K L` where the terminal doesn't report shift)
    extends the selection from where it started. `esc` shrinks it back to the
    cursor.
  - `space` cycles the unit: day → week → month. In week or month, the
    selection snaps to whole weeks or months and moves by that unit. This is
    "what did I do in week 23" in a few keys.
  - There is no "enter": the tasks panel **always shows the selection**.
    `pgup`/`pgdn` scroll it from the heatmap; `tab` moves focus into it for
    `j`/`k`, and `esc` comes back.
- **Tasks panel:** read-only. Days oldest first and tasks within a day by
  completion time, so the newest is last, like a log: each day with tasks has
  one label ("Mon 21 Sep 2026"), then its tasks as time, text, and "from
  Weekly sync" when it has an origin. Days without tasks get no label. A
  selection of several days has a header above ("Week 39 · 21–27 Sep 2026 ·
  6 tasks on 4 days"); a single day has none, as its label names it. Text goes
  through `domain.SafeText`. Empty states: "No tasks completed on Mon 21 Sep
  2026." and, with an empty archive, a hint to press `z` in a Write panel.
  Unarchive from here is out of scope.
- **Narrow terminals:** the calendar list collapses first (and comes back when
  there is room or in another view; a list the user collapsed stays so). If
  the main area is still under 78 columns (the agenda's 26 plus 52 for the
  tasks panel, which holds the view tabs), the three stack: agenda on top,
  tasks, heatmap at the bottom. Short windows drop the tasks, then the
  agenda; the heatmap is always kept, with the tabs. The global Write panel is
  never dropped. Below 40 columns the agenda's rows are compact: a one-column
  indent and the start time only.
- **Focus order** (`tab`): calendar list, agenda, heatmap, tasks, global
  Write, skipping panels that are dropped. `4` focuses the heatmap.

### Where the code goes

- `internal/write`: `CompletionsByDay([]Archived) map[domain.CivilDate][]Archived`
  (pure, table tests with offsets, midnight and DST). The existing
  `Store.Archived()` reads the file.
- `internal/app`: `WriteArchived()`; `ui.WriteSource` gains it.
- `internal/ui`:
  - `heatmap.go`: pure grid math (week columns, month gaps, quartile levels,
    selection and unit arithmetic), unit-tested without a terminal.
  - `focus.go`: view-4 layout (wide and stacked), focus, keys.
  - `focusview.go`: rendering the three panels. `theme.go`: the heat roles.
  - The archive loads in a `tea.Cmd` when the view opens, after `z`/`Z`, and on
    the existing file-change tick. `Update` stays pure.
- `archtest` needs no change: `ui` already imports `write`.

### Steps (a commit each, review in between)

| Step | What |
|---|---|
| M14a (done) | `CompletionsByDay`, `app.WriteArchived`, tests |
| M14b (done) | Heatmap grid, levels, selection and keys, golden snapshots. Also a first `4` view with the heatmap alone, so it can be tried before the other panels exist. A fourth tab costs the week and month views their date span in the top edge below about 110 columns. |
| M14c (done) | Tasks panel above the heatmap, which is now its own panel along the bottom. The view tabs sit in the tasks panel's top edge (in the heatmap's when the window is too short for the tasks panel). `tab` cycles list, heatmap, tasks, global Write. |
| M14d (done) | Slim agenda beside the tasks, reusing the agenda's state and rows with a compact row format (start time only) below 40 columns; wide, collapsed-list and stacked layouts; `tab` order list, agenda, heatmap, tasks; `4` starts the agenda at today. The tasks panel needs 52 columns (the view tabs); below that the list collapses, then the panels stack. |
| M14e (done) | `?` help and footer, this plan's status. The README is left for the author: the view reads Write's archive. |

Verification: the usual checks, golden diffs reviewed, `TestNoColorKeepsStatesVisible`
extended, and the built binary driven with `scripts/tui_screens.py` against a
temp `XDG_DATA_HOME` holding a seeded `archive.jsonl`.

## Verification

- `go build ./...`, `go vet ./...`, `go test ./...` clean after every
  milestone; `gofmt -l .` empty.
- Unit tests per package (domain, vault, store, iCal parse/sync, engine,
  agenda, CLI helpers); UI golden snapshots of `View()` plus Update-driven
  behaviour tests.
- Drive the real binary for anything touching prompts or the TUI, with an
  isolated `XDG_DATA_HOME` and Google's public holidays feed. Prompts:
  `expect` (macOS `script` sends EOF before line-based reads). TUI screens:
  `scripts/tui_screens.py`, which replays the output through the `pyte`
  terminal emulator — grepping raw output misses most redraws, since Bubble
  Tea only sends changed cells.
- Manual check of each CLI milestone against real feeds (your personal and
  work calendars) before building UI on top.
- Cross-compile: `GOOS={darwin,linux} GOARCH={arm64,amd64} go build ./...`
  (last passed at M7).
