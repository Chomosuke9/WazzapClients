# AGENTS.md

Guidance for AI coding agents (and humans) working on this repository.

## Project

WazzapClients is a lightweight, native WhatsApp desktop client written in Go.

- **UI:** [Gio](https://gioui.org) (`gioui.org`), an immediate-mode GPU UI toolkit.
  Its text library `github.com/go-text/typesetting` is patched to use less memory:
  `patches/apply.sh` builds the patched copy in `third_party/typesetting` (not checked in;
  run it after cloning). See `patches/README.md`.
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
   `$(go env GOMODCACHE)/gioui.org@<version>/...`. go-text is `third_party/typesetting`
   (upstream plus `patches/typesetting.patch`). Change go-text through the patch, not in
   `third_party`, which `apply.sh` overwrites.
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
- Gio makes only its window thread DPI aware on Windows. While a drag holds the mouse
  capture, Windows then reports the pointer in DPI-unaware coordinates (divided by the
  display scale), so every dragged thing lagged the pointer. `dpi_windows.go` makes the
  whole process per-monitor aware at init.
- `material.List`'s scrollbar turns thumb drags into "scroll by N items" against a length
  re-estimated from the visible rows, so the thumb drifts from the pointer. Use
  `u.scrollList` (`internal/ui/scrollbar.go`) for every list.
- Gio's shaper keeps up to 1000 color-emoji bitmaps decoded at the font's full 136x128
  (70 KB each) for good. The emoji picker draws its emojis as pictures from
  `u.emojiImgs` instead (`emojiimg.go`); scrolling it through Labels pinned ~70 MB.
- `x/image/draw`'s `CatmullRom` allocates dst width x src height x 32 bytes (157 MB to fit
  a 12 MP photo to a screen). Downscale with `shrink` (`shrink.go`). Go's JPEG decoder
  keeps all of a progressive JPEG's coefficients (~7.5 bytes/pixel), so big pictures
  decode one at a time (`acquireDecode`).
- `golang.org/x/image/webp` can't read animated WebP. Animated stickers go through
  `internal/webpanim`; `stickerFrame` (`player.go`) plays the ones on screen.
- Videos use the OS's decoder, never a bundled codec (`internal/video`). The Windows
  backend calls COM through `syscall.SyscallN` without cgo: convert pointers to
  `uintptr` inside the `SyscallN` argument list, and read `double` results from `r2`
  (Go returns XMM0 there). Media Foundation's memory goes back only with `MFShutdown`,
  so each player starts and shuts it down. The viewer's video UI is `videoview.go`;
  `WAZZAP_DEMO_VIDEO=<file.mp4>` makes `-demo` play that file.
- `f32.Rectangle` no longer exists. `image.Rect` normalizes swapped corners, so build an
  `image.Rectangle{Min: ..., Max: ...}` literal when `Max` is computed from `Min`.
- `gtx.Disabled()` blocks `gtx.Execute` too, so a disabled context can't ask for the
  next frame. Step animations with the enabled context before disabling it.
- A disabled context still registers its input areas, and Gio hit-tests them: they
  read no events but block every handler underneath. Draw anything fading away with
  `fadeOut` (`anim.go`), which also pushes a `pointer.PassOp`.
- A `ScrollToEnd` list drops a trailing child of height 0 when it trims to the
  viewport, then stops following the end. Rows that grow in start at 1px.
- `paint.PushOpacity` draws into an offscreen texture that Gio keeps, at the largest
  size ever needed, until the window closes. A fade of the whole window would pin
  ~16 MB. Keep opacity layers small (see Animations).
- Gio stencils every path over its whole bounding box, every frame, into a coverage
  texture that, like the opacity one, never shrinks. A rounded clip around big content,
  or a big `clip.RRect` fill, costs a screen-sized texture. Fill rounded rectangles with
  `fillRRect`/`paintRRect`, which only stencil the corners, and round a big panel's
  corner with a mask (`roundCorner`) instead of clipping it.
- A rectangle clip under a transform that isn't a whole-pixel offset becomes a path
  too, and text outlines are rebuilt. `moveBy` rounds to whole pixels; `pushFx` counts
  real scales in `fxDepth`, under which `paintRRect` draws one path (no seams).

If a doc and the source disagree, trust the source for the pinned version. If you bump a
dependency, re-read the changelog and fix any deprecations in the same change.

## Layout

```
cmd/wazzap/        desktop app entry point (-demo for fake data, -debug for protocol logs)
cmd/screenshot/    headless renderer that writes UI previews to PNG (for docs and review)
cmd/memprobe/      Windows memory benchmark: clicks through stored or demo chats, prints memory
internal/model/    Chat/Message/Event types and the Backend interface the UI talks to
internal/ui/       Gio UI: login/QR, nav rail, pages (chats, status, channels, communities,
                   settings), conversation and composer, contact/group info panel, and the
                   overlays: context menus (popup.go), dialogs and toasts (dialog.go), emoji
                   picker (emoji.go, data in the generated emojidata.go), media viewer
                   (viewer.go); replies, @mentions and select mode live in compose.go;
                   document cards and the voice/audio player in files.go; the attach
                   menu, file tray and poll dialog in attach.go; animation helpers in anim.go
internal/ui/icon/  Material Symbols from SVG path data (symbols.go is generated) and the
                   wallpaper doodles
internal/ui/styledtext/  gio-x styledtext, vendored with a fix for bitmap emoji
internal/wa/       hypermeow backend: pairing, events, SQLite message store, name resolution
internal/mock/     demo Backend with fake chats (used by -demo and cmd/screenshot)
internal/webpanim/ animated WebP (animated stickers), decoded one frame at a time
internal/video/    plays videos with the OS's own player (Media Foundation on Windows);
                   other systems return ErrUnsupported and open the system's player app.
                   OpenAudio plays voice messages and audio files the same way
internal/filepick/ the system's "Open" dialog (comdlg32 on Windows; zenity, kdialog or
                   osascript elsewhere), run on its own goroutine
internal/memtrim/  gives memory back to the OS after 30 s without a frame (see ui.Run)
patches/           go-text memory patch and apply.sh, which builds third_party/ (gitignored)
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
- Don't drop messages the app can't show: `parse` stores them as `KindUnsupported` ("This
  message couldn't load"). Add harmless protocol fields to `noContentFields`
  (`internal/wa/interactive.go`) instead. Business message buttons live there too.
- `u.msgs` is a window of the open chat, not all of it: pages of 100 load as the list
  nears either end, and at most 400 stay loaded (`internal/ui/paging.go`). Don't assume a
  message is in `u.msgs`; `jumpTo` loads the messages around one that isn't.
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
- Watch memory use. Low RAM is the reason this project exists. Measure with
  `cmd/memprobe` before and after a change; the private working set is what Task Manager
  shows. Keep caches bounded by bytes, not just entries (see `imageCache`).

## Animations

The helpers are in `internal/ui/anim.go`. Each frame computes an animation's progress
from `gtx.Now`; a moving one asks for the next frame, and nothing asks at rest
(`TestIdleAtRest` checks this).

- `tween` is an on/off progress (a popup opening, a panel sliding) that turns around
  midway without jumping. `follower` glides a number to a new target (tab underlines),
  and `switcher` moves a highlight between items (the open chat, the active rail
  button). Hovers and other per-widget fades go through `u.hover` and `u.anims`.
- A closing overlay keeps its state with a `closing` flag (or a "ghost" copy of what
  it showed) and draws with `fadeOut(gtx)` while it fades, so clicks go through.
  Check `isOpen()` or `shown()` rather than the raw fields.
- Don't animate icon or `cachedGlyph` colors: both are cached per color. Cross-fade
  two colors with `withOpacity`.
- Keep opacity layers small (menus, pickers, rows). For big areas, fade a backdrop's
  color and the parts on it one by one, or cover content on a plain background with a
  `veil` of that background.
- Lay out vertical lists with `u.scrollList`, not `List.Layout`: besides the scrollbar,
  it takes the mouse wheel before the list and eases each notch in over a few frames
  instead of jumping (`wheelList`, `internal/ui/scroll.go`).
- Film an animation with `cmd/screenshot -film` (see Commands) to check its frames.
- Run `gofmt`, `go vet ./...` and `go build ./...` before you finish.

## Commands

```sh
sh patches/apply.sh            # once after cloning: builds the patched go-text
go run ./cmd/wazzap            # run the app (links to WhatsApp via QR code)
go run ./cmd/wazzap -demo      # run with fake chats, no network
go run ./cmd/screenshot        # render preview PNGs into ./docs/
go run ./cmd/memprobe -demo    # memory benchmark (Windows); -data <copy of the data dir>
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
# Film an animation into <out>/film-<name>.png: frames -step apart, opening on top and
# closing (Esc) below. Also info, message, reorder, and hover (the pointer at -at):
go run ./cmd/screenshot -film msgmenu -at 700,300 -scale 1 -w 1100 -h 700 -step 40ms -out /tmp/shots
# Render your real stored chats instead of demo data (no network):
go run ./cmd/screenshot -compare shot.webp -crop 0,0,2000,1250 -scale 1.22 \
    -data "$APPDATA/WazzapClients" -view channels
```

## Committing

- Stage files by name (`git add path/to/file.go`), never `git add -A` or `git add .`.
- Never commit `third_party/` (the patched go-text that `patches/apply.sh` generates,
  ~56k lines) or `.idea/` (IDE settings). Both are gitignored; if either shows up as
  untracked, the `.gitignore` is missing or out of date, so sync with `origin/main`.
- Check `git diff --cached --stat` before committing. A change of tens of thousands of
  lines means something generated got staged.
- Before committing on `main`, check that it isn't behind `origin/main`
  (`git fetch && git status`), so the commit lands on the current code.
