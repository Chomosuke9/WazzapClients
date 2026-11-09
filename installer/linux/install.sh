#!/bin/sh
# Installs WazzapClients for the current user only, like the Windows
# installer: no root, and a folder the app's own updates (internal/update,
# which replaces the executable in place) can write to.
#
#   ~/.local/share/WazzapClients/wazzapclients   the app (and uninstall.sh)
#   ~/.local/bin/wazzapclients                   a link to it
#   ~/.local/share/applications/wazzapclients.desktop
#   ~/.local/share/icons/hicolor/<size>/apps/wazzapclients.png
#
# Running it again installs over the last install; chats stay.
set -eu

if [ "$(id -u)" = 0 ]; then
	echo 'Run install.sh as yourself, not as root: WazzapClients installs for one user.' >&2
	exit 1
fi

src=$(cd "$(dirname "$0")" && pwd)
data=${XDG_DATA_HOME:-$HOME/.local/share}
app=$data/WazzapClients
bin=$HOME/.local/bin
# The executable's name is also the window's app ID on Wayland and X11
# (Gio's app.ID), which the .desktop file's name and StartupWMClass match.
exe=$app/wazzapclients

mkdir -p "$app" "$bin" "$data/applications"

# Copy, then rename over the old one: that works while it's running.
cp "$src/wazzapclients" "$exe.install"
chmod 755 "$exe.install"
mv -f "$exe.install" "$exe"
rm -f "$exe.new" "$exe.old"

cp "$src/uninstall.sh" "$app/uninstall.sh"
chmod 755 "$app/uninstall.sh"
cp "$src/LICENSE" "$src/THIRD_PARTY_NOTICES.md" "$app/"

ln -sfn "$exe" "$bin/wazzapclients"

for icon in "$src"/icons/wazzapclients-*.png; do
	px=${icon##*-}
	px=${px%.png}
	dir=$data/icons/hicolor/${px}x${px}/apps
	mkdir -p "$dir"
	cp "$icon" "$dir/wazzapclients.png"
done

sed "s|@EXEC@|$exe|g" "$src/wazzapclients.desktop" > "$data/applications/wazzapclients.desktop"

# Menus pick the new entry and icons up sooner with these, where they exist.
update-desktop-database "$data/applications" > /dev/null 2>&1 || true
# GTK notices new icons by the theme folder's time; a cache someone
# made there would hide them until it's updated, so update it if there's one.
touch "$data/icons/hicolor" 2> /dev/null || true
if [ -f "$data/icons/hicolor/icon-theme.cache" ]; then
	gtk-update-icon-cache -q -t -f "$data/icons/hicolor" > /dev/null 2>&1 || true
fi

echo "WazzapClients is installed in $app."
echo 'Open it from the app menu, or run: wazzapclients'
case ":$PATH:" in
*":$bin:"*) ;;
*) echo "($bin isn't on your PATH, so the command needs the full path: $bin/wazzapclients)" ;;
esac
echo "It updates itself. To uninstall, run: $app/uninstall.sh"
