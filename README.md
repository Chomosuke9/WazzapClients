# WazzapClients

A lightweight, native WhatsApp desktop client written in Go.

WazzapClients aims to look and feel like the official WhatsApp Desktop app while using a
small fraction of its memory: no WebView, no Electron. The UI is built with
[Gio](https://gioui.org), and the WhatsApp protocol is handled by
[hypermeow](https://github.com/polymorfa/hypermeow), a performance-focused fork of
whatsmeow.

## Features

- Link to your account by scanning a QR code, like WhatsApp Web
- Chats, groups, communities, channels and status updates
- Replies, @mentions, emoji picker, forwarding, deleting and multi-select
- Media viewer and contact/group info panel
- Light and dark themes
- Messages stored locally in SQLite

## Download

Prebuilt packages for Windows (amd64) and Linux (amd64) are published on the
[Releases](https://github.com/chomosuke9/wazzapclients/releases) page and as artifacts of
each CI run. See [docs/releasing.md](docs/releasing.md) for details.

## Build from source

Requires the Go version in `go.mod`. On Linux, install Gio's
[system dependencies](https://gioui.org/doc/install/linux) first.

```sh
go run ./cmd/wazzap            # run the app and link it via QR code
go run ./cmd/wazzap -demo      # run with fake chats, no network
go run ./cmd/wazzap -debug     # log protocol traffic
go build ./...
```

Your session and messages are stored in `%AppData%\WazzapClients\wazzap.db` on Windows
(the user config directory on other platforms).

## Project layout

```
cmd/wazzap/       desktop app entry point
cmd/screenshot/   headless renderer for UI previews and comparisons
internal/model/   UI-facing types and the Backend interface
internal/ui/      Gio user interface
internal/wa/      hypermeow backend and SQLite message store
internal/mock/    demo backend with fake data
```

## Contributing

Read [AGENTS.md](AGENTS.md) before changing code. It covers conventions, Gio gotchas in
the pinned version, and the screenshot tooling used to match WhatsApp Desktop. Run
`gofmt`, `go vet ./...` and `go build ./...` before sending changes.

## Disclaimer

This is an unofficial client, not affiliated with or endorsed by WhatsApp or Meta. Using
third-party clients may violate WhatsApp's Terms of Service; use it at your own risk.
