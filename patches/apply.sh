#!/bin/sh
# Rebuilds third_party/typesetting: the go-text version required in go.mod,
# from the Go module cache, with typesetting.patch applied. go.mod replaces
# the module with that directory, so run this after cloning and whenever
# the patch or the go-text version changes. See patches/README.md.
set -eu
cd "$(dirname "$0")/.."

module=github.com/go-text/typesetting
version=$(sed -n "s|^[[:space:]]*$module \(v[^[:space:]]*\).*|\1|p" go.mod | head -n 1)
if [ -z "$version" ]; then
	echo "apply.sh: no $module requirement in go.mod" >&2
	exit 1
fi

# Download outside this module: its replace points at the directory being
# built, which may not exist yet.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
(cd "$tmp" && GOFLAGS=-mod=mod go mod download "$module@$version")
src="$(go env GOMODCACHE)/$module@$version"

dst=third_party/typesetting
rm -rf "$dst"
mkdir -p third_party
cp -R "$src" "$dst"
chmod -R u+w "$dst"
patch -s -p1 -d "$dst" < patches/typesetting.patch
echo "patched $module $version into $dst"
