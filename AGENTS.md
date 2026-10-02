# Notes for agents working on clical

clical is a read-only terminal calendar (Go, Bubble Tea v2) that syncs
calendars from secret iCal feed URLs.

## Read first

- `docs/ARCHITECTURE.md`: the layers, and **where new code goes**.
- `docs/PLAN.md`: design decisions with reasons, and milestone status.
  Update its status table when a milestone lands.

## The boundary, in one paragraph

Calendar logic lives in the core (`internal/domain`, `agenda`, `store`,
`secrets`, `engine`, `providers/*`). Front-ends (`cmd/clical`, `internal/ui`)
only gather input and present output. They reach the core **only through
`internal/app`**, plus the plain types in `domain` and `agenda`. Terminal and
UI libraries are for front-ends only. `go test ./internal/archtest` enforces
all of this. If it fails, fix the import, or change the layer table *and*
`docs/ARCHITECTURE.md` together. Don't add exemptions to get past it.

## Rules that aren't obvious from the code

- **Feed URLs are credentials.** They live only in the vault: never in the
  DB, config, logs, errors or command-line flags. `ical.redact` strips them
  from HTTP errors. Read them from hidden prompts.
- **All-day events are `domain.CivilDate`, never `time.Time`.** End dates are
  exclusive.
- **`ui.Update` must stay pure:** no network, disk or clock reads. I/O goes
  through `ui.Source` in a `tea.Cmd`, and time arrives as `tickMsg`.
- **No Windows support:** don't add build targets or code paths for it.

## Checks before calling something done

    gofmt -l .                     # must print nothing
    go vet ./...
    go test ./...                  # includes internal/archtest
    go test ./internal/ui -update  # only when a screen change is intended; review the golden diff
    for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null ./...; done

For anything involving prompts or the TUI, also run the built binary under
`expect` with `XDG_DATA_HOME` pointing at a temp directory, so the user's real
vault is never touched. Google's public US-holidays feed is a safe test URL:
`https://calendar.google.com/calendar/ical/en.usa%23holiday%40group.v.calendar.google.com/public/basic.ics`

## Commits

- Conventional Commits (Commitizen), e.g. `feat: …`, `fix: …`, `docs: …`.
- The user is the sole author. **No AI attribution of any kind:** no
  `Co-Authored-By` naming an assistant, no "Generated with", no trailers.
  End the message at its last line of substance.
- Only commit when asked.
