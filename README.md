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

If macOS refuses to open it because it was downloaded in a browser, run
`xattr -d com.apple.quarantine /usr/local/bin/clical`.

## Use

```bash
clical
```

On the first run, you choose a passphrase that protects your calendar
addresses. Press `a` to add a calendar, and `?` to see all keys.

- **iCal link:** in Google Calendar, open **Settings**, choose the calendar,
  and copy **Secret address in iCal format** from **Integrate calendar**.
  Treat it like a password.
- **Google account:** create a Desktop OAuth client in the
  [Google Cloud Console](https://console.cloud.google.com/) with the Google
  Calendar API enabled, then enter its client ID and secret in CLIcal.

## Development

You need Go 1.27 or newer.

```bash
git clone https://github.com/miphe/clical.git
cd clical
go run ./cmd/clical        # run from source
go test ./...              # run the tests
```

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
