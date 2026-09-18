# clical

Terminal calendar app for macOS and Arch Linux, read-only against Google Calendar in v1.
Elm-architecture TUI (Bubble Tea), pure-Go SQLite cache, age-encrypted OAuth tokens.
Architected so other providers (CalDAV, Microsoft Graph) drop in later behind
`internal/domain.CalendarProvider`.

## Locked decisions

- Go 1.25+, single static binary, darwin+linux only (never Windows).
- Elm architecture: immutable model, pure `Update`, all I/O as `tea.Cmd`.
- Background sync is in-process polling (`tea.Tick`) while the TUI is running —
  not a system daemon/service.
- Secrets: OAuth tokens in an age-encrypted file (one per account), keyed by a
  passphrase prompted at launch and held in memory for the session. Uses age's
  native scrypt-based passphrase KDF directly — no OS keychain, no separate
  argon2 layer (redundant on top of age's own KDF).
- Config: YAML, hot-reloaded via fsnotify. Themes are YAML too, embedded defaults
  plus user overrides in `~/.config/clical/themes/`.
- All-day events are stored as a civil date (year/month/day, no timezone), not
  as a UTC `time.Time`, to avoid the classic date-shift bug.
- OAuth: users bring their own Google Cloud OAuth client (Desktop type) and
  supply client ID/secret via config, matching `gcalcli`'s approach. This avoids
  Google's sensitive-scope verification process; see "Setting up your Google
  OAuth client" below.

## Setting up your Google OAuth client

`clical` doesn't ship a shared Google API client — every user creates their own,
free, in a couple of minutes:

1. Go to the [Google Cloud Console](https://console.cloud.google.com/), create
   a project (or reuse one), and enable the **Google Calendar API** for it.
2. Under **APIs & Services → OAuth consent screen**, configure it for
   **External** users and add your own Google account as a test user. You can
   leave the app in "Testing" mode — Phase 1 only ever needs one or a handful
   of accounts.
3. Under **APIs & Services → Credentials**, create an **OAuth client ID** of
   type **Desktop app**. Note the generated client ID and client secret.
4. Run `clical auth add --client-id=<id> --client-secret=<secret>` and follow
   the printed URL to grant access.

Note: while the consent screen is in "Testing" mode, Google expires refresh
tokens after 7 days, so you'll need to re-run `auth add` periodically. Moving
the consent screen to "Production" removes this limit but is not required
otherwise.

## Layout

    cmd/clical/                  entry point, CLI wiring (kong)
    internal/domain/             provider-agnostic models + CalendarProvider interface
    internal/providers/google/   Google Calendar API implementation
    internal/store/              SQLite cache + sync-token persistence
    internal/sync/               scheduler, delta application, backoff
    internal/secrets/            age-encrypted token vault + argon2id passphrase
    internal/config/             YAML load/merge/watch
    internal/theme/              theme structs, embedded defaults, resolver
    internal/ui/                 Bubble Tea models: app, agenda, week, month; keymap; help

## Status

See [`docs/PLAN.md`](docs/PLAN.md) for the full design/implementation plan —
domain model, provider/vault/store design, TUI/UX decisions (layout, views,
keymap), and a milestone-by-milestone status table. Keep that file, not just
chat history, up to date as decisions change — it's what makes this project
resumable in a fresh session.

Short version: M0 (domain types) done; M1 (OAuth loopback, vault, account
store) code done, live end-to-end check pending. Everything else — calendar
listing, event sync, the TUI, config/themes, background polling — not started.
