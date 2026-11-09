#!/bin/sh
# Uninstalls what install.sh installed. Chats and the linked account
# (~/.config/WazzapClients) stay unless you say otherwise, so installing
# again doesn't mean linking again.
set -eu

data=${XDG_DATA_HOME:-$HOME/.local/share}
config=${XDG_CONFIG_HOME:-$HOME/.config}
app=$data/WazzapClients
bin=$HOME/.local/bin

# Quit the app first, even while it's only in the tray.
pkill -x wazzapclients > /dev/null 2>&1 || true

rm -rf "$app"
# The link only if it's ours.
if [ -L "$bin/wazzapclients" ] && [ "$(readlink "$bin/wazzapclients")" = "$app/wazzapclients" ]; then
	rm -f "$bin/wazzapclients"
fi
rm -f "$data/applications/wazzapclients.desktop"
rm -f "$data"/icons/hicolor/*/apps/wazzapclients.png

update-desktop-database "$data/applications" > /dev/null 2>&1 || true
# GTK notices new icons by the theme folder's time; a cache someone
# made there would hide them until it's updated, so update it if there's one.
touch "$data/icons/hicolor" 2> /dev/null || true
if [ -f "$data/icons/hicolor/icon-theme.cache" ]; then
	gtk-update-icon-cache -q -t -f "$data/icons/hicolor" > /dev/null 2>&1 || true
fi

echo 'WazzapClients is uninstalled.'

if [ -d "$config/WazzapClients" ] && [ -t 0 ]; then
	printf 'Also delete your chats and log WazzapClients out of WhatsApp on this computer? [y/N] '
	read -r answer || answer=
	case $answer in
	[yY]*)
		rm -rf "$config/WazzapClients"
		echo 'Your chats are deleted.'
		;;
	*) echo "Your chats are kept in $config/WazzapClients, to pick up where you left off if you install it again." ;;
	esac
fi
