package ui

import (
	"slices"
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func menuKeys(items []menuItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.key)
	}
	return out
}

// TestEditMessage checks editing in the composer: the message's text
// replaces the draft, Enter saves it, and the draft comes back after.
func TestEditMessage(t *testing.T) {
	st := newSlashTest(t, "work")
	u := st.u
	m := u.backend.Send("work", model.Draft{Text: "Standup at 10"})
	u.upsertMessage(m)
	st.frame()

	if keys := menuKeys(u.messageMenuItems(u.selected, m)); !slices.Contains(keys, "edit") {
		t.Fatalf("your new message's menu has no Edit: %v", keys)
	}
	old := u.msgs[0]
	if keys := menuKeys(u.messageMenuItems(u.selected, old)); slices.Contains(keys, "edit") {
		t.Fatalf("someone else's message offers Edit: %v", keys)
	}

	st.typeText("half a thought")
	u.startEdit(m)
	st.frame()
	if st.text() != "Standup at 10" {
		t.Fatalf("editing put %q in the composer", st.text())
	}
	st.typeText("Standup at 10:30")
	st.press(key.NameReturn)
	if u.conv.edit.msg != nil {
		t.Fatal("still editing after Enter")
	}
	if st.text() != "half a thought" {
		t.Fatalf("the draft came back as %q", st.text())
	}
	i := slices.IndexFunc(u.msgs, func(x *model.Message) bool { return x.ID == m.ID })
	if i < 0 || u.msgs[i].Text != "Standup at 10:30" || u.msgs[i].Edited.IsZero() {
		t.Fatalf("after the edit the message is %+v", u.msgs[i])
	}

	// Esc stops editing without saving.
	u.startEdit(u.msgs[i])
	st.typeText("never mind")
	st.press(key.NameEscape)
	if u.conv.edit.msg != nil || st.text() != "half a thought" {
		t.Fatalf("after Esc: editing %v, composer %q", u.conv.edit.msg != nil, st.text())
	}
	if u.msgs[i].Text != "Standup at 10:30" {
		t.Fatalf("Esc saved the edit: %q", u.msgs[i].Text)
	}

	// Edit history is an extra feature.
	if keys := menuKeys(u.messageMenuItems(u.selected, u.msgs[i])); slices.Contains(keys, "edits") {
		t.Fatal("Edit history shows while the feature is off")
	}
	u.editHistory = true
	if keys := menuKeys(u.messageMenuItems(u.selected, u.msgs[i])); !slices.Contains(keys, "edits") {
		t.Fatal("no Edit history for an edited message")
	}
	u.openEditHistory(u.msgs[i])
	st.frame()
	if vs := u.dialog.versions; len(vs) != 1 || vs[0].Text != "Standup at 10" {
		t.Fatalf("versions %+v", vs)
	}
}
