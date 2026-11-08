package ui

import (
	"image"
	"strings"
	"sync/atomic"

	"gioui.org/layout"
	"gioui.org/op/paint"

	"github.com/chomosuke9/wazzapclients/internal/lottie"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Premium Lottie stickers carry an effect, a second animation that
// WhatsApp plays once around the sticker, 387% of its size and centered on
// it (WAWebStickerOverlayAnimation). Here it plays the first time the
// sticker plays, and again each time it's clicked.

const (
	// fxScale is the effect's size, of the sticker's.
	fxScale = 3.87
	// fxMaxSide caps the size the effect is drawn at (it's scaled up from
	// there): at 512 px it takes about 4 MB of the players' budget.
	fxMaxSide = 512
	// prefFXSeen keeps the messages whose effect has played, so it plays
	// once: their IDs, newest last, at most fxSeenMax.
	prefFXSeen = "sticker_fx_seen"
	fxSeenMax  = 300
)

// stickerFX is the effects' state.
type stickerFX struct {
	loaded bool
	seen   []string        // prefFXSeen, oldest first
	isSeen map[string]bool // the same
	want   map[string]bool // effects to start, by sticker key
	none   map[string]bool // stickers found to have none, by key
	// tried holds the stickers whose effect was started on their first
	// play this session: one that didn't get to play (no room in the
	// players' budget, or scrolled away) waits for a click.
	tried map[string]bool
}

func stickerKey(m *model.Message) string { return "m:" + m.ChatID + "/" + m.ID }

// stickerClick lets a click on an animated sticker play it from its start
// (past the players' cap, or with autoplay off), with its effect.
func (u *UI) stickerClick(gtx C, m *model.Message, sz int) {
	btn := u.btn("stk:" + m.ID)
	if btn.Clicked(gtx) {
		u.playSticker(m, sz)
	}
	cgtx := gtx
	cgtx.Constraints = layout.Exact(image.Pt(sz, sz))
	clickable(cgtx, btn, func(gtx C) D { return D{Size: image.Pt(sz, sz)} })
}

// playSticker starts a sticker shown sz px wide over, and its effect if it
// has one, stopping other players if they leave it too little memory.
func (u *UI) playSticker(m *model.Message, sz int) {
	ps := &u.players
	key := stickerKey(m)
	fxKey := "fx:" + key
	// Whether it's a Lottie sticker: the message says so, or (when it
	// doesn't) its player found out.
	lottie := m.Lottie
	if p := ps.m[key]; p != nil {
		lottie = lottie || p.lottie.Load()
		ps.stop(key, p)
	}
	if ps.forced == nil {
		ps.forced = make(map[string]bool)
	}
	ps.forced[key] = true
	delete(ps.benched, key)
	// Its effect, if it turns out to be a Lottie sticker with one
	// (stickerEffect draws it then).
	if !u.fx.none[key] {
		if p := ps.m[fxKey]; p != nil {
			ps.stop(fxKey, p)
		}
		u.fx.ask(key)
	}
	// Room for it: a WebP of the usual canvas at twice the size shown, or
	// a Lottie sticker at the size shown and its effect.
	need := webpCost(512, 512, 2*sz, 2*sz)
	if lottie {
		need = lottieCost(sz, sz)
		if !u.fx.none[key] {
			fs := min(int(float32(sz)*fxScale), fxMaxSide)
			need += lottieCost(fs, fs)
		}
	}
	ps.makeRoom(need, key, fxKey)
}

func (fx *stickerFX) ask(key string) {
	if fx.want == nil {
		fx.want = make(map[string]bool)
	}
	fx.want[key] = true
}

// loadFXSeen reads which effects have played.
func (u *UI) loadFXSeen() {
	fx := &u.fx
	if fx.loaded {
		return
	}
	fx.loaded = true
	fx.isSeen = make(map[string]bool)
	for id := range strings.FieldsSeq(u.backend.Pref(prefFXSeen)) {
		fx.seen = append(fx.seen, id)
		fx.isSeen[id] = true
	}
}

// markFXSeen remembers that m's effect played.
func (u *UI) markFXSeen(m *model.Message) {
	u.loadFXSeen()
	fx := &u.fx
	id := m.ChatID + "/" + m.ID
	if fx.isSeen[id] {
		return
	}
	fx.isSeen[id] = true
	fx.seen = append(fx.seen, id)
	if n := len(fx.seen) - fxSeenMax; n > 0 {
		for _, old := range fx.seen[:n] {
			delete(fx.isSeen, old)
		}
		fx.seen = append(fx.seen[:0], fx.seen[n:]...)
	}
	u.backend.SetPref(prefFXSeen, strings.Join(fx.seen, " "))
}

// stickerEffect draws a playing Lottie sticker's effect over it, starting
// it the first time the sticker plays. It is drawn with the sticker, over
// the messages above it (those below draw over it), and clipped to the
// conversation like the rest.
func (u *UI) stickerEffect(gtx C, m *model.Message, sz int) {
	key := stickerKey(m)
	fx := &u.fx
	if fx.none[key] {
		return
	}
	u.loadFXSeen()
	if !fx.isSeen[m.ChatID+"/"+m.ID] && !fx.tried[key] {
		if fx.tried == nil {
			fx.tried = make(map[string]bool)
		}
		fx.tried[key] = true
		fx.ask(key)
	}
	fxKey := "fx:" + key
	ps := &u.players
	if !fx.want[key] && ps.m[fxKey] == nil {
		return
	}
	delete(fx.want, key)
	side := int(float32(sz) * fxScale)
	b, chat, id := u.backend, m.ChatID, m.ID
	f, ok, p := u.fxFrame(fxKey, min(side, fxMaxSide), func() []byte { return b.MediaData(chat, id) })
	if p == nil {
		return
	}
	if p.done.Load() {
		// A sticker found to have no effect isn't tried again this
		// session. One that didn't get room in the players' budget plays
		// on the next click.
		if p.none.Load() {
			if fx.none == nil {
				fx.none = make(map[string]bool)
			}
			fx.none[key] = true
		}
		ps.stop(fxKey, p)
		return
	}
	if !ok {
		return
	}
	u.markFXSeen(m)
	c := sz / 2
	r := image.Rect(c-side/2, c-side/2, c-side/2+side, c-side/2+side)
	paintCover(gtx, f, f.Size(), r)
}

// fxFrame returns the current frame of a sticker's effect, starting its
// player. ok is false while the first frame renders.
func (u *UI) fxFrame(key string, side int, load func() []byte) (paint.ImageOp, bool, *stickerPlayer) {
	ps := &u.players
	p := ps.m[key]
	if p == nil {
		// An effect waits for room as a clicked sticker does: it plays
		// when the sticker first plays, or on a click.
		p = ps.add(key, &stickerPlayer{fx: true, clicked: true})
		ps.effects++
		go p.runEffect(load, side, &ps.bytes, u.images.invalidate)
	}
	p.used = ps.frame
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready >= 0 && p.ready != p.shown {
		p.shown = p.ready
		p.op = paint.NewImageOp(p.bufs[p.shown])
	}
	return p.op, p.shown >= 0, p
}

// runEffect plays a premium sticker's effect once.
func (p *stickerPlayer) runEffect(load func() []byte, side int, held *atomic.Int64, invalidate func()) {
	defer func() {
		p.done.Store(true)
		if invalidate != nil {
			invalidate()
		}
	}()
	a, err := lottie.LoadSecondary(load())
	if err != nil {
		p.none.Store(true)
		return
	}
	w, h := a.Fit(side)
	p.playLottie(a, w, h, false, held, invalidate)
}
