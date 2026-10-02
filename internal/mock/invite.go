package mock

import "github.com/chomosuke9/wazzapclients/internal/model"

// Demo invite links, posted in the "Alumni TI 2019" chat: one group needs
// an admin's approval, the other lets you straight in.
const (
	inviteReuni  = "DemoInviteReuni"
	inviteFutsal = "DemoInviteFutsal"
)

// invitePreview describes the demo group of an invite code, or nil.
func (b *Backend) invitePreview(code string) *model.GroupPreview {
	now := b.now()
	switch code {
	case inviteReuni:
		return &model.GroupPreview{ID: "reuni@g.us", Name: "🎓 Panitia Reuni TI 2019 🎉",
			Description: "*REUNI AKBAR TI 2019*\n\nTempat koordinasi panitia reuni: venue, konsumsi, dokumentasi dan " +
				"undangan. Rapat online tiap Kamis jam 8 malam, notulen dibagikan di sini. Yang belum isi form " +
				"kesediaan, isi dulu ya sebelum masuk.",
			Created: now.AddDate(0, -2, -3), Size: 24, Faces: []string{"budi", "clara", "dewi", "sari"}, Approval: true}
	case inviteFutsal:
		return &model.GroupPreview{ID: "futsal@g.us", Name: "Futsal Kamis Malam", Created: now.AddDate(-1, 0, 0),
			Size: 9, Faces: []string{"budi", "andre"}, Member: b.chat("futsal@g.us") != nil}
	}
	return nil
}

// GroupInvite answers with a demo group for the demo links.
func (b *Backend) GroupInvite(code string) {
	g := b.invitePreview(code)
	if g == nil {
		b.emit(model.InviteEvent{Code: code, Err: "This invite link is invalid."})
		return
	}
	b.emit(model.InviteEvent{Code: code, Group: g})
}

// JoinGroup asks to join the group that needs approval, and joins the
// other one.
func (b *Backend) JoinGroup(code string) {
	g := b.invitePreview(code)
	switch {
	case g == nil:
		b.emit(model.JoinedEvent{Code: code, Err: "This invite link is invalid."})
	case g.Approval:
		b.emit(model.JoinedEvent{Code: code, ChatID: g.ID, Requested: true})
	default:
		if b.chat(g.ID) == nil {
			b.chats = append(b.chats, &model.Chat{ID: g.ID, Name: g.Name, IsGroup: true, Time: b.now()})
		}
		b.emitChat(g.ID)
		b.emit(model.JoinedEvent{Code: code, ChatID: g.ID})
	}
}

// inviteLink is the full link of a demo invite code.
func inviteLink(code string) string { return "https://chat.whatsapp.com/" + code }
