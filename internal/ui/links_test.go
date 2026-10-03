package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestLinkSpans checks which part of a text is a link: not the punctuation
// that ends a sentence, unless it belongs to the link.
func TestLinkSpans(t *testing.T) {
	u := New(mock.New())
	for _, tc := range []struct{ text, link string }{
		{"see https://example.com.", "https://example.com"},
		{"(www.example.com/a)", "www.example.com/a"},
		{"https://en.wikipedia.org/wiki/Go_(game), nice", "https://en.wikipedia.org/wiki/Go_(game)"},
		{"is it https://example.com/?q=1?", "https://example.com/?q=1"},
	} {
		spans, deco := u.richSpans(tc.text, 15, u.pal.Text, false, 0)
		got := ""
		for i, s := range spans {
			if deco[i]&decoLink != 0 {
				got += s.Content
			}
		}
		if got != tc.link {
			t.Errorf("%q: link %q, want %q", tc.text, got, tc.link)
		}
	}
}

func TestInviteCode(t *testing.T) {
	for link, want := range map[string]string{
		"https://chat.whatsapp.com/FrLhsO1aBMEBhOFBtpLwxl":        "FrLhsO1aBMEBhOFBtpLwxl",
		"https://chat.whatsapp.com/invite/FrLhsO1aBMEBhOFBtpLwxl": "FrLhsO1aBMEBhOFBtpLwxl",
		"http://CHAT.whatsapp.com/abc123/":                        "abc123",
		"https://chat.whatsapp.com/":                              "",
		"https://chat.whatsapp.com.evil.example/abc":              "",
		"https://example.com/abc":                                 "",
		"www.chat.whatsapp.com/abc":                               "",
	} {
		if got := inviteCode(link); got != want {
			t.Errorf("inviteCode(%q) = %q, want %q", link, got, want)
		}
	}
}

// TestLinkHoverClick checks that hovering a link takes its underline away
// and that clicking an invite link opens the invite dialog.
func TestLinkHoverClick(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(600, 400)}}
		u.layoutRich(gtx, "https://chat.whatsapp.com/DemoInviteFutsal is the link", 15, u.pal.Text, u.pal.TextSecondary,
			richOpts{links: "t"})
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	frame()
	key := "lk:t:0:0"
	if u.btn(key).Hovered() {
		t.Fatal("the link is hovered before the pointer moved")
	}
	p := f32.Pt(30, 10)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	frame()
	if !u.btn(key).Hovered() {
		t.Fatal("the link isn't hovered under the pointer")
	}
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
	frame()
	frame()
	if u.dialog.kind != dialogInvite || u.dialog.invite.code != "DemoInviteFutsal" {
		t.Fatalf("clicking the link opened dialog %d (code %q), want the invite", u.dialog.kind, u.dialog.invite.code)
	}
}

// TestMentionClick checks that only mentions of people are clickable, and
// that clicking one shows their contact info.
func TestMentionClick(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.applyEvents()
	var m *model.Message
	for _, x := range b.Messages("work", 100) {
		if strings.Contains(x.Text, "@Bima") {
			m = x
		}
	}
	if m == nil {
		t.Fatal("no demo message mentions Bima")
	}
	spans, deco := u.richSpans(m.Text, 15, u.pal.Text, false, pillMe)
	got := ""
	for i, s := range spans {
		if deco[i]&decoMention != 0 {
			got += s.Content
		}
	}
	if got != "@Bima" {
		t.Fatalf("clickable mentions %q, want only @Bima", got)
	}
	u.openChatByID("work")
	u.openMention(m, "@Bima")
	if !u.info.open || u.info.chatID != "bima" || u.selected.ID != "work" {
		t.Fatalf("clicking @Bima showed info %q (open %v) in chat %q, want bima's in work", u.info.chatID, u.info.open, u.selected.ID)
	}
}
