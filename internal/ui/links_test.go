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

func TestParseAppLink(t *testing.T) {
	for link, want := range map[string]*appLink{
		"whatsapp://send?phone=6281234567890&text=Hi%20there": {phone: "6281234567890", text: "Hi there"},
		"whatsapp://send/?phone=%2B62+812-3456-7890":          {phone: "6281234567890"},
		"WhatsApp://chat?code=FrLhsO1aBMEBhOFBtpLwxl":         {code: "FrLhsO1aBMEBhOFBtpLwxl"},
		"https://wa.me/6281234567890?text=hello":              {phone: "6281234567890", text: "hello"},
		"https://api.whatsapp.com/send?phone=15551234567":     {phone: "15551234567"},
		"https://api.whatsapp.com/send/?phone=15551234567":    {phone: "15551234567"},
		"https://chat.whatsapp.com/FrLhsO1aBMEBhOFBtpLwxl":    {code: "FrLhsO1aBMEBhOFBtpLwxl"},
		"whatsapp://send?text=no+number":                      {text: "no number", share: true},
		"https://api.whatsapp.com/send/?text=https%3A%2F%2Fwww.reddit.com%2Fr%2Fx&type=custom_url&app_absent=0": {
			text: "https://www.reddit.com/r/x", share: true},
		"https://wa.me/?text=hello":                       {text: "hello", share: true},
		"whatsapp://send?phone=12&text=hello":             nil,
		"whatsapp://send":                                 nil,
		"whatsapp://chat?code=bad/../code":                nil,
		"https://wa.me/message/ABCDEF":                    {resolve: "ABCDEF", kind: model.LinkBusiness},
		"https://api.whatsapp.com/message/ABCDEF":         {resolve: "ABCDEF", kind: model.LinkBusiness},
		"https://wa.me/qr/QRCODE12":                       {resolve: "QRCODE12", kind: model.LinkContact},
		"https://whatsapp.com/channel/0029VaExample":      {resolve: "0029VaExample", kind: model.LinkChannel},
		"https://www.whatsapp.com/channel/0029VaExample/": {resolve: "0029VaExample", kind: model.LinkChannel},
		"whatsapp://channel/0029VaExample":                {resolve: "0029VaExample", kind: model.LinkChannel},
		"https://wa.me/channel/0029VaExample":             nil,
		"https://wa.me/qr/bad-code!":                      nil,
		"https://wa.me/c/6281234567890":                   nil,
		"https://wa.me.evil.example/6281234567890":        nil,
		"https://example.com/send?phone=6281234567890":    nil,
	} {
		got, ok := parseAppLink(link)
		switch {
		case want == nil && ok:
			t.Errorf("parseAppLink(%q) = %+v, want none", link, got)
		case want != nil && (!ok || got != *want):
			t.Errorf("parseAppLink(%q) = %+v, %v, want %+v", link, got, ok, *want)
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

// TestOpenAppLink checks that a wa.me link opens the number's chat with
// the link's text in the composer.
func TestOpenAppLink(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	u.openOutsideLink("whatsapp://send?phone=%2B1+555-123-4567&text=Hello%21")
	u.applyEvents()
	if u.selected == nil || u.selected.ID != "15551234567@s.whatsapp.net" {
		t.Fatalf("open chat %v, want 15551234567@s.whatsapp.net", u.selected)
	}
	if got := u.conv.composer.Text(); got != "Hello!" {
		t.Errorf("composer %q, want %q", got, "Hello!")
	}
}

// TestResolveLinks opens the links the server resolves with the demo
// backend: a business's message link and a contact's QR link open their
// chats, a followed channel opens, and another shows to follow first.
func TestResolveLinks(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	open := func(link string) {
		t.Helper()
		if !u.openAppLink(link) {
			t.Fatalf("openAppLink(%q) = false", link)
		}
		u.applyEvents()
	}
	open("https://wa.me/message/" + mock.LinkBusiness)
	if u.selected == nil || u.selected.ID != "nova@lid" {
		t.Fatalf("business link opened %v, want nova@lid", u.selected)
	}
	if got := u.conv.composer.Text(); !strings.HasPrefix(got, "Hi! I'd like") {
		t.Errorf("business link's composer %q", got)
	}
	open("https://wa.me/qr/" + mock.LinkContact)
	if u.selected == nil || u.selected.ID != "rina" {
		t.Fatalf("contact link opened %v, want rina", u.selected)
	}
	open("https://whatsapp.com/channel/" + mock.LinkChannelFollowed)
	if u.page != pageChannels || u.selected == nil || u.selected.ID != "techdaily@newsletter" {
		t.Fatalf("followed channel's link: page %v, chat %v", u.page, u.selected)
	}
	open("https://whatsapp.com/channel/" + mock.LinkChannelSuggested)
	in := u.dialog.invite
	if u.dialog.kind != dialogInvite || !in.channel || in.ch == nil || in.ch.ID != "bola@newsletter" {
		t.Fatalf("suggested channel's link shows %+v", in)
	}
	u.followFromLink(in.ch.ID)
	u.applyEvents()
	if u.selected == nil || u.selected.ID != "bola@newsletter" {
		t.Fatalf("followed channel opened %v, want bola@newsletter", u.selected)
	}
	open("https://wa.me/qr/Unknown1")
	if u.selected.ID != "bola@newsletter" {
		t.Errorf("an unknown link opened %v", u.selected)
	}
}

// TestShareLink checks that a share link (text, no number) asks for the
// chat, and that the chat picked opens with the text in its composer.
func TestShareLink(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	const link = "https://api.whatsapp.com/send/?text=https%3A%2F%2Fwww.reddit.com%2Fr%2Fx&type=custom_url&app_absent=0"
	if !u.openAppLink(link) {
		t.Fatal("the share link wasn't taken")
	}
	if u.dialog.kind != dialogForward || u.dialog.shareText != "https://www.reddit.com/r/x" {
		t.Fatalf("dialog %v with %q", u.dialog.kind, u.dialog.shareText)
	}
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: testNow(), Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
	}
	frame()
	u.btn("fwd:budi").Click()
	frame()
	if u.selected == nil || u.selected.ID != "budi" {
		t.Fatalf("picked budi, open %v", u.selected)
	}
	if got := u.conv.composer.Text(); got != "https://www.reddit.com/r/x" {
		t.Errorf("composer %q", got)
	}
	if u.dialog.isOpen() {
		t.Error("the picker is still open")
	}
}
