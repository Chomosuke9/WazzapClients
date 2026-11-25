package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"github.com/go-text/typesetting/fontscan"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestRobotoWithoutSegoe checks that without Segoe UI text comes from the
// bundled Roboto in the weights and slants the UI uses, before another
// font the system has, and that its lines are as tall as Segoe UI's.
func TestRobotoWithoutSegoe(t *testing.T) {
	for _, f := range []font.Font{{}, {Weight: font.Medium}, {Weight: font.Bold}, {Style: font.Italic}, {Style: font.Italic, Weight: font.Bold}} {
		fm := fontscan.NewFontMap(nil)
		for i, f := range bundledFonts() {
			fm.AddFace(f.Face.Face(), fontscan.Location{File: fmt.Sprintf("font-%d", i)}, opentype.FontToDescription(f.Font))
		}
		want := opentype.FontToDescription(f).Aspect
		fm.SetQuery(fontscan.Query{Families: strings.Split(string(typeface), ", "), Aspect: want})
		for _, r := range "xÉ9#Жλẽ" {
			face := fm.ResolveFace(r)
			family, aspect := fm.FontMetadata(face.Font)
			if family != "roboto" || aspect.Weight != want.Weight || aspect.Style != want.Style {
				t.Errorf("%c of %+v is drawn with %s %+v", r, f, family, aspect)
			}
		}
		face := fm.ResolveFace('x')
		ext, _ := face.FontHExtents()
		if h := (ext.Ascender - ext.Descender) / float32(face.Upem()); h < 1.32 || h > 1.34 {
			t.Errorf("%+v: lines are %.3f em tall, want Segoe UI's 1.33", f, h)
		}
	}
}

// TestArchivedRow checks that the chat list begins with the Archived row
// under All, that it opens the Archived page (which ignores the chips and
// the search), and that the list stays where it is as the row goes.
func TestArchivedRow(t *testing.T) {
	u, _, frame, _ := chatListUI(t)
	s := &u.sidebar
	if !s.archivedRow || u.listRow(0) != 1 {
		t.Fatal("no Archived row with an archived chat")
	}
	u.pickChip(u.chipItems()[filterGroups])
	frame()
	if s.archivedRow {
		t.Error("the Archived row shows under Groups")
	}
	u.pickChip(u.chipItems()[filterAll])
	frame()

	s.list.Position = layout.Position{First: 3, Offset: 5, BeforeEnd: true}
	u.startChatSelect()
	frame()
	if p := s.list.Position; s.archivedRow || p.First != 2 || p.Offset != 5 {
		t.Errorf("select mode: row %v at %d+%d, want no row at 2+5", s.archivedRow, p.First, p.Offset)
	}
	u.endChatSelect()
	frame()
	if p := s.list.Position; !s.archivedRow || p.First != 3 || p.Offset != 5 {
		t.Errorf("after it: row %v at %d+%d, want the row and 3+5", s.archivedRow, p.First, p.Offset)
	}

	s.filter = filterUnread
	s.archived.Click()
	frame()
	frame()
	if got := visibleIDs(u); !s.showArchived || !slices.Equal(got, []string{"old-project"}) {
		t.Errorf("the Archived page (%v) shows %v", s.showArchived, got)
	}
	s.search.SetText("rina")
	frame()
	if got := visibleIDs(u); !slices.Equal(got, []string{"old-project"}) {
		t.Errorf("a search left behind filters the Archived page to %v", got)
	}
	s.back.Click()
	frame()
	if s.showArchived {
		t.Error("Back didn't leave the Archived page")
	}

}

// TestSelectChatsBar checks that in select mode a row opens its chat, its
// checkbox picks it, and the bar's menu acts on those picked.
func TestSelectChatsBar(t *testing.T) {
	u, b, frame, click := chatListUI(t)
	u.sidebar.search.SetText("bud")
	u.startChatSelect()
	frame()
	if u.listQuery() != "" {
		t.Error("the search, which the bar hides, still filters the list")
	}
	if u.pickItems() != nil {
		t.Error("the menu offers actions on no chats")
	}
	u.rowClick(u.chatByID("sari")).Click()
	frame()
	if u.selected == nil || u.selected.ID != "sari" || len(u.pickedChats()) != 0 {
		t.Error("clicking a row picked it or didn't open its chat")
	}
	click("pickbox:budi")
	click("pickbox:mom")
	click("pickbox:mom")
	if got := u.pickedChats(); len(got) != 1 || got[0].ID != "budi" {
		t.Fatalf("picked %v", got)
	}
	click("pick:menu")
	var labels []string
	for _, it := range u.ctx.items {
		labels = append(labels, it.label)
	}
	want := []string{"Mark as unread", "Mute notifications", "Archive chat", "Pin chat", "", "Clear selected chats", "Delete selected chats"}
	if !slices.Equal(labels, want) {
		t.Fatalf("menu %q, want %q", labels, want)
	}
	click("menu:pick-clear")
	if u.dialog.kind != dialogConfirm {
		t.Fatal("Clear selected chats didn't ask")
	}
	u.dialog.buttons[0].run()
	frame()
	if u.sidebar.pick.on || u.chatByID("budi") == nil {
		t.Error("after clearing, select mode is on or the chat is gone")
	}
	if len(b.Messages("budi", 10)) != 0 {
		t.Error("Budi's chat wasn't cleared")
	}
}

// TestMarkAllReadItem checks that the ⋮ menu's Mark all as read is there
// only while something is unread.
func TestMarkAllReadItem(t *testing.T) {
	u, _, frame, _ := chatListUI(t)
	if len(u.unreadChats()) == 0 {
		t.Fatal("the demo has no unread chats")
	}
	u.markAllRead()
	frame()
	if got := u.unreadChats(); len(got) != 0 {
		t.Errorf("still unread: %v", got)
	}
}

// TestContactLetters checks that contacts are listed under a heading per
// letter, those whose name doesn't start with one under # at the end.
func TestContactLetters(t *testing.T) {
	var nc newChatState
	var cs []*model.Contact
	for _, n := range []string{"9 Lives", "adi", "Ana", "budi", "✨ Star", "Citra", "émile"} {
		cs = append(cs, &model.Contact{ID: n, Name: n})
	}
	var got []string
	for _, r := range nc.contactRows(byLetter(cs)) {
		if r.contact == nil {
			got = append(got, "["+r.heading+"]")
		} else {
			got = append(got, r.contact.Name)
		}
	}
	want := []string{"[A]", "adi", "Ana", "[B]", "budi", "[C]", "Citra", "[É]", "émile", "[#]", "9 Lives", "✨ Star"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
