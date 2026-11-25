package ui

import (
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

const longDesc = "A place for anime fans to meet.\nDiscuss shows, favorite characters, recommendations and whatever is airing this season.\n\nRules:\n1. Be kind\n2. No spoilers\n3. Have fun"

// TestReadMoreLines: the cut text and its link keep to the lines they were
// cut to, whatever the width, and a short text isn't cut.
func TestReadMoreLines(t *testing.T) {
	h := newInfoHarness(t, "family")
	u := h.u
	texts := []string{longDesc, strings.Repeat("word ", 200), strings.Repeat("x", 900), strings.Repeat("\n", 20) + "end", strings.Repeat("🎉 ok ", 90),
		"a\nb\nc\nd\ne\nf", strings.Repeat("日本語", 150)}
	for _, s := range texts {
		for w := 120; w < 520; w += 7 {
			gtx := u.testContext(h, w)
			m := moreText{text: s, size: 17, lineH: 25, lines: 4, col: u.pal.Text, link: u.pal.Green, more: u.btn("t")}
			if d := u.layoutMoreText(gtx, m); d.Size.Y > 4*25 || d.Size.X != w {
				t.Errorf("%.12q at %d px: %v", s, w, d.Size)
			}
		}
	}
	if s, long := u.cutLines(u.testContext(h, 400), lineCut{text: "short", w: 400, lines: 4}, 17, 80); long || s != "short" {
		t.Errorf("a short text was cut to %q", s)
	}
}

func (u *UI) testContext(h *infoHarness, w int) C {
	h.ops.Reset()
	gtx := C{Ops: &h.ops, Now: h.now}
	gtx.Metric.PxPerDp, gtx.Metric.PxPerSp = 1, 1
	gtx.Constraints.Max = image.Pt(w, 1<<20)
	return gtx
}

// TestReadMoreDescription: a group's long description shows its first
// lines until "Read more" is clicked, and again in another group.
func TestReadMoreDescription(t *testing.T) {
	h := newInfoHarness(t, "family")
	u := h.u
	info := h.b.Info("family")
	info.About = longDesc
	height := func() int { return u.infoAbout(u.testContext(h, 400), u.selected, info).Size.Y }
	cut := height()
	u.btn("info:moredesc").Click()
	h.frames(3)
	if !u.info.allDesc || height() <= cut {
		t.Fatalf("Read more didn't show the rest: %v, %d then %d", u.info.allDesc, cut, height())
	}
	u.openInfo("work")
	if u.info.allDesc {
		t.Error("another group's description opened whole")
	}
}

// TestReadMoreStatus: a status's "Read more" shows its whole text and holds
// it; a click beside it, or Escape, shows the first lines again without
// moving on.
func TestReadMoreStatus(t *testing.T) {
	for _, media := range []model.Media{model.MediaNone, model.MediaImage} {
		h := newInfoHarness(t, "family")
		u := h.u
		v := &u.status.viewer
		th := &model.StatusThread{ID: "rina", Name: "Rina", Updates: []*model.StatusUpdate{
			{ID: "a", Text: strings.Repeat("A long one. ", 200), Media: media, Time: h.now},
			{ID: "b", Text: "next", Background: 0xff123456, Time: h.now},
		}}
		v.show(th)
		v.anim.snap(true)
		v.paused = true
		h.frames(3)
		click := func(at f32.Point) {
			h.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at})
			h.frames(3)
		}
		// Find the link with the pointer, and click it.
		link := f32.Point{}
		for y := float32(500); y < 840 && link.X == 0; y += 12 {
			for x := float32(500); x < 900; x += 30 {
				h.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, y)})
				h.frames(2)
				if v.moreBtn.Hovered() {
					link = f32.Pt(x, y)
					break
				}
			}
		}
		if link.X == 0 {
			t.Fatalf("media %v: no Read more link", media)
		}
		click(link)
		v.paused = false // its whole text holds it now
		v.held, v.shownAt = 0, h.now
		if v.more != "a" {
			t.Fatal("Read more didn't show the whole text")
		}
		h.now = h.now.Add(2 * statusDuration)
		h.frames(3)
		if v.index != 0 {
			t.Fatal("it moved on while its text was read")
		}
		// The wheel scrolls the text.
		at := f32.Pt(700, 600)
		h.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
			pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, 120)})
		h.frames(60)
		if p := v.moreList.Position; p.Offset == 0 {
			t.Error("the wheel didn't scroll the text")
		}
		for _, at := range []f32.Point{{X: 1200, Y: 500}, {X: 700, Y: 500}} { // beside it, and on it
			click(at)
			if v.more != "" || v.index != 0 || !v.isOpen() {
				t.Fatalf("media %v: a click at %v: whole %q, update %d", media, at, v.more, v.index)
			}
			v.wholeText(th.Updates[0])
			h.frames(3)
		}
		u.escape()
		h.frames(2)
		if v.more != "" || !v.isOpen() {
			t.Error("Escape didn't show the first lines again")
		}
		h.press(key.NameEscape)
		if v.isOpen() {
			t.Error("Escape didn't close the viewer")
		}
	}
}
