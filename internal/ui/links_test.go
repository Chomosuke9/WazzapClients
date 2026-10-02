package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
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
