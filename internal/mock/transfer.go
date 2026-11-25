package mock

import (
	"slices"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The demo moves no files: its transfers are the ones tests and
// cmd/screenshot make up.

// DemoTransfer shows t going on, or further along.
func (b *Backend) DemoTransfer(t model.Transfer) {
	b.endTransfer(t.ChatID, t.MsgID)
	b.transfers = append(b.transfers, t)
	b.emit(model.TransferEvent{Transfer: t})
}

// DemoTransfers adds a video in every state its file can be in to a chat:
// waiting, not here, on its way and here, then a document and an album on
// theirs. It returns their IDs.
func (b *Backend) DemoTransfers(chatID string) (ids []string) {
	if b.files == nil {
		b.files = make(map[string]bool)
	}
	put := func(m *model.Message) string {
		m.ChatID, m.Time, m.Receipt = chatID, b.now(), model.Sent
		if m.Media != model.MediaDocument {
			m.Kind = model.KindImage
		}
		b.add(m)
		b.emit(model.MessageEvent{Msg: deepCopy(m)})
		ids = append(ids, m.ID)
		return m.ID
	}
	video := func(out bool) *model.Message {
		return &model.Message{FromMe: out, Media: model.MediaVideo, Duration: 797, HD: true, FileSize: 42_300_000,
			ImageA: 0x33343a, ImageB: 0x24252b}
	}
	const mb = 1 << 20
	b.DemoTransfer(model.Transfer{ChatID: chatID, MsgID: put(video(true)), Upload: true})
	put(video(false))
	plain := video(false)
	plain.HD = false
	put(plain)
	b.DemoTransfer(model.Transfer{ChatID: chatID, MsgID: put(video(false)), Done: 13 * mb, Total: 100 * mb, Left: 16 * time.Second})
	b.DemoTransfer(model.Transfer{ChatID: chatID, MsgID: put(video(true)), Upload: true, Done: 71 * mb, Total: 100 * mb, Left: 4 * time.Second})
	b.files[chatID+"/"+put(video(false))] = true
	b.DemoTransfer(model.Transfer{ChatID: chatID, MsgID: put(&model.Message{Media: model.MediaDocument, Text: "Recording.zip",
		FileName: "Recording.zip", FileSize: 120_400_000, FileType: "application/zip"}), Done: 42 * mb, Total: 100 * mb, Left: 95 * time.Second})
	for i := range 4 {
		m := video(true)
		m.Album = "demo-transfers"
		m.Media = model.MediaImage
		if i == 1 {
			m.Media = model.MediaVideo
		}
		t := model.Transfer{ChatID: chatID, MsgID: put(m), Upload: true}
		if i < 2 {
			t.Done, t.Total, t.Left = int64(30*(i+1))*mb, 100*mb, 8*time.Second
		}
		if i < 3 {
			b.DemoTransfer(t)
		}
	}
	return ids
}

func (b *Backend) endTransfer(chatID, msgID string) bool {
	n := len(b.transfers)
	b.transfers = slices.DeleteFunc(b.transfers, func(t model.Transfer) bool { return t.ChatID == chatID && t.MsgID == msgID })
	return len(b.transfers) < n
}

func (b *Backend) Transfers() []model.Transfer { return slices.Clone(b.transfers) }

// CancelTransfer ends a transfer like the real backend: a message being
// uploaded goes with it.
func (b *Backend) CancelTransfer(chatID, msgID string) {
	i := slices.IndexFunc(b.transfers, func(t model.Transfer) bool { return t.ChatID == chatID && t.MsgID == msgID })
	if i < 0 {
		return
	}
	t := b.transfers[i]
	b.endTransfer(chatID, msgID)
	b.emit(model.TransferEvent{Transfer: t, End: true})
	if !t.Upload {
		b.emit(model.MediaEvent{ChatID: chatID, MsgID: msgID, Failed: true})
		return
	}
	b.msgs[chatID] = slices.DeleteFunc(b.msgs[chatID], func(m *model.Message) bool { return m.ID == msgID })
	b.emit(model.DeletedEvent{ChatID: chatID, IDs: []string{msgID}})
}
