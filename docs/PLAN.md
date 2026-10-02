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
provider covers them. Google OAuth was the original plan; it's shelved (see
Decisions) and kept only as a fallback.

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
| M8–M11 | Search and help, config (non-color), background polling, polish | Not started |

Deferred (2026-09-29): the **work** (Google Workspace) calendar. Focus is the
personal calendar for now. Whether the work calendar exposes a secret iCal
address (admins can disable it) decides whether the shelved OAuth path is
revived or deleted — see "Google OAuth (shelved)".

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
    Provider string // "ical" (or "google" for the shelved path)
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

## Google OAuth (shelved) — `internal/providers/google`

The loopback OAuth flow from the original plan (`oauth.go`: local redirect
server, code exchange, `openid`+`email` scopes to read the account email,
token saved to the vault). It no longer implements `CalendarProvider`. CLI:
`clical auth add/list`, hidden from help. Revive only if a Workspace calendar
has no secret iCal address; otherwise delete it. Its Google Cloud setup steps
were in the README as of commit `4ab3788`.

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
only exemption is the shelved `cmd/clical/auth.go`.

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
- Calendars are referenced by ID or case-insensitive name; an ambiguous name
  lists the matches and asks for the ID or `--account`.
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
- `auth add/list`: shelved OAuth, hidden from help.

## Background polling — M10 (planned)

Per-calendar polling needs a per-calendar sync on the seam, so M10 adds
`app.SyncCalendar(ctx, CalendarInfo) SyncOutcome` (wrapping
`engine.SyncOne`) rather than letting the UI reach the engine. Sketch:

```go
type pollMsg struct{ CalendarID string }                // ui-internal
func pollCmd(cal app.CalendarInfo, after time.Duration) tea.Cmd
func syncCalendarCmd(src Source, cal app.CalendarInfo) tea.Cmd // → syncDoneMsg-like result
func InitialDelay(calendarID string, interval time.Duration) time.Duration // FNV-hash phase offset (app or ui)
```

Since every iCal poll is a full download, the default poll interval for
iCal calendars should be longer than the original 60s — **5 minutes**,
configurable. `Update`: `TickMsg` → `tea.Batch(SyncCmd, PollCmd)`;
`SyncCompletedMsg` → reload the visible range from the store (skip if
nothing changed); `CalendarUnavailableMsg` → flag that calendar in the sidebar and
footer; Enter on it prompts for a new URL — no restart, other calendars keep
syncing.

## TUI (`internal/ui`) — M5 done

`clical` with no command opens it (kong default command `open`). The vault is
unlocked with a plain prompt *before* the TUI takes the screen; the passphrase
then lives in memory for the session. No calendars → a hint instead of an
empty screen. One sync runs at startup, `r` syncs again; the footer shows
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
- Keys: `h`/`l` move between days (`h` on Monday moves focus to the
  sidebar; `l` on Sunday stays), `j`/`k`/`g`/`G` within a day, `[`/`]` a
  week back/forward keeping the weekday, `t` this week on today's next
  event, `enter` details.
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
- `tab` now cycles Agenda → Week → Month; `1`/`2`/`3` jump directly. Every
  switch carries the current day (and, between Agenda and Week, the
  selected event).
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

Not done (yet): rename, or replacing a calendar's URL from the TUI (the CLI
has `calendars set-url`).

## Panels (`internal/ui/panel.go`) — done

lazygit-style panels: the calendar list, the main view and the event detail
are rounded boxes with their titles set into the top edge. The bottom key
hints and status line stay unboxed, as in lazygit. Boxing them would cost
two rows and make them compete with the panels.

- **Focus:** the focused panel's border and active title are green and bold;
  the other panel's border is faint.
- **Tabs:** the main panel's title is its view tabs, `[1] Agenda ─ [2] Week`
  (the numbers are the jump keys; `tab` cycles). Month joins in M7.
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
**Search**: "/" live-filters the current view by title/location; Enter jumps
to the first match, Esc clears.
**Mouse**: none in v1 (scroll wheel works via `bubbles/viewport`).
**Help**: "?" opens a full-screen modal of all bindings (`bubbles/help`).

**Keymap sketch** (`internal/ui/keymap.go` owns exact bindings; remappable in M9):

| Key(s) | Action |
|---|---|
| `h` / `l` (or Left/Right) | Focus sidebar / main pane (done) |
| `g` / `G` (or Home/End) | First / last event (done) |
| `j` / `k` (or Down/Up) | Move selection in the focused pane |
| `[` / `]` (or PgUp/PgDn) | Previous/next period (Agenda: day before/after the current day — done) |
| `Tab` | Cycle Agenda → Week → Month (done) |
| `1` / `2` / `3` | Jump to Agenda / Week / Month (done) |
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
| M8 | "/" filter, "?" help (multi-account merge, toggles, permanent colors and theming already done) | `internal/ui/search.go`, `internal/ui/help.go` | |
| M9 | YAML config for non-color settings (time format, first day of week, poll interval), mergo defaults, fsnotify hot-reload. No colors or themes: theming is delegated to the terminal | `internal/config/` | |
| M10 | In-process polling (default 5 min for iCal), jittered per calendar | `internal/engine/scheduler.go`, `internal/engine/jitter.go` | |
| M11 | Revoked-feed flow in the TUI, `--logs` tail, cross-compile check | `internal/ui/revoked.go`, `cmd/clical/logs.go` | |

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
