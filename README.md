# CLIcal

A read-only terminal calendar for macOS and Linux. It merges iCal feeds and
Google accounts into one agenda, week and month view.

## Install

Download the archive for your system from the
[latest release](https://github.com/miphe/clical/releases), unpack it, and put
`clical` somewhere on your `PATH`:

```bash
tar -xzf clical_*_darwin_arm64.tar.gz   # or linux_amd64, and so on
sudo mv clical /usr/local/bin/
```

The binary isn't signed by Apple yet, so if macOS refuses to open it (it was
downloaded in a browser), run
`xattr -d com.apple.quarantine /usr/local/bin/clical` once.

## Use

```bash
clical
```

On the first run, you choose a passphrase that protects your calendar
addresses. Press `a` to add a calendar, and `?` to see all keys.

- **iCal link:** in Google Calendar, open **Settings**, choose the calendar,
  and copy **Secret address in iCal format** from **Integrate calendar**.
  Treat it like a password.
- **Google account:** you need your own Google OAuth client (a few minutes in
  the Google Cloud Console), which you enter once in CLIcal. Follow
  [`docs/GOOGLE.md`](docs/GOOGLE.md). Only this is sure to show who is invited
  and who has replied; an iCal link shows guests only if the feed has them.

**Your data:** feed addresses, the OAuth client and sign-in tokens are kept
only in an encrypted vault on your machine. CLIcal talks only to your calendar
feeds and Google, and to no other server. It does keep a local copy of your
events (a SQLite file) and your notes (plain files) in its data directory,
`~/Library/Application Support/clical` on macOS and `~/.local/share/clical` on
Linux, readable only by you but not encrypted.

## Development

You need Go 1.27 or newer.

```bash
git clone https://github.com/miphe/clical.git
cd clical
go run ./cmd/clical        # run from source
go install ./cmd/clical    # or install the binary into $(go env GOPATH)/bin
go test ./...              # run the tests
go run ./cmd/clical --mock # try it on made-up data; nothing real is read or saved
```

Google accounts and iCal links work the same as for the binary, see above.

To keep your real calendars untouched while developing, point
`XDG_DATA_HOME` at a temporary directory, for example
`XDG_DATA_HOME=$(mktemp -d) go run ./cmd/clical`.

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the code layout and
[`docs/PLAN.md`](docs/PLAN.md) for design decisions.

## License

CLIcal is free to use, at home and at work, including when your employer is a
company. You may fork it and change it. You may not sell it, charge for it, or
make money from it in any other way. The terms are in
[`LICENSE.md`](LICENSE.md) (PolyForm Noncommercial 1.0.0) and
[`ADDITIONAL-PERMISSION.md`](ADDITIONAL-PERMISSION.md).
