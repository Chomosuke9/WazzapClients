<div align="center">

# ⚡ WazzapClients

**WhatsApp Desktop, without the browser inside it.**

A native WhatsApp client written in Go: no WebView, no Electron, no Chromium.
It aims to look and feel like the official app while using a small fraction of its memory.

[![Build](https://github.com/chomosuke9/wazzapclients/actions/workflows/build.yml/badge.svg)](https://github.com/chomosuke9/wazzapclients/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/chomosuke9/wazzapclients?include_prereleases&sort=semver)](https://github.com/chomosuke9/wazzapclients/releases)
![Go](https://img.shields.io/github/go-mod/go-version/chomosuke9/wazzapclients)
![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20Linux-25D366)

<img src="docs/screenshots/hero.png" alt="WazzapClients in its light and dark themes" width="900">

</div>

---

## Why?

The official desktop app is a whole web browser running one web page. WazzapClients draws
its own UI on the GPU with [Gio](https://gioui.org) and talks to WhatsApp directly through
[hypermeow](https://github.com/polymorfa/hypermeow), a performance-focused fork of
whatsmeow. You get the same app, pixel for pixel where it matters, in one small executable.

- **Native and light.** One Go binary. Caches are capped by size, and after ten idle seconds
  the app hands memory back to the OS.
- **Familiar.** Layouts, sizes and colors are measured against screenshots of the real app,
  so nothing needs relearning.
- **Local.** Messages are kept in a SQLite file on your computer.
- **Stays in the tray.** Closing the window frees its GPU memory, and the connection keeps
  running so notifications still arrive.

## A tour

### Everything you'd expect

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/login.png" alt="Login with a QR code"></td>
    <td width="50%"><img src="docs/screenshots/group-info.png" alt="Group info panel"></td>
  </tr>
  <tr>
    <td><b>Link with a QR code</b>, like WhatsApp Web, and choose how much history to copy from your phone.</td>
    <td><b>Groups and their info panel:</b> pinned messages, documents, voice notes, typing indicators, members and settings.</td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/reactions.png" alt="Message menu with reactions"></td>
    <td><img src="docs/screenshots/search.png" alt="Searching a chat"></td>
  </tr>
  <tr>
    <td><b>Reactions, replies, forwarding, pinning, starring</b>, select mode and deleting, from a right-click.</td>
    <td><b>Search inside a chat</b>, with matches highlighted. Clicking one jumps to it, even far back.</td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/emoji.png" alt="Emoji picker"></td>
    <td><img src="docs/screenshots/gallery.png" alt="Media panel"></td>
  </tr>
  <tr>
    <td><b>Emoji and stickers</b>, including animated ones, with search and recents.</td>
    <td><b>The Media panel:</b> photos, videos, docs and links from every chat, or from just one.</td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/poll.png" alt="A poll and its votes"></td>
    <td><img src="docs/screenshots/msginfo.png" alt="Message info"></td>
  </tr>
  <tr>
    <td><b>Polls</b> you vote in with a click, and who voted for what. Locations, contacts and events show as cards.</td>
    <td><b>Message info:</b> who each of your messages was delivered to and read by, and when.</td>
  </tr>
</table>

### Status, channels and communities

<table>
  <tr>
    <td width="33%"><img src="docs/screenshots/status.png" alt="Status updates"></td>
    <td width="33%"><img src="docs/screenshots/channels.png" alt="Channels"></td>
    <td width="33%"><img src="docs/screenshots/communities.png" alt="Communities"></td>
  </tr>
  <tr>
    <td>View and post <b>status updates</b>: text, photos and videos.</td>
    <td>Follow and read <b>channels</b>.</td>
    <td><b>Communities</b> with their announcements and groups.</td>
  </tr>
</table>

### A photo editor before you send

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/editor.png" alt="Drawing, text and emoji on a photo"></td>
    <td width="50%"><img src="docs/screenshots/filter.png" alt="Photo filters"></td>
  </tr>
  <tr>
    <td>Crop, rotate, draw, add text, shapes, emoji and stickers, or blur part of the picture.</td>
    <td>Filters, and a choice of Standard or HD quality. Pick files, paste them or drop them on the window.</td>
  </tr>
</table>

### Things WhatsApp doesn't do

These are all **off until you turn them on** in *Settings > Extra features*. They only use what
WhatsApp already lets every linked device do, and they run from your own account.

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/slash.png" alt="Slash command picker"></td>
    <td width="50%"><img src="docs/screenshots/calc.png" alt="The /calc command"></td>
  </tr>
  <tr>
    <td><b>Slash commands, like Discord.</b> Type <code>/</code> to manage a group you run without opening a single menu.</td>
    <td><b><code>/calc</code></b> works out the answer as you type and picks up the amounts in the message you reply to. Splitting a bill takes one line.</td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/scheduled.png" alt="Scheduled messages and AFK"></td>
    <td><img src="docs/screenshots/edits.png" alt="Edit history"></td>
  </tr>
  <tr>
    <td><b><code>/schedule</code></b> sends a message later, even with the window closed. <b><code>/afk</code></b> replies for you while you're away.</td>
    <td><b>Edit history</b> shows what an edited message said before each edit.</td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/privacy.png" alt="Privacy mode"></td>
    <td><img src="docs/screenshots/ghost.png" alt="Ghost mode"></td>
  </tr>
  <tr>
    <td><b>Privacy mode</b> for when others can see your screen: names and messages turn into bars and pictures blur until you point at them. Ctrl+Shift+P or the eye in the title bar. <b>Block screen recording</b> keeps the window out of captures.</td>
    <td><b><code>/ghost</code></b> reads chats and statuses invisibly: no read receipts, shown offline, and nothing sent from you until you turn it off.</td>
  </tr>
</table>

A page of its own, *Ethically gray features*, holds the ones that show more than the sender
meant you to see. Each has its own switch:

- 🗑️ **Keep deleted messages** keeps messages and statuses deleted for everyone, marked Deleted.
- 👁️ **Replay view once** opens view once messages as often as you like, screenshots allowed.
- 👻 **`/ghost`** and ✏️ **Edit history**, above.
- **`/whisper @member @member <text>`** submits group text to the selected members' devices only (experimental).

Turning one on opens a warning about privacy expectations. You must check the acknowledgment
and choose **Enable feature** before it is activated. Turning it off is immediate; turning it
back on asks again. `/whisper` requires a second confirmation with a separate
acknowledgment and the warning **"This feature can easily get your account banned."** The switch
stays off until both confirmations are completed; canceling either leaves it off.

<details>
<summary><b>All the slash commands</b></summary>

| Command | What it does |
| --- | --- |
| `/add <contact>` | Adds people to the group |
| `/kick <member>` | Removes members from the group |
| `/promote <member>` / `/demote <member>` | Makes members admins, or dismisses admins |
| `/link` | Shows the group's invite link |
| `/lockdown on\|off` | Lets only admins send messages, or everyone again |
| `/description <text>` | Changes the group description |
| `/sticker <top text>` | Turns a photo into a sticker, meme text included; a new line (Shift+Enter) starts the bottom text |
| `/purge <count>` | Deletes your last messages for everyone |
| `/raffle <winners>` | Draws group members at random and announces them |
| `/calc <sum>` | A calculator: `12 x 4500`, `15k x 3`, `15% x 80000` |
| `/schedule <when> <message>` | Sends later: `21:00`, `2h`, `tomorrow 08:00`, `fri 18:00` |
| `/scheduled` | Lists the chat's scheduled messages, to send now or cancel |
| `/afk <reason>` | Auto-replies while you're away, until you send something |
| `/ghost` | Goes invisible: no read receipts, shown offline, no sending (an ethically gray feature) |
| `/whisper @member @member <text>` | Sends text to selected group members using individual device envelopes (an ethically gray feature) |
| `/snippet send <name>` | Sends a saved message; the picker over the composer offers them as you type |
| `/snippet save [name]` | Saves the whole message you reply to (a poll, document or photo too), under that name or a generated one |
| `/catch` | Opens the stored payload of the message you reply to, with Copy JSON and Export JSON |

To use `/whisper`, enable **Slash commands** and its own switch under **Extra features >
Ethically gray features**, acknowledge the warning, then pick one or more members and
type the message, for example `/whisper @Budi @Siti Meet at 8`. The first non-mention
starts the message body, where `@mentions` work as in any message. To start the body
with a mention, put it on a new line (Shift+Enter) after the members: `/whisper @Budi`,
then `@Siti is here` below. It supports text
only and respects admin-only groups and ghost mode. Recipients may still copy, quote,
or forward what they receive.

A sent whisper leaves a record in the chat, drawn as yours but tinted apart from an
ordinary message and headed **"Only you can see this"** with the members it went to.
That record is local to this device only: it is stored here, never broadcast to the
group and never synced to your other devices.

Whisper uses hypermeow's internal, retry-shaped individual envelopes, not a supported
WhatsApp selective-group feature. The record reports submission, not confirmed delivery.
It is not sent as an ordinary group message and does not retry automatically or fall
back to a group broadcast. A partial failure may leave some devices with the message;
manually resending can duplicate it. Live recipient interoperability has not been
verified; local tests exercise routing, validation, and failure handling.

Manage saved messages in **Settings > Snippets**: search, add text or message JSON,
edit, rename, and delete. Snippets belong to the current account. Their text (and a
payload's text and captions) can use variables, filled in when you send: `{name}` (the
contact, or in a group the author of the message you reply to), `{first}`, `{mention}`
(@mentions the author of the message you reply to; without a reply, the chat by its name,
like `@admin` but notifying no one), `{chat}`,
`{me}`, `{quote}`, `{greeting}`, `{date}`, `{time}` and `{day}`. Unknown `{...}` stays as
it is, and `\{name}` sends `{name}` literally. Saving a media snippet downloads a separate
copy for reuse; sending it uploads that copy again. A snippet sent while you reply goes as a reply.

`/catch` shows a local result; it does not send the payload to the chat. Raw payloads
are stored in the existing message database for newly received, synced, and outgoing
messages. Older messages may not have one. JSON includes fields understood by the
pinned protocol schema; unknown protobuf fields remain in the database. An edited
message exposes its latest available edit content. Deleted payloads follow the
existing deletion preferences, and view once export requires Replay view once.

Group commands work in groups where you're an admin. You'll also find **`@admin`**, which
mentions every admin of a group at once, and **Raw quality**, which sends JPEG and PNG
photos exactly as they are.

</details>

### And plenty more

- 🌗 Light and dark themes, with WhatsApp's doodle wallpaper
- 👥 Several linked accounts. Only the open one stays connected, which keeps memory low
- 🔔 System notifications, with Reply and Mark as read on Windows
- 📝 Drafts that stay with each chat, @mentions, and a formatting toolbar (bold, italic, strikethrough, code)
- 🎙️ Voice messages and videos played by the system's own decoders (no bundled codecs)
- 📌 Pinned, archived and muted chats, Favourites and custom lists, disappearing messages, chat themes
- 📊 Polls with Hide voter names and an end time when you create one
- 🔗 Link previews, sent and received, with WhatsApp's big picture; group invite links, albums and documents
- 1️⃣ View once messages, opened once, with screenshots blocked while one is shown
- 🔍 Font size from 80% to 200% (*Settings > General*, or Ctrl with +, - and 0)
- 🅰️ Initials on a color for people without a profile picture, and clickable @mentions
- ⬆️ Built-in updates from signed GitHub releases (*Settings > Help > Check for updates*)

## Download

Prebuilt packages for **Windows** (amd64) and **Linux** (amd64) are on the
[Releases](https://github.com/chomosuke9/wazzapclients/releases) page and attached to every CI run.

- `WazzapClients-Setup.exe` installs the app for your user only. It doesn't need admin rights.
- `WazzapClients-windows-amd64.exe` is the same app without an installer.

To update, use **Settings > Help > Check for updates**, which installs the new release in place.
See [docs/releasing.md](docs/releasing.md) for how releases are made and signed.

## Try it without an account

Want to look around first? The demo mode starts the app with made-up chats and no network.
All the screenshots above come from it.

```sh
go run ./cmd/wazzap -demo
```

## Build from source

You need the Go version in `go.mod`. On Linux, first install Gio's
[system dependencies](https://gioui.org/doc/install/linux).

```sh
sh patches/apply.sh            # once after cloning: builds the patched go-text (see patches/)
go run ./cmd/wazzap            # run the app and link it with a QR code
go run ./cmd/wazzap -demo      # made-up chats, no network
go run ./cmd/wazzap -debug     # log protocol traffic
go run ./cmd/wazzap -background  # start in the tray, without a window
go build ./...
```

Your session and messages are stored in `%AppData%\WazzapClients\wazzap.db` on Windows,
and in the user config directory on other platforms.

## How it fits together

```
cmd/wazzap/        the desktop app
cmd/screenshot/    renders the UI headlessly to PNG, or next to a real WhatsApp screenshot
cmd/memprobe/      memory and frame-time benchmark (Windows)
internal/model/    what the UI sees: chats, messages, events, and the Backend interface
internal/ui/       the Gio interface, from the chat list to the photo editor
internal/wa/       hypermeow backend and the SQLite message store
internal/mock/     the demo backend
internal/command/  slash commands
internal/auto/     scheduled messages and AFK replies
internal/sticker/  makes stickers, with its own small lossless WebP encoder
internal/video/    plays video and audio with the OS's decoder
internal/notify/   system notifications
internal/desktop/  tray icon, start at login, single instance
internal/update/   signed self-updates
```

The UI never touches protocol types. `internal/wa` turns hypermeow events into
`internal/model` events, and the UI drains them on its own goroutine, so UI state needs no locks.

## Contributing

Read [AGENTS.md](AGENTS.md) before changing code. It covers the conventions, the gotchas
found in the pinned Gio version, and the screenshot tooling used to match WhatsApp Desktop.
Low memory use is the reason this project exists, so measure with `cmd/memprobe` before and
after a change. Run `gofmt`, `go vet ./...` and `go build ./...` before you send it.

## Disclaimer

This is an unofficial client. It isn't affiliated with or endorsed by WhatsApp or Meta.
Using a third-party client may break WhatsApp's Terms of Service, so use it at your own risk.
