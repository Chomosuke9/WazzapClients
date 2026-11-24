# Writing your own slash commands

A command is a JavaScript file (`.js`). Turn on **Settings > Extra features > Slash
commands**, add the file under **Your commands** there, and it's in the `/` picker. A
command can do anything with your account, so look inside a file before you add it: what it
does is your responsibility. The app keeps a copy, so add the file again after changing it.
Examples are in [`commands/`](commands).

This page assumes you know [Baileys](https://github.com/WhiskeySockets/Baileys). If you do,
read [what differs from it](#what-differs-from-baileys) before porting anything.

Any editor will do. In VS Code, put [`wazzap.d.ts`](commands/wazzap.d.ts) beside your file
and start the file with `/// <reference path="wazzap.d.ts" />`: it then completes
everything below.

## A template

```js
command({
  name: "order",
  description: "Sends an order",
  options: [
    { type: "number", name: "qty", required: true, min: 1, max: 99 },
    { type: "text", name: "item", required: true },
  ],
  text: "*Order*\n{qty} × {item}",
});
```

`{option}` is filled in, as are `{name}`, `{first}`, `{chat}`, `{me}`, `{quote}`,
`{greeting}`, `{date}`, `{time}` and `{day}`, the variables snippets have. An option wins
over a variable of the same name, and one left out is empty. `\{` writes a brace (in a
JavaScript string, `"\\{"`); anything else in braces is sent as it is.

## A script

```js
command({
  name: "roll",
  description: "Rolls a die",
  async run(ctx) {
    await ctx.send(`🎲 ${1 + Math.floor(Math.random() * 6)}`);
  },
});
```

`command()` takes `name` (lowercase letters, digits and `_`, at most 24, and not one of the
app's own), `description`, `options`, `text` or `run`, `group` (only in groups) and `admin`
(only where you're an admin). All but `options`, `group` and `admin` are required, and any
other key is an error.

| Option | Value in `ctx.args.<name>` |
| --- | --- |
| `{ type: "text" }` | The rest of the line; must be last |
| `{ type: "number", min, max }` | A whole number; `min` is 0 unless you give it |
| `{ type: "choice", choices }` | One of `choices`: 1 to 25 words without spaces |
| `{ type: "member", multiple }` | A member, or a list of them with `multiple`; groups only |

One left out is `null`, or `[]` with `multiple`. Each has a `name`, and may have a
`description` and `required`; the required ones come first, and there are at most 10. A
member has `id`, `name`, `mention` (put it in a message to mention them), `admin` and `me`.

| `ctx` | |
| --- | --- |
| `await ctx.send(text)` | Sends a message in this chat, as a reply if you were replying, and gives it; at most 4096 bytes (`sock.sendMessage` takes 64 KB) |
| `await ctx.members()` | The group's members |
| `await ctx.ask(text, buttons)` | Shows a note with 1 to 5 buttons; the one clicked, or `null` when dismissed |
| `await ctx.note(text)` | Shows a note only you see |
| `await ctx.copy(text)` | Puts text on the clipboard |
| `ctx.quoted` | The message you reply to (`text`, `sender`, `fromMe`...), or `null` |
| `ctx.chat` | `id`, `name` and `group` |

There are also `await sleep(ms)`, `setTimeout`, `clearTimeout` and `console.log`, whose lines
show in a note once the command ends. `await` everything that has it above: an error then
says its line, and `try { ... } catch (e) { ... }` catches it.

## The language

The engine is [goja](https://github.com/dop251/goja), not Node: modern JavaScript, `async`
and `await`, private fields, `BigInt`, `Proxy` and Unicode regular expressions included. It
doesn't have:

- `require`, `import` and `export`, `await` outside a function, or async generators (`async function*`).
- Anything of Node's or a browser's: `fetch`, `Buffer`, `process`, `crypto`, `URL`, `atob`,
  `TextEncoder`, `structuredClone`, `setInterval`, `setImmediate`, `queueMicrotask`.
- `Intl`. Don't give a number's `toLocaleString` a locale: it takes it for a radix and throws
  a `RangeError` (a date's ignores it). Use `String(n).replace(/\B(?=(\d{3})+$)/g, ".")` for 1.234.567.

## The whole account: `ctx.sock`

`ctx.sock` has Baileys' names, and can do nearly anything you can.

```js
async run(ctx) {
  const { sock, chat } = ctx;
  const m = await sock.sendMessage(chat.id, { text: "Hi @628123", mentions: ["628123@s.whatsapp.net"] });
  await sock.sendMessage(chat.id, { react: { text: "🔥", key: m.key } });
  await sock.relayMessage(chat.id, { extendedTextMessage: { text: "raw" } }, { quoted: m });
}
```

All but `user` need `await`.

| | |
| --- | --- |
| `sendMessage(jid, content, { quoted })` | Sends a message (below) and gives it. `react`, `edit`, `delete` and `pin` give the message they were done to |
| `relayMessage(jid, message, { quoted, messageId })` | Sends any `waE2E.Message`, in Baileys' spelling or whatsmeow's; bytes are base64 or a `Uint8Array` |
| `getMessage(message)` | A message's raw protobuf, as JSON has it |
| `fetchMessages(jid, limit)` | A chat's newest stored messages, oldest first: 20, or `limit` (at most 500) |
| `chats()`, `contacts()`, `user` | Your chats, your contacts, and you (`id`, `lid`, `name`) |
| `onWhatsApp(...phones)` | `[{ jid, exists, name }]`, for at most 50 numbers |
| `pickFile(title)` | Asks you for a file to send, or `null` |
| `readMessages(keys)` | Marks the chats they're in as read; takes jids too |
| `chatModify(changes, jid)` | `{ archive: true }`, `pin`, `markRead`, `mute` (milliseconds, or `null`) and `star` |
| `star(jid, messages, star)` | Stars messages |
| `updateBlockStatus(jid, action)` | `"block"` or `"unblock"` |
| `groupMetadata(jid)` | `id`, `subject`, `desc`, `announce`, `restrict`, `creation`, `size`, `participants` |
| `groupParticipantsUpdate(jid, participants, action)` | `"add"`, `"remove"`, `"promote"` or `"demote"`; gives `[{ jid, name, status, error }]` |
| `groupUpdateSubject(jid, text)`, `groupUpdateDescription(jid, text)` | |
| `groupSettingUpdate(jid, setting)` | `"announcement"`, `"not_announcement"`, `"locked"`, `"unlocked"` |
| `groupInviteCode(jid)`, `groupRevokeInvite(jid)`, `groupLeave(jid)` | |

`sendMessage`'s content is one of these:

| Content | |
| --- | --- |
| `{ text }` | With `mentions` (a list of jids) and `viewOnce` |
| `{ image: file }`, `video`, `audio`, `document` | A file from `pickFile()`. `caption` and `mentions` with all but `audio`; `viewOnce` with `image` and `video`; `gifPlayback` with `video` |
| `{ sticker: message }` | A sticker that was sent before |
| `{ react: { text: "🔥", key: m.key } }` | Empty text takes it back |
| `{ edit: m.key, text }`, `{ delete: m.key }` | `edit` takes `mentions`; `delete` is for everyone |
| `{ forward: message }` | |
| `{ poll: { name, values, selectableCount } }` | 2 to 12 values; one answer only if `selectableCount` is 1 |
| `{ location: { degreesLatitude: -6.2, degreesLongitude: 106.8 } }` | |
| `{ contacts: [jid, ...] }` | |
| `{ pin: m.key, type: 1, time: seconds }` | `type` 2 unpins; 7 days unless you give `time` |

A message has `key` (`remoteJid`, `id`, `fromMe`, `participant`), `text`, `sender`,
`senderId`, `fromMe`, `time` (Unix seconds), `media` and `fileName`. `media` is `""`,
`"image"`, `"video"`, `"gif"`, `"ptt"`, `"audio"`, `"document"`, `"sticker"`, `"location"`,
`"contact"`, `"poll"`, `"event"` or `"stickerPack"`. A view once message's `text` is empty.
Where a message is wanted, its key does too.

## What differs from Baileys

- **No events.** A command runs when you type it, and ends. There are no raw nodes, no
  `sock.ev`, no downloads, and no logging out.
- **Messages aren't `WAMessage`s.** They have the fields above, not `message`, `pushName` or
  `messageTimestamp`. `getMessage` gives the protobuf, whose enums are names (`"REVOKE"`),
  64-bit numbers strings and bytes base64. `relayMessage` takes those back as they are, and
  enums as numbers too.
- **`relayMessage` gives a message**, where Baileys gives the ID: it's `.key.id`, even of one
  without a bubble, such as an album's header. Besides `quoted` it takes `messageId` (1 to 64
  printable characters, no spaces), but no `additionalNodes`, `participant` or `statusJidList`.
- **Media comes only from `pickFile()`**: no `{ url }`, buffer or stream. The app works out
  the type, name and thumbnail, so `mimetype`, `fileName`, `jpegThumbnail`, `ptt` and `ptv`
  are errors, and so is any other key that isn't listed above.
- **`contacts` is a list of jids**, not vCards. For a vCard of your own, relay a `contactMessage`.
- **A send resolves once it's queued**, not once WhatsApp has it. A message can be edited only
  after that, so editing one right after sending it fails: `await sleep(1000)` first, or more
  on a slow connection.
- **Failures.** What can't be done throws, with three exceptions. A send made offline (text, a
  file, a poll, contacts, a location or a relayed message with a bubble) still gives its
  message, which then fails in the app. `readMessages` does nothing offline. And what WhatsApp
  turns down later shows only in the app. In ghost mode sends, reactions, edits, deletes and
  pins throw; group changes don't.
- **LIDs.** A chat with a person has their LID as its `id` and `remoteJid` when it's known,
  and a member's `id` often is one. Functions take either form, but comparing with a
  `@s.whatsapp.net` jid is false for the same person: run the number through `onWhatsApp`, or
  use `user.lid` besides `user.id` for yourself. Neither of those has a device part.
- **`key.participant`** is there in chats with a person too.
- **`onWhatsApp`** gives numbers that aren't on WhatsApp too, with `exists: false` and `jid: null`.
- **`readMessages`** reads whole chats, as the app does.
- **`groupParticipantsUpdate`** gives `status` `"200"` or `"error"`, and why in `error`.
- **`chatModify`** takes no `delete` or `clear`, and needs no `lastMessages`.
- **Only known messages** can be quoted, edited, reacted to and so on: ones stored here, or
  that this run was given. A key made up for another throws.
- **`status@broadcast`** isn't a chat here.

## Group statuses

```js
const s = await sock.relayMessage(groupJid, { groupStatusMessageV2: { message: { extendedTextMessage: { text: "Hi" } } } });
await sock.sendMessage(groupJid, { delete: s.key });
```

The app adds the `is_group_status` meta node, the message secret and the `contextInfo` a
status needs, and keeps it with the statuses rather than among the chat's messages. `delete`
also takes back someone else's where you're an admin. Don't relay a bare `protocolMessage`
REVOKE for one: WhatsApp takes it for a chat message's, and the status stays up.

## Limits

A file is at most 64 KB. A run sends at most 30 messages (`ctx.send`, `relayMessage` and every
`sendMessage`, even a `react`, `edit`, `delete` or `pin`, or one that fails later), makes at
most 500 calls, goes on at most 10 seconds without waiting for anything, and stops after 5
minutes, the time you take to answer `ask` or `pickFile` included. Scripts reach no files and
no network. Their memory isn't capped: a hostile one could use enough to crash the app.
