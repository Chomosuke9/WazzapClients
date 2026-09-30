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
- `widget.Icon` caches only its last size and color. Icons come from `internal/ui/icon`
  instead, which caches a rasterized image per size and color.
- If a dependency fails to compile in a way upstream can't (e.g. an import cycle), run
  `go mod verify`. The module cache was once modified locally (probably by an IDE
  auto-import); delete that module version from `GOMODCACHE`, re-download it, and
  `go clean -cache`.
- Text rendering supports bitmap color-emoji fonts (CBDT/sbix) but not COLR fonts such as
  Windows' Segoe UI Emoji, which renders monochrome.
- The shaper never switches fonts for a space, so a space after an emoji takes the emoji
  font's (very wide) advance. `narrowEmojiSpaces` in `internal/ui/fonts.go` patches the
  bundled font's space glyphs at load time.
- `layout.Flex` passes its cross-axis minimum to every child. Giving a row a minimum
  height stretches its labels and pins their text to the top. Use `vcenter`
  (`internal/ui/draw.go`) to make a row taller.
- A `Flexed` child must return the full width it was given. If it returns less, the
  `Rigid` children after it move left (see how `layoutListItem` applies `padRight`).
- `layout.Center` doesn't center a child that is larger than the box; it places it at
  0,0. Record the child and position it yourself.
- `LineHeightScale` defaults to 1.2 and multiplies `LineHeight`. Set it to 1 when you
  want an exact line height.
- `widget.Clickable` registers its input area after drawing its content, so a Clickable
  wrapping a widget hides any Clickable inside it. Draw nested buttons afterwards, on
  top (see `layoutChatRow`), and use `hoverArea`/`rightClick`, which pass events through.
- Popups that must draw above later siblings (the emoji picker) use `op.Defer`, which
  keeps the local transform. Context menus instead open at `u.mouse`, the last pointer
  position in content coordinates.
- `widget.Editor` paints all its text in one color. Colored spans (the composer's
  @mentions) are drawn over it: see `paintMentions`. `Editor.Regions` reuses the slice
  you pass it, so don't use it to append.
- `f32.Rectangle` no longer exists. `image.Rect` normalizes swapped corners, so build an
  `image.Rectangle{Min: ..., Max: ...}` literal when `Max` is computed from `Min`.

If a doc and the source disagree, trust the source for the pinned version. If you bump a
dependency, re-read the changelog and fix any deprecations in the same change.

## Layout

```
cmd/wazzap/        desktop app entry point (-demo for fake data, -debug for protocol logs)
cmd/screenshot/    headless renderer that writes UI previews to PNG (for docs and review)
internal/model/    Chat/Message/Event types and the Backend interface the UI talks to
internal/ui/       Gio UI: login/QR, nav rail, pages (chats, status, channels, communities,
                   settings), conversation and composer, contact/group info panel, and the
                   overlays: context menus (popup.go), dialogs and toasts (dialog.go), emoji
                   picker (emoji.go, data in the generated emojidata.go), media viewer
                   (viewer.go); replies, @mentions and select mode live in compose.go
internal/ui/icon/  Material Symbols from SVG path data (symbols.go is generated) and the
                   wallpaper doodles
internal/ui/styledtext/  gio-x styledtext, vendored with a fix for bitmap emoji
internal/wa/       hypermeow backend: pairing, events, SQLite message store, name resolution
internal/mock/     demo Backend with fake chats (used by -demo and cmd/screenshot)
```

## Conventions

- Keep the UI immediate-mode: state lives in plain structs, and every frame is rebuilt
  from that state. Don't cache widget trees.
- Keep UI code free of protocol types. The UI only sees `internal/model`; `internal/wa`
  converts hypermeow events into model events. The UI drains them with `Backend.Poll` on
  the window goroutine, so UI state never needs locks.
- hypermeow stores keys and sessions, not messages. `internal/wa/store.go` keeps chats and
  messages in `wz_*` tables of the same SQLite file (`%AppData%\WazzapClients\wazzap.db`).
- Never call into hypermeow's device store inside a `wz_*` write transaction. It writes
  to the same SQLite file and would block until the busy timeout (see `onHistory`).
- One-to-one chats are keyed by LID when a mapping is known (`canonical`), because
  hypermeow treats the LID as the stable identity.
- Channels (newsletters) are stored like chats in `wz_chats`/`wz_messages`, plus their
  metadata in `wz_channels`, and are left out of the chat list. Status updates live in
  `wz_status`. Community structure is in the `parent`, `community` and `announce_sub`
  columns of `wz_chats`, filled from `GetJoinedGroups` on connect.
- Measure against real WhatsApp Desktop screenshots instead of guessing sizes. Use
  `cmd/screenshot -compare`, which renders the same view at the screenshot's scale next to
  it (see Commands). Full-window screenshots at 2000px wide are 1.22 px/dp; native
  2560x1600 crops are 1.5616 px/dp.
- Sizes are in `unit.Dp` / `unit.Sp`, never raw pixels. Convert with `gtx.Dp` / `gtx.Sp`.
- Colors live in `internal/ui/theme.go` (light and dark palettes). Don't hard-code colors
  in widgets.
- Watch memory use. Low RAM is the reason this project exists.
- Run `gofmt`, `go vet ./...` and `go build ./...` before you finish.

## Commands

```sh
go run ./cmd/wazzap            # run the app (links to WhatsApp via QR code)
go run ./cmd/wazzap -demo      # run with fake chats, no network
go run ./cmd/screenshot        # render preview PNGs into ./docs/
go vet ./... && go build ./...

# Side by side with a WhatsApp screenshot (writes compare.png and ours.png).
# -view: chats, archived, status, channels, communities, settings, info, statusviewer
go run ./cmd/screenshot -compare shot.webp -crop 0,0,2000,1250 -scale 1.22 -view status
# A crop of the right edge of a 2560x1600 window, with the info panel scrolled:
go run ./cmd/screenshot -compare info.png -crop 0,0,795,1597 -win 2560,1600 -right \
    -scale 1.5616 -view info -infoscroll 7 -infooffset 40
# Render one overlay with demo data (chatmenu, msgmenu, emoji, viewer, forward, reply,
# delete, select, mention, mentioned) into <out>/overlay-<name>.png:
go run ./cmd/screenshot -overlay msgmenu -at 700,300 -out /tmp/shots
# Render your real stored chats instead of demo data (no network):
go run ./cmd/screenshot -compare shot.webp -crop 0,0,2000,1250 -scale 1.22 \
    -data "$APPDATA/WazzapClients" -view channels
```
