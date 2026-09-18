# clical — Phase 1 (MVP) design & implementation plan

This is the living design doc for Phase 1. It's a mirror of a Claude Code plan
file (`~/.claude/plans/steady-crunching-willow.md` at time of writing) copied
into the repo so it survives independently of any particular chat session —
update this file, not just the chat, when decisions change.

## Context

`clical` is a terminal calendar app (Go, Bubble Tea/Elm architecture),
read-only against Google Calendar in v1, architected so CalDAV/Microsoft
Graph can drop in later behind a `CalendarProvider` interface. Phase 1 is:
OAuth → encrypted token storage → incremental sync into SQLite →
agenda/week/month TUI views → multi-account merge → config/themes →
in-process background polling.

## Status (update this as milestones land)

- **M0** (domain skeleton) — done.
- **M1** (OAuth loopback, vault, accounts store) — code done; end-to-end
  `auth add` against a real Google account not yet confirmed.
- **M2–M11** — not started. See Milestones below.

## Decisions made during planning (deviations/clarifications from the original spec)

- **Vault KDF simplified**: no separate argon2id layer. age's passphrase mode
  (`age.NewScryptRecipient`/`ScryptIdentity`) already does audited
  scrypt-based key derivation — layering argon2id on top added complexity
  with no security benefit, and this way the encrypted files stay openable
  with the standard `age` CLI for manual recovery. `golang.org/x/crypto/argon2`
  is not a dependency.
- **`Account.ID`** = a stable hash of `provider:label` (e.g.
  `sha256("google:c-adrougge@gmail.com")[:16]`, hex-encoded). Deterministic,
  so re-running `auth add` for an account that's already added is detected
  instead of silently creating a duplicate — but the vault filename and logs
  don't expose the raw email.
- **Passphrase caching scope** = one long-running TUI process only.
  Short-lived CLI subcommands (`auth add`, `sync`) are separate processes and
  re-prompt each time. No ssh-agent-style daemon for Phase 1.
- **Cancelled recurring instances** (Google returns `status: "cancelled"`
  items, not a separate deletions list) are translated straight into
  `SyncResult.DeletedIDs` — they disappear from the local view like any other
  deletion. No distinct "declined/cancelled but visible" state in Phase 1.
- **BYO-OAuth onboarding** is README documentation only for Phase 1 (see
  README's "Setting up your Google OAuth client"). No in-app setup wizard.
- **`SyncResult` gains one field**, `FullResync bool`, versus the originally
  reviewed struct — represents the Google `410 Gone` case (full resync;
  deletions aren't enumerable) distinctly from a normal incremental delta.
- **OAuth scopes requested are `calendar.readonly` + `openid` + `email`**, not
  just `calendar.readonly` as the very first spec draft implied. The extra
  two scopes are used once, right after token exchange, to call Google's
  userinfo endpoint and populate `Account.Label` with the real email address
  — `CalendarProvider.Authenticate` has no other way to learn it, since the
  interface takes no label input. (`internal/providers/google/oauth.go`,
  `fetchEmail`.)
- **Dependency stack note**: `github.com/samber/lo` is fine to reach for on
  slice map/filter/groupBy operations over events; `github.com/samber/mo`
  (Option/Result monads) was explicitly rejected in review as fighting
  idiomatic Go error handling for no clear benefit — don't add it.

## Domain model (`internal/domain`)

```go
type Account struct {
    ID       string // sha256("provider:label")[:16], hex
    Provider string // "google"
    Label    string // e.g. the email address
}

type Calendar struct {
    ID, AccountID, Name, Color string // Color is hex
}

// CivilDate: timezone-less date, used for AllDay events specifically to avoid
// the UTC-midnight date-shift bug. Never represent all-day events as time.Time.
type CivilDate struct{ Year, Month, Day int }
func NewCivilDate(y, m, d int) CivilDate
func CivilDateFromTime(t time.Time) CivilDate
func (d CivilDate) Before(o CivilDate) bool
func (d CivilDate) AddDays(n int) CivilDate
func (d CivilDate) String() string // "2026-09-09"

// Event: unexported time fields + constructors + ok-returning accessors, so
// "which field pair is populated" can't be gotten wrong by a caller.
type Event struct {
    ID, CalendarID, Title, Location, Notes string
    AllDay                                 bool
    start, end                             time.Time // unexported, UTC
    startDate, endDate                     CivilDate // unexported
}
func NewTimedEvent(id, calID, title string, start, end time.Time, loc, notes string) Event
func NewAllDayEvent(id, calID, title string, start, end CivilDate, loc, notes string) Event
func (e Event) TimeRange() (start, end time.Time, ok bool)
func (e Event) DateRange() (start, end CivilDate, ok bool)

type CalendarProvider interface {
    Authenticate(ctx context.Context) (Account, error)
    ListCalendars(ctx context.Context, acct Account) ([]Calendar, error)
    SyncEvents(ctx context.Context, cal Calendar, token string) (SyncResult, error)
}

type SyncResult struct {
    Upserted   []Event
    DeletedIDs []string
    NextToken  string
    FullResync bool // true on a post-410 full resync: replace-all, not a partial delta
}
```

`internal/domain/errors.go` closes the re-auth gap without changing the
3-method interface:

```go
type ReauthRequiredError struct {
    AccountID string
    Err       error
}
func (e *ReauthRequiredError) Error() string { return fmt.Sprintf("account %s needs re-authentication: %v", e.AccountID, e.Err) }
func (e *ReauthRequiredError) Unwrap() error  { return e.Err }
```

## Google provider (`internal/providers/google`)

- `oauth.go` (done): loopback OAuth flow — local redirect server, browser
  launch, code exchange, `fetchEmail` via the userinfo endpoint for the
  account label, token handed straight to the vault (never exposed through
  the `CalendarProvider` interface).
- `tokensource.go` (not yet written): wraps `oauth2.TokenSource`. On
  `Token()` failure, checks `errors.As(err, &oauth2.RetrieveError{})` for
  `ErrorCode == "invalid_grant"` and returns `*domain.ReauthRequiredError`
  instead — this propagates up through `googleapi` calls via normal error
  wrapping, so `ListCalendars`/`SyncEvents` just need one `errors.As` check
  each. On successful refresh, persists the new token via the vault
  (`SaveToken`).
- `calendars.go` (M2): `ListCalendars` loops on `resp.NextPageToken`
  internally — pagination never reaches the `CalendarProvider` interface.
- `events.go` (M3): `SyncEvents` loops pages the same way; `Events.List` uses
  `SingleEvents=true`, `SyncToken` when present else `TimeMin/TimeMax`; items
  with `Status == "cancelled"` go to `DeletedIDs`, everything else to
  `Upserted`; `NextSyncToken` is only read off the *last* page. A `410 Gone`
  triggers a full resync (`FullResync: true`, no `DeletedIDs`).
- `backoff.go` (M4): wraps calls needing retry (403 `rateLimitExceeded`, 429)
  with `cenkalti/backoff/v4`, jittered exponential backoff.

## Secrets vault (`internal/secrets`) — done

Disk layout under `xdg.DataHome/clical/vault/` (dir `0700`):
```
vault/<accountID>.age  # one age-encrypted blob per account (marshaled oauth2.Token)
```
One file per account — refreshing account A's token only rewrites `A.age`;
adding account B never touches A's file or requires re-entering anything
beyond the existing cached passphrase.

```go
type Vault struct { /* dir string; recipient/identity from age.NewScryptRecipient/Identity */ }
func Open(dir string, prompt func() (string, error)) (*Vault, error) // ErrWrongPassphrase on mismatch against an existing account file
func (v *Vault) SaveToken(accountID string, tok *oauth2.Token) error
func (v *Vault) LoadToken(accountID string) (*oauth2.Token, error)
func (v *Vault) DeleteToken(accountID string) error
```

## Local cache (`internal/store`) — SQLite schema (accounts table done; calendars/events tables created but unused until M2/M3)

```sql
CREATE TABLE accounts (
    id TEXT PRIMARY KEY, provider TEXT NOT NULL, label TEXT NOT NULL
);
CREATE TABLE calendars (
    id TEXT NOT NULL, account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL, color TEXT NOT NULL,
    sync_token TEXT, last_synced_at INTEGER,
    PRIMARY KEY (account_id, id)
);
CREATE TABLE events (
    id TEXT NOT NULL, calendar_id TEXT NOT NULL, account_id TEXT NOT NULL,
    title TEXT NOT NULL, all_day INTEGER NOT NULL,
    start_utc INTEGER, end_utc INTEGER,        -- NULL if all_day
    start_date TEXT, end_date TEXT,            -- "YYYY-MM-DD", NULL if !all_day
    location TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (account_id, calendar_id, id),
    FOREIGN KEY (account_id, calendar_id) REFERENCES calendars(account_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_events_range       ON events (calendar_id, start_utc, end_utc);
CREATE INDEX idx_events_alldayrange ON events (calendar_id, start_date, end_date);
```

DSN: `file:<path>?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)`
(modernc.org/sqlite's DSN pragma syntax). All writes go through
`Store.writeMu sync.Mutex` + a single transaction per sync result, so the
delta and the sync-token update commit atomically:

```go
func (s *Store) ApplySync(ctx context.Context, cal domain.Calendar, r domain.SyncResult) error {
    s.writeMu.Lock(); defer s.writeMu.Unlock()
    return s.withTx(ctx, func(tx *sql.Tx) error {
        if r.FullResync {
            /* DELETE FROM events WHERE account_id=? AND calendar_id=? */
        } else {
            /* DELETE ... for each r.DeletedIDs */
        }
        /* INSERT OR REPLACE ... for each r.Upserted */
        /* UPDATE calendars SET sync_token=?, last_synced_at=? WHERE ... */
        return nil
    })
}
```
(`ApplySync` itself is not yet written — lands with M3.)

## Sync engine ↔ UI message flow (`internal/sync`, `internal/ui`) — not yet written (M5, M10)

```go
type TickMsg struct{ CalendarID string }
type SyncCompletedMsg struct{ CalendarID string; Upserted, Deleted int }
type SyncErrorMsg struct{ CalendarID string; Err error }
type ReauthRequiredMsg struct{ AccountID string }

func PollCmd(cal domain.Calendar, after time.Duration) tea.Cmd
func SyncCmd(e *Engine, acct domain.Account, cal domain.Calendar) tea.Cmd // runs SyncOne, maps errors.As(*ReauthRequiredError) -> ReauthRequiredMsg
```

Per-calendar jitter is a deterministic phase offset (FNV hash of calendar ID
→ fraction of the interval), so restarts don't re-randomize and multiple
calendars don't all poll in lockstep against a no-batch-API backend:

```go
func InitialDelay(calendarID string, interval time.Duration) time.Duration
```

`Update` handling: `TickMsg` → `tea.Batch(SyncCmd(...), PollCmd(...))`
(re-arm); `SyncCompletedMsg` → issue a store-read `Cmd` to refresh the
visible range; `ReauthRequiredMsg` → flag that account degraded, show a
banner, a keypress triggers `AuthenticateCmd` (re-runs the loopback flow for
just that account) — no app restart, and other accounts keep syncing
normally.

## TUI / UX design (M5–M11) — not yet written

Decided in a design conversation ahead of writing any `internal/ui` code, so
M5–M8 have a concrete target rather than just message types and file names.

**Layout**: persistent left sidebar + main pane + two-line footer
(lazygit-style).
- Sidebar: tree of accounts → their calendars, one color swatch + toggle per
  calendar to show/hide it in the merged view. Collapsible while focused
  down to a narrow strip showing only the color swatches — the legend stays
  visible even collapsed, just without labels.
- Main pane: exactly one of Agenda / Week / Month visible at a time (no
  persistent mini-calendar split — kept simple, matches the
  agenda→week→month build order).
- Footer: top line = context-sensitive keybinding hints (changes with focus,
  like lazygit); bottom line = current date/time, sync status ("Synced 12s
  ago" / spinner while syncing), and the re-auth banner when an account is
  degraded.

**Agenda view**: rolling window forward from today (default configurable,
e.g. 14 days), events grouped under day headers; scrolling past the end
extends the window via a further store query. A jump-to-date action seeks
directly to an arbitrary day. A subtle "── now ──" marker is inserted at the
correct position among today's events, re-rendered on a lightweight UI-only
ticker (no network).

**Week view**: 7 day-list columns, *not* an hourly time-grid — each column is
that day's events in chronological order with a leading time. Avoids
positional/overlap-layout math entirely; better suited to narrow terminal
columns than a graphical hour-grid would be.

**Month view**: the fixed-width grid with "+N more" overflow (the hard piece,
built last). Selecting a day and pressing Enter switches to Agenda focused on
that day — month is primarily a navigation aid into Agenda, not a place to
read full event details.

**Event detail**: Enter on a selected event (in any view) opens a
full-screen detail popup — title, time range, location, notes,
calendar/account — plus a keybind to open the event in Google Calendar's web
UI. Esc closes any popup (detail, search, help) uniformly.

**Colors**: calendar colors are auto-assigned from the active theme's palette
(via go-colorful for contrast), not pulled from each calendar's own Google
color — deliberately trading "matches your existing Google Calendar colors"
for guaranteed cross-theme contrast/consistency.

**Time format**: 24-hour by default; configurable to 12-hour in YAML (M9).

**Search**: "/" starts a live filter of the *current* view's visible events
by title/location match-as-you-type; Enter jumps to the first match, Esc
clears.

**Mouse**: none in v1 — keyboard-only, though scroll-wheel scrolling still
works for free via `bubbles/viewport` with no explicit handling.

**Help**: "?" opens a full-screen modal listing every keybinding grouped by
context (`bubbles/help` in full mode).

**Keymap sketch** (illustrative; `internal/ui/keymap.go` owns the exact
bindings, all remappable later via YAML in M9):

| Key(s) | Action |
|---|---|
| `h` / `l` (or Left/Right) | Move focus to sidebar / back to main pane |
| `j` / `k` (or Down/Up) | Move selection within the focused pane |
| `[` / `]` (or PgUp/PgDn) | Previous/next period (day in Agenda, week in Week, month in Month) |
| `Tab` | Cycle main view: Agenda → Week → Month → Agenda |
| `1` / `2` / `3` | Jump directly to Agenda / Week / Month |
| `c` (sidebar focused) | Collapse/expand the sidebar |
| `Space` or `Enter` (calendar row focused) | Toggle that calendar's visibility |
| `Enter` (degraded account row focused) | Trigger re-auth for that account |
| `Enter` (event focused, main pane) | Open event detail popup |
| `t` | Jump to today |
| `T` | Cycle theme (moved off `t`, which the original spec had, since "jump to today" is far more frequent) |
| `/` | Live filter current view |
| `r` | Manual sync refresh (superseded by automatic polling at M10, kept as a manual override) |
| `?` | Full keybinding help |
| `Esc` | Close popup/filter |
| `q` | Quit (top level only — closes a popup instead if one is open) |

## Milestones (each independently runnable/demoable)

| # | Deliverable | Representative files | Status |
|---|---|---|---|
| M0 | Domain skeleton + unit tests (CivilDate arithmetic, Event constructors), `clical --version` via kong | `internal/domain/domain.go`, `cmd/clical/main.go` | **Done** |
| M1 | `clical auth add` runs OAuth loopback, writes token to vault, writes account row; `clical auth list` decrypts + prints | `internal/providers/google/oauth.go`, `internal/secrets/vault.go`, `internal/store/accounts.go` | Code done; live end-to-end check pending |
| M2 | `clical calendars list` — `CalendarList.List` + pagination, persisted | `internal/providers/google/calendars.go`, `internal/store/calendars.go` | Not started |
| M3 | `clical sync --debug` — incremental `Events.List` sync into SQLite, sync token persisted atomically | `internal/providers/google/events.go`, `internal/sync/engine.go`, `internal/store/events.go` | Not started |
| M4 | Re-auth + backoff exercised headless (fake provider returning `ReauthRequiredError`; 403/429 retried) | `internal/providers/google/tokensource.go`, `internal/providers/google/backoff.go`, `internal/sync/engine_test.go` | Not started |
| M5 | Bubble Tea shell: sidebar (collapsible) + main pane + two-line footer; Agenda view (rolling window, day headers, now-marker) over cached data; manual `r` refresh; event detail popup; first `teatest` golden tests | `internal/ui/app.go`, `internal/ui/sidebar.go`, `internal/ui/agenda.go`, `internal/ui/detail.go`, `internal/ui/keymap.go`, `internal/ui/footer.go` | Not started |
| M6 | Week view: 7 day-list columns (no time-grid), reusing Agenda's per-event line rendering | `internal/ui/week.go` | Not started |
| M7 | Month view (fixed-width grid, "+N more" overflow — hardest UI piece); Enter drills into Agenda for the selected day; shared grapheme-width/truncation helper (`uniseg`) reused by Week/Agenda's line rendering | `internal/ui/month.go`, `internal/ui/textwidth.go` | Not started |
| M8 | Multi-account merge; per-calendar colors auto-assigned from the active theme's palette; sidebar calendar toggles; "/" live filter; "?" help modal | `internal/theme/resolve.go` (go-colorful contrast/assignment), `internal/ui/search.go`, `internal/ui/help.go` | Not started |
| M9 | YAML config + themes, mergo defaults, fsnotify hot-reload (config/theme only — never vault/account list) | `internal/config/config.go`, `internal/theme/theme.go` | Not started |
| M10 | In-process background polling replaces M5's manual refresh, jittered per calendar | `internal/sync/scheduler.go`, `internal/sync/jitter.go` | Not started |
| M11 | In-TUI re-auth flow (degraded account shown in sidebar + footer banner, Enter on that row re-runs the loopback flow for just that account), `--logs` tail, cross-compile check | `internal/ui/reauth.go`, `cmd/clical/logs.go` | Not started |

## Verification

- `go build ./...` and `go vet ./...` clean after every milestone.
- `go test ./...`: unit tests for `domain` (CivilDate, Event accessors),
  `store` (ApplySync atomicity, FullResync replace-all path), `sync` (jitter
  determinism, reauth error mapping), and `teatest` golden-file snapshots for
  each UI view starting at M5.
- Manual end-to-end check at M1–M4 via the CLI subcommands against a real
  Google account (with a self-registered Desktop OAuth client) before any UI
  code exists, so provider/auth/sync issues surface before UI work masks
  them.
- Cross-compile check (already done once, during M0/M1 — re-run after
  significant changes, definitely before M11 wraps up):
  `GOOS=darwin GOARCH=arm64 go build ./...` and the same for darwin/amd64,
  linux/amd64, linux/arm64.
