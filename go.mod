module github.com/chomosuke9/wazzapclients

go 1.27.0

require (
	gioui.org v0.10.3
	github.com/dop251/goja v0.0.0-20261122185237-35fdc4b88e39
	github.com/go-text/typesetting v0.3.5
	github.com/godbus/dbus/v5 v5.2.4
	github.com/polymorfa/hypermeow v0.0.0-20261121205317-6e3f26d9b53f
	github.com/tc-hib/winres v0.3.1
	golang.org/x/exp v0.0.0-20261124000051-d6646c509b6d
	golang.org/x/image v0.48.0
	golang.org/x/net v0.63.0
	golang.org/x/sys v0.50.0
	google.golang.org/protobuf v1.36.12
	modernc.org/sqlite v1.63.0
	rsc.io/qr v0.2.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	gioui.org/shader v1.0.9 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/dlclark/regexp2/v2 v2.13.2 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/google/pprof v0.0.0-20261118010117-084b33a85f63 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/petermattis/goid v0.0.0-20261120131106-3f141f74aebc // indirect
	github.com/polymorfa/libsignal-protocol-go v0.2.3-0.20260806162910-a2adef2e8a11 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rs/zerolog v1.35.1 // indirect
	go.mau.fi/util v0.11.1 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/exp/shiny v0.0.0-20261124000051-d6646c509b6d // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace github.com/go-text/typesetting => ./third_party/typesetting

replace gioui.org => ./third_party/gio

replace github.com/polymorfa/hypermeow => ./third_party/hypermeow
