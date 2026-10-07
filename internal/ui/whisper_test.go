package ui

import (
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

type whisperUIBackend struct {
	model.Backend
	chat, text        string
	targets, mentions []string
	done              chan error
}

func (b *whisperUIBackend) SendWhisper(chat string, targets []string, text string, mentions []string) <-chan error {
	b.chat, b.targets, b.text, b.mentions = chat, targets, text, mentions
	return b.done
}

func TestSlashWhisper(t *testing.T) {
	st := newSlashTest(t, "work")
	st.b.SetPref(grayCmdPref("whisper"), "on")
	st.u.loadExtras()
	b := &whisperUIBackend{Backend: st.u.backend, done: make(chan error, 1)}
	st.u.backend = b
	before := len(st.b.Messages("work", 1000))
	st.typeText("/whisper ")
	for range 2 {
		sp := st.u.slashQuery()
		if sp == nil || len(sp.vals) == 0 || sp.opt.Name != "members" {
			t.Fatal("missing member picker")
		}
		st.u.pickSlash(sp, 0)
		st.frame()
	}
	// The body also takes @mentions, picked like any message's.
	ed := &st.u.conv.composer
	ed.Insert("hello 👋 @")
	st.frame()
	ms := st.u.mentionQuery()
	if ms == nil {
		t.Fatal("no mention picker in the body")
	}
	pick := 0
	for ms.members[pick].Me || strings.HasPrefix(ms.members[pick].ID, "@") { // skip you, @all, @admin
		pick++
	}
	name, jid := st.u.mentionName(ms.members[pick]), ms.members[pick].ID
	st.u.pickMention(pick)
	st.frame()
	st.press(key.NameReturn)
	user, _, _ := strings.Cut(jid, "@")
	if b.chat != "work" || b.text != "hello 👋 @"+user || len(b.targets) != 2 || b.targets[0] == b.targets[1] {
		t.Fatalf("wrong send: %+v (mentioned %q)", b, name)
	}
	if len(b.mentions) != 1 || b.mentions[0] != jid {
		t.Fatalf("body mention not forwarded: %+v", b.mentions)
	}
	if !st.lastNote("work").Busy {
		t.Fatal("submission didn't wait for backend")
	}
	b.done <- nil
	close(b.done)
	select {
	case done := <-st.u.slash.done:
		done()
	case <-time.After(2 * time.Second):
		t.Fatal("submission didn't complete")
	}
	st.frame()
	// On success the note is dismissed; the chat's whisper message stands in.
	if ns := st.u.slash.notes["work"]; len(ns) != 0 {
		t.Fatalf("success left a note: %+v", ns)
	}
	if len(st.b.Messages("work", 1000)) != before {
		t.Fatal("whisper became a group broadcast")
	}
	if st.u.ghostMode() {
		t.Fatal("whisper enabled ghost mode")
	}
}

func TestWhisperDoesNotFallThrough(t *testing.T) {
	for _, mode := range []string{"disabled", "slash disabled", "private", "leading space", "attachment", "edit"} {
		t.Run(mode, func(t *testing.T) {
			chat := "work"
			if mode == "private" {
				chat = "rina"
			}
			st := newSlashTest(t, chat)
			st.b.SetPref(grayCmdPref("whisper"), "on")
			if mode == "disabled" {
				st.b.SetPref(grayCmdPref("whisper"), "off")
			}
			if mode == "slash disabled" {
				st.b.SetPref(prefSlash, "off")
			}
			st.u.loadExtras()
			text := "/whisper @Budi secret"
			if mode == "leading space" {
				text = " " + text
			}
			st.typeText(text)
			if mode == "attachment" {
				st.u.attach.files = []*attachFile{{}}
			}
			if mode == "edit" {
				st.u.conv.edit.msg = &model.Message{}
			}
			before := len(st.b.Messages(chat, 1000))
			st.u.sendComposer()
			if len(st.b.Messages(chat, 1000)) != before || st.text() != text {
				t.Fatal("whisper draft broadcast or lost")
			}
			if mode == "disabled" && st.u.slashQuery() != nil {
				t.Fatal("disabled command offered")
			}
		})
	}
}

func TestWhisperConsentCancel(t *testing.T) {
	for _, stage := range []string{"1/2", "2/2"} {
		for _, dismiss := range []string{"cancel", "escape", "outside"} {
			t.Run(stage+"/"+dismiss, func(t *testing.T) {
				st := newSlashTest(t, "work")
				st.u.ShowPage("gray")
				st.frame()
				click := func(id string) {
					st.u.btn(id).Click()
					st.frame()
					st.frame()
				}
				pref := grayCmdPref("whisper")
				click("settings:" + pref)
				if stage == "2/2" {
					click("dialog:agreement")
					click("dialog:1")
				}
				if !strings.Contains(st.u.dialog.title, stage) || st.u.dialog.agreed {
					t.Fatal("wrong stage or acknowledgment carried over")
				}
				click("dialog:agreement")
				switch dismiss {
				case "cancel":
					click("dialog:2")
				case "escape":
					st.press(key.NameEscape)
				case "outside":
					st.u.dialog.scrim.Click()
					st.frame()
				}
				click("dialog:1") // a late click must not activate the canceled step
				if st.u.dialog.isOpen() || extraOn(st.b, pref) || st.u.grayCmds["whisper"] {
					t.Fatal("canceling enabled whisper or left the dialog open")
				}
				for range 30 {
					st.frame()
				}
				click("settings:" + pref)
				if !strings.Contains(st.u.dialog.title, "1/2") || st.u.dialog.agreed {
					t.Fatal("activation did not restart with fresh first confirmation")
				}
			})
		}
	}
}

// A note dropped once it has shown shrinks away instead of vanishing, and
// one dropped before it was ever drawn just goes.
func TestDismissedNoteShrinksAway(t *testing.T) {
	st := newSlashTest(t, "work")
	host := slashHost{u: st.u, chat: "work"}
	inRows := func(n *command.Note) bool {
		for _, r := range st.u.rows(st.u.selected) {
			if r.kind == rowNote && r.note.note == n {
				return true
			}
		}
		return false
	}
	quick := &command.Note{Text: "Submitting whisper…", Busy: true}
	host.Note(quick)
	host.Dismiss(quick)
	st.frame()
	if inRows(quick) || len(st.u.slash.leaving["work"]) != 0 {
		t.Fatal("a note never drawn lingered")
	}

	n := &command.Note{Text: "Submitting whisper…", Busy: true}
	host.Note(n)
	for range 30 {
		st.frame()
	}
	host.Dismiss(n)
	if len(st.u.slash.notes["work"]) != 0 {
		t.Fatal("dismissed note still listed")
	}
	st.frame()
	if !inRows(n) {
		t.Fatal("the note vanished instead of shrinking away")
	}
	for range 30 {
		st.frame()
	}
	if inRows(n) || len(st.u.slash.leaving["work"]) != 0 {
		t.Fatal("the note never went")
	}
}
