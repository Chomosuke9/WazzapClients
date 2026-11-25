# Dependency patches

`typesetting.patch` patches go-text (`github.com/go-text/typesetting`, Gio's
text library) to use far less memory for fonts, and lets the app swap emoji
pictures. `gio.patch` patches Gio (`gioui.org`) so it lets go of pictures it
drew. `hypermeow.patch` patches hypermeow (`github.com/polymorfa/hypermeow`)
so it decrypts status updates, can send to some of status@broadcast, can take scheduled messages back,
sends stickers with a sound as stickers and chat themes as chat themes, and asks for missed messages in batches. `go.mod` replaces the modules with
`third_party/typesetting`, `third_party/gio` and `third_party/hypermeow`,
which `apply.sh` builds from the Go module cache and are not checked in:

```sh
sh patches/apply.sh
```

Run it after cloning, and again when the patch or the go-text version in
`go.mod` changes. Until then the build fails with "replacement directory
./third_party/typesetting does not exist".

What `gio.patch` does (marked `WazzapClients patch`):

- `gpu/gpu.go`: the renderer clears its list of the frame's image draws
  before reusing it. Truncated, the list kept pointing past its new length
  at the pictures of earlier, busier frames, which stayed reachable until a
  frame drew as many: closing the emoji picker with its animated emojis left
  ~50 MB of their frames in the heap.
- Keyboard use (`internal/ui/keyboard.go`). `io/key`: `key.FocusTrap`, an
  `event.Op` tag that keeps Tab, Shift-Tab and the new `key.MoveFocusCmd`
  among the focusable handlers recorded after it (a menu or dialog), and
  clears a focus outside them. `io/input`: `Source.Focus` tells the focused
  tag, its area in window coordinates (for the focus ring), whether a trap
  is up and whether the last press was a key's; `SyntheticCmd` routes
  made-up events after the frame (Shift+F10 right-clicks the focused
  element). `MoveFocus` no longer panics on a focus that's gone. `widget`:
  `Clickable.NoFocus` keeps scrims out of Tab's way.
- `widget/editor.go`: Ctrl+Backspace and Ctrl+Delete delete a word. Where
  the shortcut modifier is also the one that moves by words (Ctrl, anywhere
  but macOS) the editor took them for a shortcut and did nothing.
- Linux desktop integration (`app/`), for `internal/desktop`:
  `SetDropHandler` hears of files dragged from another program over a
  window and dropped on it, on Wayland (`os_wayland.go`: the data device's
  drag offers, which upstream throws away) and X11 (`os_x11_dnd.go`: XDND).
  `SetActivationToken` and `Perform(system.ActionRaise)` on Wayland bring a
  window to the front through xdg-activation, which upstream doesn't speak
  (`wayland_xdg_activation.c` and `.h` are wayland-scanner's output for
  wayland-protocols' `staging/xdg-activation/xdg-activation-v1.xml`,
  generated like the files beside them, see `os_wayland.go`'s
  `go:generate` lines).
- `app/os_wayland.go`: a window draws only after the frame callback of
  its last frame, configures included (upstream waited only while
  animating). A hidden window (on another workspace) gets no callbacks and
  its buffers aren't released, so the configures it got drew until
  `eglSwapBuffers` blocked, and with it the event loop: the compositor's
  pings went unanswered and Hyprland called the app unresponsive.
- Pasting after a paste that found nothing (`io/input/clipboard.go`,
  `app/os_wayland.go`). A read of an empty clipboard never answers: the
  router kept the editor waiting for it and never asked the driver again,
  and the Wayland driver kept its read marked as under way, so pasting
  stopped working until the app restarted. The router now asks the driver
  on every read, the driver ends a read with nothing to read, and a read
  whose owner never finishes writing gives up after 5 seconds.

What `hypermeow.patch` does (marked `WazzapClients patch`):

- `message.go`: since November 2026 WhatsApp delivers status updates as
  `<status from="status@broadcast">` stanzas with the `<enc>` nodes a
  `<message>` had. hypermeow (and upstream whatsmeow) handled `<status>` as
  a plaintext newsletter update only, and acked and dropped the rest, so no
  status arrived. One with `<enc>` now goes through `handleEncryptedMessage`;
  the node keeps its tag, so the ack is still `class="status"`.
  `status_patch_test.go` checks it. Drop the patch once hypermeow does this.
- `send.go`: `SendRequestExtra.StatusRecipients`. A message to
  `status@broadcast` went to the list the server keeps (`GetStatusPrivacy`),
  which knows only My contacts, My contacts except and Only share with; with
  it set, it goes to those people (and your own devices) alone, like
  WhatsApp's `statusJidList`. A status goes to the audience chosen on the
  phone this way (`statusAudience`), and a like, a reaction, to its poster
  alone (`internal/wa/statuslike.go`).
- `status_patch.go`, `send.go`: a message to `status@broadcast` goes to LIDs
  alone, your own account's included (`statusLIDs`). The server answers a
  list that mixes phone numbers and LIDs with error 400 (whatsmeow issue
  1293), and hypermeow added your own phone number to every list: a like,
  sent to its poster's LID, never went. LIDs come from the store, missing
  ones from the server (one usync); someone without one is left out.
- `send.go`: a `MESSAGE_UNSCHEDULE` protocol message (taking a scheduled
  message back) goes with `edit="7"`, as phones send it. hypermeow sent it
  with no edit attribute.

- `send.go`: a sticker with a sound (`audioStickerMessage`) goes as
  `type="media"` with `mediatype="sticker"`, as phones send it. hypermeow sent
  it as `type="text"`. `audiosticker_patch_test.go` checks it.
- `send.go`, `download.go`: a chat's theme for everyone (`CHAT_THEME_SETTING`)
  goes as `type="media"` with `mediatype="chat_theme"`, without which phones
  drop it, and `MediaChatWallpaper` uploads and downloads its photo.
  `chattheme_patch_test.go` checks it.
- `offline_patch.go`, `client.go`: missed stanzas are asked for in batches
  (`<ib><offline_batch count="200"/></ib>`). The login says pull mode, but
  nothing asked, and the server sent about 20 messages a second (10,000 of them
  took nine minutes). `handlerQueueLoop` counts the stanzas it has handled and
  asks again while two batches or less are left, so at most 600 are under way.
  The handler queue holds 2048 for that (256 upstream): a full one resets the
  connection. `offline_patch_test.go` checks it.

After `go get github.com/polymorfa/hypermeow@main` run `apply.sh` again; if
the hunk no longer applies, regenerate the patch as below.

What `typesetting.patch` does (each change is marked `WazzapClients patch`):

- `font/opentype/tables/glyphs_glyf_src.go` and its users in `font/`: glyphs
  are parsed on demand. Upstream parses a font's whole `glyf` table when it
  loads, about ten times the table size in heap (17 MB for MS Gothic).
- `font/opentype/bytes.go`, `reader.go`: a `BytesReader` resource returns
  tables as subslices instead of copies (the bundled Go fonts and the
  memory-mapped emoji font, see `internal/ui/fonts.go`).
- `fontscan/mmap*.go`, `footprint.go`: system fonts are memory-mapped
  (copy-on-write) instead of read into the heap.
- `font/font.go`, `renderer.go`: `Font.BitmapHook` can hand out another PNG
  for a bitmap glyph. The 3D emoji style (`internal/ui/emojistyle.go`) shows
  its downloaded pictures in place of the 2D font's this way.
- `fontscan/anywhere.go`, `fontmap.go`: a character that no font of the query's
  families (or close to them) has is looked for in every font there is before
  it's drawn as a box. Upstream looks in the fonts of its script, which it
  isn't told (Gio doesn't call `SetScript`): on Linux Thai, mathematical
  letters (𝗯𝗼𝗹𝗱, 𝓼𝓬𝓻𝓲𝓹𝓽) and many symbols were boxes with Noto installed.
  Private use runes still are: icon fonts cost up to 37 MB to open.

To move to another go-text version (Gio decides which one it needs), change
`go.mod`, run `apply.sh`, fix any rejected hunks in `third_party/typesetting`,
and regenerate the patch from a clean copy of the new version:

```sh
cd "$(mktemp -d)" && cp -R "$(go env GOMODCACHE)/github.com/go-text/typesetting@<version>" ts &&
    chmod -R u+w ts && cd ts && git init -q && git add -A && git commit -qm upstream &&
    cp -R <repo>/third_party/typesetting/. . && git add -A &&
    git diff --cached > <repo>/patches/typesetting.patch
```

Gio's patch moves the same way: `gioui.org@<version>` into `third_party/gio`
and `patches/gio.patch`.
