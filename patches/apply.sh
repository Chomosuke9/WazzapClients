#!/bin/sh
# Rebuilds third_party/: each patched module, at the version required in
# go.mod, from the Go module cache, with its patch applied. go.mod replaces
# the modules with those directories, so run this after cloning and
# whenever a patch or a patched module's version changes. See
# patches/README.md.
set -eu
cd "$(dirname "$0")/.."

# Download outside this module: its replaces point at the directories
# being built, which may not exist yet.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# build <module> <directory in third_party> <patch in patches>
build() {
	module=$1
	version=$(sed -n "s|^[[:space:]]*$module \(v[^[:space:]]*\).*|\1|p" go.mod | head -n 1)
	if [ -z "$version" ]; then
		echo "apply.sh: no $module requirement in go.mod" >&2
		exit 1
	fi
	(cd "$tmp" && GOFLAGS=-mod=mod go mod download "$module@$version")
	src="$(go env GOMODCACHE)/$module@$version"
	dst=third_party/$2
	rm -rf "$dst"
	mkdir -p third_party
	cp -R "$src" "$dst"
	chmod -R u+w "$dst"
	patch -s -p1 -d "$dst" < "patches/$3"
	echo "patched $module $version into $dst"
}

build github.com/go-text/typesetting typesetting typesetting.patch
build gioui.org gio gio.patch
