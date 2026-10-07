package ui

import (
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

type whisperUIBackend struct {
	model.Backend
	chat, text string
	targets    []string
	done       chan error
}

func (b *whisperUIBackend) SendWhisper(chat string, targets []string, text string) <-chan error {
	b.chat, b.targets, b.text = chat, targets, text
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
	st.typeText(st.text() + "hello 👋")
	st.press(key.NameReturn)
	if b.chat != "work" || b.text != "hello 👋" || len(b.targets) != 2 || b.targets[0] == b.targets[1] {
		t.Fatalf("wrong send: %+v", b)
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
	if n := st.lastNote("work"); n.Busy || n.Failed || !strings.Contains(n.Text, "Delivery is unconfirmed") {
		t.Fatalf("note %+v", n)
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
