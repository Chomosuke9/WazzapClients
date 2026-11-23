package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// One emoji alone has no bubble, in a reply too, and takes no sticker
// reactions; two or three stay in a bubble, bigger than text, as on
// WhatsApp.
func TestJumbo(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	c := u.selected
	text := func(s string) *model.Message {
		return &model.Message{ID: s, ChatID: c.ID, Text: s, Time: testNow()}
	}
	for s, want := range map[string]struct {
		jumbo, big, stickers bool
	}{
		"😀":     {true, true, false},
		"😀😀":    {false, true, true},
		"😀 😀 😀": {false, true, true},
		"😀😀😀😀":  {false, false, true},
		"hi 😀":  {false, false, true},
	} {
		m := text(s)
		if got := u.jumbo(c, m) != nil; got != want.jumbo {
			t.Errorf("%s: jumbo %v", s, got)
		}
		if got := bigEmojiSize(m) > 0; got != want.big {
			t.Errorf("%s: big %v", s, got)
		}
		if got := u.takesStickers(m); got != want.stickers {
			t.Errorf("%s: takes sticker reactions %v", s, got)
		}
	}

	m := text("😡")
	fwd := text("😀")
	fwd.Forwarded = true
	if u.jumbo(c, fwd) != nil {
		t.Error("a forward lost its bubble")
	}
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(800, 600)}}
	alone := u.layoutJumboMessage(gtx, c, m, u.jumbo(c, m), true, 500)
	m.Quote = &model.Quote{ID: "x", Sender: "Dewi", Text: "a long enough text to make the quote's bubble wide"}
	if u.jumbo(c, m) == nil {
		t.Fatal("a reply with one emoji has a bubble")
	}
	reply := u.layoutJumboMessage(gtx, c, m, u.jumbo(c, m), true, 500)
	if reply.Size.X < 250 || reply.Size.X > 500 || reply.Size.Y < alone.Size.Y+40 {
		t.Errorf("reply %v, alone %v", reply.Size, alone.Size)
	}
}

// A sticker is as big in a reply as alone.
func TestStickerReplySize(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow().Add(time.Second), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(800, 600)}}
	m := &model.Message{ID: "s", ChatID: "rina", Kind: model.KindSticker, Media: model.MediaSticker, Time: testNow()}
	quote := u.layoutQuote(gtx, "rina", &model.Quote{Text: "hi"}, u.pal.QuoteIn, u.pal.TextSecondary, 216, nil)
	alone := u.layoutStickerMessage(gtx, u.selected, m, false, 500)
	m.Quote = &model.Quote{Text: "hi"}
	reply := u.layoutStickerMessage(gtx, u.selected, m, false, 500)
	if alone.Size.X != int(stickerDp) {
		t.Errorf("alone %v", alone.Size)
	}
	if h := reply.Size.Y - quote.Size.Y; h > alone.Size.Y+20 {
		t.Errorf("reply %v over a quote of %v: its sticker is bigger than alone, %v", reply.Size, quote.Size, alone.Size)
	}
}
