# Group status

Open a group, open its info panel, then choose **Status**. The group status page
shows received updates and an **Add group status** row. Use **+** to post text,
photos/videos, or an audio file. The back arrow returns to all statuses.

Updates last 24 hours. Each group has its own thread, separate from personal
statuses and ordinary chat messages. The viewer shows the author, text or caption,
photo, video, or audio, with pause and mute controls. Group status replies are not
yet offered. Audio posting currently selects an existing file; it does not record
from the microphone.

## Protocol and storage

Incoming `groupStatusMessageV2.message` is routed from live events and history
sync to `wz_status`. The group JID, authenticated sender (or attribution when the
sender is unavailable), duration and audio MIME type are retained. Media blobs
keep the original download metadata. Reading a group story uses the group JID and
its author for the read receipt. Expired updates and their cached media are removed.

Outgoing posts use hypermeow's public `SendMessage` API, `groupStatusMessageV2`,
`contextInfo.isGroupStatus`, a message secret and the `meta is_group_status` node
through `SendRequestExtra.AdditionalNodes`. Hypermeow itself is unchanged. Failed
posts are removed from the local status list with an error notice; pending posts
are cleaned on restart.

## Validation

Regression tests cover text, image, voice and video envelopes, downloadable media
metadata, group/personal separation, viewed state, expiry, outgoing serialization,
and attachment/composer destinations across page changes. Local tests do not
establish live WhatsApp acceptance or playback on every platform. End-to-end
posting and receiving with a linked WhatsApp account still require verification.

Preview without a WhatsApp connection:

```sh
go run ./cmd/screenshot -overlay groupstatus -ochat work -out bin/group-status-preview
go run ./cmd/screenshot -overlay groupstatustext -ochat work -out bin/group-status-preview
go run ./cmd/screenshot -overlay groupstatusviewer -ochat work -out bin/group-status-preview
```
