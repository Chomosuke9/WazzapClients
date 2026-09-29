# AGENTS.md

Guidance for AI coding agents (and humans) working on this repository.

## Project

WazzapClients is a lightweight, native WhatsApp desktop client written in Go.

- **UI:** [Gio](https://gioui.org) (`gioui.org`), an immediate-mode GPU UI toolkit.
- **WhatsApp protocol:** [`github.com/polymorfa/hypermeow`](https://github.com/polymorfa/hypermeow),
  branch `main`. It is a performance-focused fork of `tulir/whatsmeow` that keeps the
  upstream package names. Install it with `go get github.com/polymorfa/hypermeow@main`
  (tagged versions are retracted on purpose). Do **not** use upstream `go.mau.fi/whatsmeow`.
- **Goal:** look and feel as close to the official WhatsApp Desktop app as possible, while
  using a small fraction of its memory (no WebView, no Electron).

## Rule #1: read the docs before you write code

Gio is still pre-1.0. Its API changes between minor releases, and a lot of what you
remember (blog posts, old examples, training data) is **outdated or removed**. For example,
the event model moved from `gtx.Events(tag)`/`pointer.InputOp` to `gtx.Event(filters...)`/`event.Op`,
and `app.NewWindow()` became a zero-value `app.Window`.

**Every time you write or change code**, first check the current documentation for the
APIs you are about to use:

1. The version pinned in `go.mod` is the source of truth. Read the actual source in the
   module cache when you're unsure:
   `$(go env GOMODCACHE)/gioui.org@<version>/...`
2. API reference: https://pkg.go.dev/gioui.org (pick the version that matches `go.mod`).
3. Guides:
   - Learn: https://gioui.org/doc/learn/get-started, https://gioui.org/doc/learn/split-widget,
     https://gioui.org/doc/learn/common-errors
   - Architecture: https://gioui.org/doc/architecture/window, `/drawing`, `/input`,
     `/widget`, `/layout`, `/theme`, `/units`, `/text`, `/color`
4. Official examples: https://git.sr.ht/~eliasnaur/gio-example

The same applies to hypermeow: check its source, README, and the
[pkg.go.dev reference](https://pkg.go.dev/github.com/polymorfa/hypermeow) instead of assuming
upstream whatsmeow behavior. The fork adds and changes APIs (LID-first identities, and more).

Gotchas already found in the pinned version (v0.10.x):

- `layout.N`/`layout.S` clear only `Min.Y` and `layout.E`/`layout.W` only `Min.X`. Only
  `Center` and the corners clear both. Reset `Min` yourself if the child must shrink-wrap.
- Gio blends colors in linear space, so a translucent overlay looks much stronger than the
  same alpha in CSS. For subtle tints (wallpaper doodles), pre-mix an opaque sRGB color.
- `widget.Icon` caches only its last size and color. Use the `iconCache` in `internal/ui`
  instead of sharing one `widget.Icon` across call sites.
- Text rendering supports bitmap color-emoji fonts (CBDT/sbix) but not COLR fonts such as
  Windows' Segoe UI Emoji, which renders monochrome.

If a doc and the source disagree, trust the source for the pinned version. If you bump a
dependency, re-read the changelog and fix any deprecations in the same change.

## Layout

```
cmd/wazzap/        desktop app entry point
cmd/screenshot/    headless renderer that writes UI previews to PNG (for docs and review)
internal/ui/       Gio UI: theme, icons, nav rail, chat list, conversation, composer
internal/mock/     fake chats/messages used until the WhatsApp backend is wired in
```

## Conventions

- Keep the UI immediate-mode: state lives in plain structs, and every frame is rebuilt
  from that state. Don't cache widget trees.
- Keep UI code free of protocol types. The UI reads `internal/mock` style models; a future
  `internal/wa` package will adapt hypermeow events into those models.
- Sizes are in `unit.Dp` / `unit.Sp`, never raw pixels. Convert with `gtx.Dp` / `gtx.Sp`.
- Colors live in `internal/ui/theme.go` (light and dark palettes). Don't hard-code colors
  in widgets.
- Watch memory use. Low RAM is the reason this project exists.
- Run `gofmt`, `go vet ./...` and `go build ./...` before you finish.

## Commands

```sh
go run ./cmd/wazzap            # run the app
go run ./cmd/screenshot        # render preview PNGs into ./docs/
go vet ./... && go build ./...
```
