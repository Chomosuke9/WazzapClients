# Dependency patches

`typesetting.patch` patches go-text (`github.com/go-text/typesetting`, Gio's
text library) to use far less memory for fonts, and lets the app swap emoji
pictures. `gio.patch` patches Gio (`gioui.org`) so it lets go of pictures it
drew. `hypermeow.patch` patches hypermeow (`github.com/polymorfa/hypermeow`)
so it decrypts status updates. `go.mod` replaces the modules with
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

What `hypermeow.patch` does (marked `WazzapClients patch`):

- `message.go`: since November 2026 WhatsApp delivers status updates as
  `<status from="status@broadcast">` stanzas with the `<enc>` nodes a
  `<message>` had. hypermeow (and upstream whatsmeow) handled `<status>` as
  a plaintext newsletter update only, and acked and dropped the rest, so no
  status arrived. One with `<enc>` now goes through `handleEncryptedMessage`;
  the node keeps its tag, so the ack is still `class="status"`.
  `status_patch_test.go` checks it. Drop the patch once hypermeow does this.

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
