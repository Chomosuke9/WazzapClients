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
	note      *Note
	dismissed bool
}

func (h *whisperTestHost) Note(n *Note)          { h.note = n }
func (h *whisperTestHost) Dismiss(n *Note)       { h.dismissed = h.note == n }
func (h *whisperTestHost) Do(work func() func()) { work()() }

// Draft rewrites a member's "@Name" to "@<user>" and collects the JID, as the
// UI's draftWith does, so the test exercises mentions in the body.
func (h *whisperTestHost) Draft(text string) model.Draft {
	d := model.Draft{Text: text}
	for _, m := range testMembers {
		if at := "@" + m.Name; strings.Contains(d.Text, at) {
			d.Text = strings.ReplaceAll(d.Text, at, "@"+m.ID[:strings.IndexByte(m.ID, '@')])
			d.Mentions = append(d.Mentions, m.ID)
		}
	}
	return d
}

type whisperTestBackend struct {
	model.Backend
	chat, text        string
	targets, mentions []string
	reply             *model.Message
	err               error
}

func (b *whisperTestBackend) SendWhisper(chat string, targets []string, text string, mentions []string, reply *model.Message) <-chan error {
	b.chat, b.targets, b.text, b.mentions, b.reply = chat, targets, text, mentions, reply
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
		reply := &model.Message{ID: "Q1", ChatID: "group", Text: "quoted"}
		Execute(&Context{Cmd: in.Cmd, Input: text, Values: in.Values, Chat: &model.Chat{ID: "group", IsGroup: true},
			Info: &model.ChatInfo{Members: testMembers}, Reply: reply, Backend: b, Host: h})
		if b.reply != reply {
			t.Fatalf("reply not forwarded: %+v", b.reply)
		}
		if b.chat != "group" || !slices.Equal(b.targets, []string{"budi@lid", "siti@lid"}) || b.text != "Halo 👋\nbaris kedua @sigit" {
			t.Fatalf("wrong recipients/body: %+v", b)
		}
		if !slices.Equal(b.mentions, []string{"sigit@lid"}) {
			t.Fatalf("body mentions not forwarded: %+v", b.mentions)
		}
		if sendErr == nil {
			// The chat's whisper message is the confirmation; no note lingers.
			if !h.dismissed || h.note.Failed {
				t.Fatalf("success should dismiss the note: %+v (dismissed %v)", h.note, h.dismissed)
			}
		} else if h.dismissed || h.note == nil || !h.note.Failed || h.note.Text != sendErr.Error() {
			t.Fatalf("failure note: %+v (dismissed %v)", h.note, h.dismissed)
		}
	}
}

// TestWhisperMentionFirst checks that a new line ends the members, so the
// text may start with an @mention of its own.
func TestWhisperMentionFirst(t *testing.T) {
	text := "/whisper @Budi Santoso @Siti\n@Sigit hey"
	in, ok := Parse(text, len([]rune(text)), []Mention{{"Budi Santoso", "budi@lid"}}, testMembers)
	if !ok || in.Problem() != "" || in.Current != 1 {
		t.Fatalf("parse: %+v; %s", in, in.Problem())
	}
	b, h := &whisperTestBackend{}, &whisperTestHost{}
	Execute(&Context{Cmd: in.Cmd, Input: text, Values: in.Values, Chat: &model.Chat{ID: "group", IsGroup: true},
		Info: &model.ChatInfo{Members: testMembers}, Backend: b, Host: h})
	if !slices.Equal(b.targets, []string{"budi@lid", "siti@lid"}) || b.text != "@sigit hey" ||
		!slices.Equal(b.mentions, []string{"sigit@lid"}) {
		t.Fatalf("wrong recipients/body: %+v", b)
	}

	// Right after the new line, the text is being typed (and missing).
	text = "/whisper @Siti\n"
	in, _ = Parse(text, len([]rune(text)), nil, testMembers)
	if in.Current != 1 || in.Problem() != "Fill in text." {
		t.Errorf("%q: current %d, problem %q", text, in.Current, in.Problem())
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
