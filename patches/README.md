# Dependency patches

`typesetting.patch` patches go-text (`github.com/go-text/typesetting`, Gio's
text library) to use far less memory for fonts, and lets the app swap emoji
pictures. `gio.patch` patches Gio (`gioui.org`) so it lets go of pictures it
drew. `go.mod` replaces the modules with `third_party/typesetting` and
`third_party/gio`, which `apply.sh` builds from the Go module cache and are
not checked in:

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
