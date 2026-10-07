package command

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

type whisperTestHost struct {
	Host
	note *Note
}

func (h *whisperTestHost) Note(n *Note)          { h.note = n }
func (h *whisperTestHost) Do(work func() func()) { work()() }

type whisperTestBackend struct {
	model.Backend
	chat, text string
	targets    []string
	err        error
}

func (b *whisperTestBackend) SendWhisper(chat string, targets []string, text string) <-chan error {
	b.chat, b.targets, b.text = chat, targets, text
	done := make(chan error, 1)
	done <- b.err
	close(done)
	return done
}

func TestWhisperCommand(t *testing.T) {
	text := "/whisper @Budi Santoso @Siti @Budi Santoso Halo 👋\nbaris kedua @Sigit"
	in, ok := Parse(text, len([]rune(text)), []Mention{{"Budi Santoso", "budi@lid"}}, testMembers)
	if !ok || in.Problem() != "" || in.Current != 1 || !in.Cmd.Gray || !in.Cmd.Group {
		t.Fatalf("parse: %+v; %s", in, in.Problem())
	}
	for _, sendErr := range []error{nil, errors.New("partial submission")} {
		b, h := &whisperTestBackend{err: sendErr}, &whisperTestHost{}
		Execute(&Context{Cmd: in.Cmd, Input: text, Values: in.Values, Chat: &model.Chat{ID: "group", IsGroup: true},
			Info: &model.ChatInfo{Members: testMembers}, Backend: b, Host: h})
		if b.chat != "group" || !slices.Equal(b.targets, []string{"budi@lid", "siti@lid"}) || b.text != "Halo 👋\nbaris kedua @Sigit" {
			t.Fatalf("wrong recipients/body: %+v", b)
		}
		if h.note == nil || h.note.Busy || h.note.Failed != (sendErr != nil) {
			t.Fatalf("note: %+v", h.note)
		}
		if sendErr == nil && !strings.Contains(h.note.Text, "Delivery is unconfirmed") {
			t.Fatal("submission claimed delivery")
		}
	}
}

func TestWhisperInvalid(t *testing.T) {
	for _, text := range []string{"/whisper ", "/whisper @Siti", "/whisper @Nobody secret", "/whisper @You secret"} {
		in, ok := Parse(text, len([]rune(text)), nil, testMembers)
		if !ok || in.Problem() == "" {
			t.Errorf("accepted %q", text)
		}
	}
	for _, tc := range []struct {
		text        string
		group, info bool
		mentions    []Mention
	}{
		{"/whisper @Siti secret", false, true, nil},
		{"/whisper @Siti secret", true, false, nil},
		{"/whisper @Gone secret", true, true, []Mention{{"Gone", "gone@lid"}}},
		{"/whisper Siti secret", true, true, nil},
	} {
		in, _ := Parse(tc.text, len([]rune(tc.text)), tc.mentions, testMembers)
		b, h := &whisperTestBackend{}, &whisperTestHost{}
		c := &Context{Cmd: in.Cmd, Input: tc.text, Values: in.Values, Chat: &model.Chat{ID: "group", IsGroup: tc.group}, Backend: b, Host: h}
		if tc.info {
			c.Info = &model.ChatInfo{Members: testMembers}
		}
		Execute(c)
		if b.chat != "" || h.note == nil || !h.note.Failed {
			t.Errorf("accepted %+v: %+v", tc, h.note)
		}
	}
}
