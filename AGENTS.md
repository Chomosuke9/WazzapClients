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
- `widget.Editor` paints all its text, and its caret, in one color and one font. When the
  composer's text has formatting or @mentions, the editor paints it transparent and
  `paintComposerText` (`composertext.go`) draws the text and caret itself. Bold and italic
  are faked (outline drawn twice, slant) so glyphs keep the advances the editor's caret
  and selection use. Color-emoji bitmaps ignore the text color, so the editor still
  paints them. `Editor.Regions` reuses the slice you pass it, so don't use it to append.
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
  a 12 MP photo to a screen). Downscale with `photo.Shrink` (`internal/photo`). Go's JPEG decoder
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
- Gio's window thread waits for the UI goroutine while it delivers an event. Never
  `SendMessage` to the window from the UI goroutine (it hangs both); post instead,
  as `desktop.SetWindowIcon` does. `Window.Perform` and `Window.Option` wait for the
  window thread too: outside a frame (a tray or notification request), call them on
  a goroutine of their own, as `host.show` raises the window.
- WinRT interfaces are called through vtables (`internal/notify`). Don't trust
  remembered IIDs: one wrong digit is E_NOINTERFACE. Windows PowerShell 5.1 reads the
  real ones and the method order from the system metadata, e.g.
  `[Windows.UI.Notifications.ToastNotification].GetInterfaces() | % { $_.FullName + " " + $_.GUID }`
  after loading the type with `, Windows.UI.Notifications, ContentType = WindowsRuntime`.
- Gio keys a path's GPU data by where the path was recorded, so a `clip.RRect`,
  `clip.Ellipse` or `clip.Path` built into the frame's ops is tessellated and uploaded
  to a new GPU buffer every frame. Shapes from `roundShape` (`memo.go`) are recorded
  once per size and drawn anywhere; `fillCircle` and `paintRRect` use them.
- Don't set a Go memory limit (`SetMemoryLimit`/`GOMEMLIMIT`). Once a long session's
  live heap nears it, the GC runs back to back on several cores, and scrolling the
  chat list went from ~5 to ~100 s of CPU (`memprobe -scroll 1200 -ballast 60`).
- A rectangle clip under a transform that isn't a whole-pixel offset becomes a path
  too, and text outlines are rebuilt. `moveBy` rounds to whole pixels; `pushFx` counts
  real scales in `fxDepth`, under which `paintRRect` draws one path (no seams).
- Gio clips a color-emoji bitmap to the bitmap's own size (about 136x128) before
  scaling it to the font size, so above ~109 px only its top left corner shows. The
  photo editor lays text and emoji out at most `markTextPx` tall and scales them up.
- Key events go to whoever asks for them first in a frame. `updatePaste` reads Ctrl+V
  before the composer does: files or a picture on the clipboard (`internal/osclip`)
  open the send view, and anything else is handed back with `clipboard.ReadCmd`.
- The slash command picker reads Up, Down, Tab and (when it picks) Enter before the
  composer does, in `slashKeys`, and only while it offers rows; otherwise the editor
  moves its caret with them as usual.
- Files dropped on the window come through an OLE drop target (`desktop.EnableDrop`).
  OLE wants it registered on the window's own thread, so the window is subclassed and
  the registration posted to it; the callbacks run on that thread and only queue.

If a doc and the source disagree, trust the source for the pinned version. If you bump a
dependency, re-read the changelog and fix any deprecations in the same change.

## Layout

```
cmd/wazzap/        desktop app entry point (-demo for fake data, -debug for protocol logs)
cmd/screenshot/    headless renderer that writes UI previews to PNG (for docs and review)
cmd/memprobe/      Windows memory benchmark: clicks through stored or demo chats, prints memory
internal/model/    Chat/Message/Event types and the Backend interface the UI talks to
internal/ui/       Gio UI: login/QR, nav rail, pages (chats, status, channels, communities,
                   settings), conversation and composer (an album's pictures as one grid in
                   album.go), contact/group info panel (a person's
                   or business's sections in contactinfo.go), and the
                   overlays: context menus (popup.go), dialogs and toasts (dialog.go), emoji
                   picker (emoji.go, data in the generated emojidata.go), media viewer
                   (viewer.go); the send view for picked, pasted and dropped files
                   (sendview.go), its photo editor (mediaedit.go) and the rendering of
                   edits and the send queue (editrender.go); replies, @mentions and
                   select mode live in compose.go;
                   document cards and the voice/audio player in files.go; selecting message
                   text in textsel.go; searching a chat's messages (the panel that takes the info
                   panel's place) and a group's members in chatsearch.go; the composer's
                   formatting toolbar in formatbar.go; the attach
                   menu, file tray and poll dialog in attach.go; slash commands (their
                   picker over the composer and the notes only you see) in slash.go, and
                   the Extra features settings page in extras.go (the app's own features,
                   such as slash commands, @admin and Raw photos: each off until turned on); posting your own status
                   (its menus, the text composer, photos through the send view) in statuspost.go;
                   animation helpers in anim.go; the "N unread messages" divider a chat
                   opens at in unread.go; group invite links (the dialog that joins
                   one) in invite.go; a community's announcements (cards down the
                   middle headed by their sender, a forward button beside them, and
                   "Only community admins can send messages" for members) in announce.go;
                   chat actions shared by menus and info panels (mute choices, lists,
                   clear/exit/delete confirms) in chatactions.go; the open chat's ⋮ menu and
                   the settings it shares with the info panel (disappearing timer, chat
                   theme, encryption code, Add member, invite link) in chatmenu.go; the
                   info panel's own pages (starred messages, group permissions, member
                   changes) in infopages.go; the Media panel (the rail's Media button:
                   media, docs and links from every chat, or one chat's) in gallery.go; the list column's
                   draggable edge and hiding it (the open page's rail button, Ctrl+Shift+L) in split.go; a sender's run of stickers, side by side as many to a
                   line as fit, in stickerrow.go; the New chat panel and the
                   New group flow (also "Create a similar group") in newchat.go; the settings
                   pages (profile, account, privacy, chats, shortcuts, help) in
                   settingsdetail.go
internal/ui/icon/  Material Symbols from SVG path data (symbols.go is generated) and the
                   wallpaper doodles
internal/ui/styledtext/  gio-x styledtext, vendored with a fix for bitmap emoji
internal/command/  slash commands, like Discord's: the list (commands.go), parsing their options,
                   and running them through a Host the UI implements. No Gio here
internal/sticker/  turns a picture into a 512x512 sticker, with meme text in the embedded Anton
                   font (OFL), and its own lossless WebP (VP8L) encoder: x/image only decodes
                   WebP, and libwebp needs cgo or a WASM runtime
internal/wa/       hypermeow backend: pairing, events, SQLite message store, name resolution;
                   albums (an albumMessage, then each picture pointing back to it) in album.go
internal/mock/     demo Backend with fake chats (used by -demo and cmd/screenshot)
internal/photo/    scales and compresses photos to send (Standard, HD, Raw) and Shrink
internal/webpanim/ animated WebP (animated stickers), decoded one frame at a time
internal/video/    plays videos with the OS's own player (Media Foundation on Windows);
                   other systems return ErrUnsupported and open the system's player app.
                   OpenAudio plays voice messages and audio files the same way
internal/osclip/   files and pictures on the system clipboard (Gio's carries only text)
internal/filepick/ the system's "Open" dialog (comdlg32 on Windows; zenity, kdialog or
                   osascript elsewhere), run on its own goroutine
internal/memtrim/  gives memory back to the OS after 10 s without a frame (see ui.Run)
internal/notify/   system notifications: WinRT toasts on Windows (replaced per chat, removed
                   when read, Reply and Mark as read through a COM activator), notify-send
                   or osascript elsewhere
internal/desktop/  tray icon, one instance per data directory, start at login, window icon
                   (Windows; stubs elsewhere)
internal/accounts/ the WhatsApp accounts linked on this computer (accounts.json), each with
                   a data directory of its own: the first is the data directory itself,
                   the ones added later are accounts/<n>
patches/           go-text memory patch and apply.sh, which builds third_party/ (gitignored)
```

## Window lifecycle and notifications

`ui.Run` (`internal/ui/host.go`) owns the process. Its goroutine is the UI goroutine
for good, with or without a window: a helper goroutine waits for each window event and
hands it over, so requests (tray, notification clicks, a second launch) are served even
while the window is minimized and Gio draws no frames.

- With the tray icon up and the user logged in, closing the window destroys it, which
  frees its GPU textures, and drops the window's `UI` and the package caches
  (`dropCaches`; add new package-level drawing caches there). The backend keeps running;
  the next window gets a fresh `UI` built from the stored chats plus the latest
  `ConnEvent`. `-background` starts without a window (start at login).
- Every backend event goes through `host.poll`: the notifier (`notifications.go`) sees
  them all, and the window's `UI` gets them through `hostBackend.Poll`. Only
  `MessageEvent`s with `New` set notify; backends set it for messages that just arrived
  (not history, edits, reactions or repeats).
- Several accounts can be linked; one is open (connected) at a time, to keep memory
  low. Switching (`internal/ui/accounts.go`: the ⋮ menu's "Switch account" and the
  login screen) closes the open backend, opens the other one through `Options.Open`
  and gives the window a new `UI`. The theme and other `appPrefs` carry over. Logging
  out with another account linked opens that one and takes the logged-out account
  off the list; so does switching away from an account that isn't linked.
- Notification rules follow WhatsApp: one per chat, nothing while the window has focus,
  muted and archived chats only for mentions and replies to you, removed once the chat
  is read (here or on another device). Preferences are `Backend.Pref` keys, on unless
  "off" (`prefNotify*`, `prefBackground`).

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
- UI tests run on a clock of their own (`internal/ui/main_test.go`): 15:00 UTC on a fixed
  day, moving on in real time, read by the mock (`mock.Clock`) and the UI. The demo chats
  are dated "today at 09:02" by it, so on the real clock tests passed in one time zone or
  hour and failed on CI, which runs in UTC. Use `testNow()` in tests, never `time.Now()`,
  and backend code should read its clock (`b.now()`), not `time.Now()`.

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
go run ./cmd/wazzap -background  # start in the tray, without a window
go run ./cmd/screenshot        # render preview PNGs into ./docs/
go run ./cmd/memprobe -demo    # memory benchmark (Windows); -data <copy of the data dir>
# Scroll the chat list for 1200 frames and print frame times, CPU and GCs; -ballast
# adds live heap like a long session's, -cpuprofile writes a profile:
go run ./cmd/memprobe -data <copy> -scroll 1200 -ballast 60 -cpuprofile cpu.pprof
go vet ./... && go build ./...

# Side by side with a WhatsApp screenshot (writes compare.png and ours.png).
# -view: chats, archived, status, channels, communities, settings, general, profile,
# account, privacy, lastseen, blocked, chatsettings, notifications, shortcuts, extras, help,
# info, statusviewer, contact (a group member's contact info:
# -contact <id>, default the demo business vivy@lid)
go run ./cmd/screenshot -compare shot.webp -crop 0,0,2000,1250 -scale 1.22 -view status
# A crop of the right edge of a 2560x1600 window, with the info panel scrolled:
go run ./cmd/screenshot -compare info.png -crop 0,0,795,1597 -win 2560,1600 -right \
    -scale 1.5616 -view info -infoscroll 7 -infooffset 40
# Render one overlay with demo data (menu, accounts, loginaccounts, slash, slashkick, slashrun (open a
# group: -ochat work), chatmenu, mute, lists, msgmenu, stickermenu, emoji, sticker, viewer, forward, reply, invite,
# delete, select, mention, mentioned, search (WAZZAP_DEMO_SEARCH=<query>), membersearch; the Media panel:
# gallery, gallerydocs, gallerylinks, galleryselect, chatgallery, starredall (the ⋮ menu's Starred messages); the open chat's convmenu, timer, theme,
# encryption, addmember, invitelink, and its info pages perms, starred, changes; the list column listwide, listnarrow, listhidden; the send view: tray, sendedit, sendcrop, sendfilter, senddoc, with
# WAZZAP_DEMO_PHOTO=<a photo> to edit) into <out>/overlay-<name>.png:
go run ./cmd/screenshot -overlay msgmenu -at 700,300 -out /tmp/shots
# Film an animation into <out>/film-<name>.png: frames -step apart, opening on top and
# closing (Esc) below. Also info, message, reorder, typing, and hover (the pointer at -at):
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
