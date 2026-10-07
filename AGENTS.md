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
- `widget.Clickable` clips what it draws to its own size, so a badge or ring that sticks
  out past a button gets cut: draw it after the Clickable (see `railButton`).
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
- Gio blends every pixel of every draw, even one an opaque draw covers later. On a
  2560x1600 window at 165 Hz with integrated graphics a full-window fill takes about
  0.2 ms of every frame, and frames used to fill each pixel almost four times. Don't
  paint a background under something opaque: the card around the panes has none,
  the wallpaper's tile has its background in it, and list rows on the panel paint
  nothing at rest (`rowBg`).
- Task Manager's GPU % follows the GPU's clock, which drops with the load, so it
  barely moves when a frame gets cheaper. Measure the GPU time per frame with the
  `\GPU Engine(pid_<pid>_*engtype_3D)\Running Time` counter (100 ns units) during a
  `memprobe -scroll` run. Gio presents through a blt-model swap chain, so DWM copies
  and composes every frame (about as much GPU time again).
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
  moves its caret with them as usual. In a command's text that takes @mentions
  (`Option.Mentions`, /schedule's message) the mention picker shows instead, and it
  takes Enter first (`slashTakesMentions`).
- hypermeow doesn't parse a group's `<general_chat/>` (a community's General chat).
  `markGeneralChats` (`internal/wa/communities.go`) asks for the joined groups again
  through `DangerousInternals().SendGroupIQ` on connect and reads it from the raw nodes.
  Run the app with `-debug` to see raw nodes in `wazzap.log` when something is missing.
- WhatsApp now sends most message edits as a `secretEncryptedMessage` (MESSAGE_EDIT),
  decrypted with `DecryptSecretEncryptedMessage`, not as a `protocolMessage`. History
  sync (`ParseWebMessage`) hands an edit out as the new content under the original's
  ID with `IsEdit` set. `parse` turns all three into an edit; `putMessage` never lets
  the original coming again overwrite an edited text.
- SQLite uses a partial index only when the query's WHERE repeats its condition: it
  can't tell that `media IN (?, ?)` or `media = ?` means `media != 0`, and read every
  message for the Media panel until `galleryWhere` said so. A sort also reads every
  column it returns of every row it sorts, so the gallery sorts rowids alone and reads
  the page's rows after (each one's payload is decoded).
- modernc's SQLite binds every statement of a multi-statement `Exec` from the first
  argument, so positional `?` in a second statement gets the wrong values. Use
  numbered `?1`, `?2` (as `deleteChat` does) or separate `Exec` calls.
- Poll votes and event answers are encrypted with the poll's or event's message
  secret, which hypermeow stores as messages arrive. It decrypts votes
  (`DecryptPollVote`) but not event answers; `decryptEventResponse` (`internal/wa/polls.go`)
  does those the same way. hypermeow can't send an event answer (it has no stanza
  type for it), so the app shows answers but doesn't send them. Your own votes are
  stored under `meVoter`, since they come from your phone number or your LID.
- WhatsApp sends view once media only to the phone. Linked devices get a stanza with
  `<unavailable type="view_once"/>` and no ciphertext, which hypermeow hands out as an
  `events.UndecryptableMessage`; `onUndecryptable` stores it as a `KindViewOnce`
  placeholder (`OnPhone`). A reply quoting it often carries its media key, and
  `fillViewOnce` gives the placeholder that media. While one is shown the window is kept
  out of screenshots (`desktop.BlockCapture`, `SetWindowDisplayAffinity` posted to the
  window's thread through the drop target's subclass).
- Files dropped on the window come through an OLE drop target (`desktop.EnableDrop`).
  OLE wants it registered on the window's own thread, so the window is subclassed and
  the registration posted to it; the callbacks run on that thread and only queue.
- `widget.Clickable.Layout` reads and drops the clicks nobody has asked about yet.
  Check `Clicked` before laying the button out (the title bar's privacy button).
- Many buttons act on their click while they're laid out, after parts of the window
  that show the change were drawn. `updateMouse` asks for one more frame after every
  press and release, or those parts waited for the pointer to move;
  `TestClickRedraws` clicks all over the demo window to check.
- A new link's app state sync (pins, mutes, archives) comes before the chats'
  history. `setField` creates a chat it doesn't have yet (unlisted until it has
  messages) and marks the setting in `wz_chats.settled`, so the history's older
  copy (`setMeta`) doesn't undo it. Reading on the phone with read receipts on comes
  as a plain read receipt from your own device, not `read-self`.

If a doc and the source disagree, trust the source for the pinned version. If you bump a
dependency, re-read the changelog and fix any deprecations in the same change.

## Layout

```
cmd/wazzap/        desktop app entry point (-demo for fake data, -debug for protocol logs)
cmd/screenshot/    headless renderer that writes UI previews to PNG (for docs and review)
cmd/memprobe/      Windows memory benchmark: clicks through stored or demo chats, prints memory;
                   -scroll scrolls instead and prints frame times and the frame rate
cmd/winres/        draws the app icon (icon.App) into the .exe's resources and installer/wazzap.ico;
                   run `go generate ./cmd/wazzap` after changing the icon, and commit both
cmd/signrelease/   signs a release's SHA256SUMS with the update key (CI), or makes a new key
installer/         the Windows installer (Inno Setup), per user in %LocalAppData%\Programs
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
                   select mode live in compose.go; editing your messages (in the composer)
                   and the Edit history dialog in edit.go; Message info (who got and read
                   one of your messages, and when; it takes the info panel's place) in
                   msginfo.go; profile pictures and their placeholders (a person's initial
                   on a color from their ID, a General chat's speech bubble) in avatar.go; link previews, in bubbles and
                   over the composer, in linkpreview.go; drafts (what a chat's composer
                   and send view held when another chat opened, and the list's "Draft:")
                   in draft.go;
                   document cards and the voice/audio player in files.go; polls (voting by
                   clicking an option), maps, shared contacts and events as cards in bubbles
                   in cards.go, and a poll's votes or an event's answers (the Message info
                   panel's other mode) in votes.go; selecting message
                   text in textsel.go; searching a chat's messages (the panel that takes the info
                   panel's place) and a group's members in chatsearch.go; the composer's
                   formatting toolbar in formatbar.go; the attach
                   menu, file tray and poll dialog in attach.go; slash commands (their
                   picker over the composer and the notes only you see) in slash.go; scheduled
                   messages as bubbles after a chat's newest (send now, edit, cancel) and
                   the AFK bar above the chat list in scheduled.go; saved messages (Settings >
                   Snippets, the snippets /snippet send offers in the slash picker, their {variables}
                   and /catch's payload dialog) in snippets.go; and
                   the Extra features settings page in extras.go (the app's own features,
                   such as slash commands, @admin, the Privacy mode toggle, Block screen
                   recording and Raw photos: each off until turned on), and
                   its Ethically gray features page (Edit history, Keep deleted messages, Replay view
                   once, and commands marked Gray, like /ghost, each with a switch of its own);
                   view once messages (opened once, screenshots blocked) in viewonce.go; posting your own status
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
                   settingsdetail.go; the window's zoom (Settings > General > Font size,
                   Ctrl with +, - and 0, and the bubble that shows it) in scale.go; privacy
                   mode (names and messages drawn as bars, pictures blurred, shown under
                   the pointer; the title bar's eye and Ctrl+Shift+P, once the Privacy mode
                   toggle on the Extra features page is on) in privacy.go: `u.hiding` sets `u.secret`, which
                   `u.label`, `layoutSpans`, `drawAvatar` and `messageImage` read
internal/ui/icon/  Material Symbols from SVG path data (symbols.go is generated) and the
                   wallpaper doodles
internal/ui/styledtext/  gio-x styledtext, vendored with a fix for bitmap emoji
internal/command/  slash commands, like Discord's: the list (commands.go), parsing their options,
                   and running them through a Host the UI implements. No Gio here
internal/auto/     scheduled messages (/schedule) and AFK replies (/afk): wraps the Backend
                   (`withAuto`; the host's, so they go on without a window), sees its events
                   and your sends, and keeps its state in the backend's prefs. In ghost mode
                   (/ghost) it refuses your own sends, but not its own
internal/sticker/  turns a picture into a 512x512 sticker, with meme text in the embedded Anton
                   font (OFL), and its own lossless WebP (VP8L) encoder: x/image only decodes
                   WebP, and libwebp needs cgo or, translated to Go, adds megabytes
internal/wa/       hypermeow backend: pairing, events, SQLite message store, name resolution;
                   what a stored message shows, read from its payload (raw_payload, which
                   /catch shows, and edit_payload), in payload.go;
                   albums (an albumMessage, then each picture pointing back to it) in album.go;
                   polls, locations, contact cards and events (cards in bubbles) and the
                   votes and event answers they get (wz_votes) in polls.go;
                   each person's receipts of your messages (wz_receipts, for Message info;
                   a group message's ticks wait for every member) in receipts.go;
                   snippets (wz_snippets, their media copied to snippet-media/) in snippets.go
                   and snippetmedia.go
internal/mock/     demo Backend with fake chats (used by -demo and cmd/screenshot)
internal/photo/    scales and compresses photos to send (Standard, HD, Raw) and Shrink
internal/webpanim/ animated WebP (animated stickers), decoded one frame at a time
internal/video/    plays videos with the OS's own player (Media Foundation on Windows);
                   other systems return ErrUnsupported and open the system's player app.
                   OpenAudio plays voice messages and audio files the same way
internal/linkpreview/ reads a page's Open Graph tags (or title) for the preview of a link you
                   send; Settings > Privacy > Disable link previews turns it off
internal/osclip/   files and pictures on the system clipboard (Gio's carries only text)
internal/filepick/ the system's "Open" dialog (comdlg32 on Windows; zenity, kdialog or
                   osascript elsewhere), run on its own goroutine
internal/memtrim/  gives memory back to the OS after 10 s without a frame (see ui.Run)
internal/notify/   system notifications: WinRT toasts on Windows (replaced per chat, removed
                   when read, Reply and Mark as read through a COM activator), notify-send
                   or osascript elsewhere
internal/desktop/  tray icon, one instance per data directory, start at login, window icon
                   (Windows; stubs elsewhere)
internal/update/   updates from GitHub releases when the user asks (Settings > Help): checks
                   the signed SHA256SUMS, swaps the executable (the running one moves to
                   .old on Windows) and the UI restarts it with -wait-pid (ui/update.go).
                   Only tag builds (-X main.version=v1.2.3) update
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
- A message is stored as its payload, the waE2E protobuf it came or went as
  (`raw_payload`), plus its latest edit's (`edit_payload`). Everything it shows (media,
  thumbnail, file, link preview, quote, mentions, cards, buttons...) is read from them by
  `rawMsg.fill` (`internal/wa/payload.go`), the way `parse` reads a message that arrives;
  don't add a column for something the payload holds. The columns are only the envelope
  (sender, time), what happens to a message later (receipt, reaction, star, pin, edit,
  revoke, opened) and `kind`, `media` and `text`, copies kept for queries. Your own
  messages get a payload before they go (a file's, before its upload) and
  `setRawPayload` replaces it as they're sent. `dropOldMessages` drops the messages of a
  database from older versions, which kept them in columns of their own, once.
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
  it takes the mouse wheel before the list and a critically damped spring eases the
  notches in, its speed never jumping, even when a notch comes mid-scroll (`wheelList`,
  `internal/ui/scroll.go`). On Windows, touchpad deltas (not multiples of 120) move
  the list at once.
- Frames go out one per display refresh, but start unevenly (4.5 to 7.5 ms apart at
  165 Hz), and steps worked out from those times judder. The window's `gtx.Now` comes
  from `frameClock` (`anim.go`), which moves by whole refresh intervals while frames
  follow each other. Tests and `cmd/memprobe` draw with the real (or test) time.
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
# Scroll the chat list for 1200 frames and print frame times, CPU, GCs, the frame
# rate and late frames; -ballast adds live heap like a long session's, -cpuprofile
# writes a profile. -chat scrolls the first chat's messages instead, and -size sets
# the window in dp (1706x1040 is about a maximized 2560x1600 window at 150%):
go run ./cmd/memprobe -data <copy> -scroll 1200 -ballast 60 -cpuprofile cpu.pprof
go run ./cmd/memprobe -demo -scroll 2000 -size 1706x1040 -chat
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
# Render one overlay with demo data (menu, accounts, loginaccounts, slash, slashkick, slashcalc, slashschedule, slashrun, ghost (/ghost; open a
# group: -ochat work), chatmenu, mute, lists, msgmenu, stickermenu, emoji, sticker, viewer, forward, reply, replyphoto, linkpreview, invite,
# delete, select, edit, edits, mention, mentioned, votes (a poll's or event's votes: -ochat design or family), search (WAZZAP_DEMO_SEARCH=<query>), membersearch, zoombubble, zoommenu, privacy (also
# privacystatus, privacycommunities); the Media panel:
# gallery, gallerydocs, gallerylinks, galleryselect, chatgallery, starredall (the ⋮ menu's Starred messages); the open chat's convmenu, timer, theme,
# encryption, addmember, invitelink, and its info pages perms, starred, changes; the list column listwide, listnarrow, listhidden; the send view: tray, sendedit, sendcrop, sendfilter, senddoc, with
# WAZZAP_DEMO_PHOTO=<a photo> to edit) into <out>/overlay-<name>.png:
go run ./cmd/screenshot -overlay msgmenu -at 700,300 -out /tmp/shots
# Film an animation into <out>/film-<name>.png: frames -step apart, opening on top and
# closing (Esc) below. Also info, message, reorder, typing, typists (a second person
# typing in a group, then the first stopping: -ochat work), ghost (on, then off), privacy (on, then off),
# privacyhover (privacy mode on, the pointer at -at), vote (in a poll: -ochat design,
# moving the vote below), and hover (the pointer at -at):
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
