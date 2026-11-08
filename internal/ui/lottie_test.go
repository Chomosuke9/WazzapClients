package ui

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// squareJSON is a 512x512 animation of a red square growing over 60
// frames at 60 a second.
const squareJSON = `{"v": "5.7.0", "fr": 60, "ip": 0, "op": 60, "w": 512, "h": 512,
 "layers": [{"ty": 4, "ind": 1, "ip": 0, "op": 60, "st": 0,
  "ks": {"p": {"a": 0, "k": [256, 256]}},
  "shapes": [
   {"ty": "rc", "p": {"a": 0, "k": [0, 0]}, "r": {"a": 0, "k": 0},
    "s": {"a": 1, "k": [{"t": 0, "s": [200, 200]}, {"t": 59, "s": [400, 400]}]}},
   {"ty": "fl", "c": {"a": 0, "k": [1, 0, 0, 1]}, "o": {"a": 0, "k": 100}}]}]}`

// redSquare is a WhatsApp Lottie sticker (.was) of squareJSON.
func redSquare(t *testing.T) []byte { return wasFile(t, false) }

// wasFile makes a .was sticker, with squareJSON as its effect too when
// premium.
func wasFile(t *testing.T, premium bool) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	files := []string{"animation/animation.json"}
	if premium {
		files = append(files, "animation/animation_secondary.json")
	}
	for _, name := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(squareJSON))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A Lottie sticker shows its first frame, drawn at the size asked for, and
// counts as animated.
func TestDecodeLottie(t *testing.T) {
	img, animated := decodeScaled(redSquare(t), 128)
	if img == nil || !animated {
		t.Fatalf("decodeScaled = %v, animated %v", img, animated)
	}
	rgba := img.(*image.RGBA)
	if b := rgba.Bounds(); b.Dx() != 128 || b.Dy() != 128 {
		t.Fatalf("size %v, want 128x128", b)
	}
	if got := rgba.RGBAAt(64, 64); got.R != 255 || got.A != 255 {
		t.Errorf("middle %v, want red", got)
	}
	if got := rgba.RGBAAt(5, 5); got.A != 0 {
		t.Errorf("corner %v, want clear (the square starts 200 of 512 wide)", got)
	}
}

// The player draws a Lottie sticker's frames as time goes by.
func TestPlayLottie(t *testing.T) {
	data := redSquare(t)
	p := &stickerPlayer{ready: -1, shown: -1, stop: make(chan struct{})}
	var held atomic.Int64
	frames := make(chan struct{}, 100)
	done := make(chan struct{})
	go func() {
		p.run(func() []byte { return data }, 64, &held, func() {
			select {
			case frames <- struct{}{}:
			default:
			}
		})
		close(done)
	}()
	for n := 0; n < 3; n++ {
		select {
		case <-frames:
		case <-time.After(5 * time.Second):
			t.Fatal("no frame")
		}
	}
	p.mu.Lock()
	b := p.bufs[p.ready]
	p.mu.Unlock()
	// Drawn at the size shown: half what the image cache asks for.
	if b.Bounds().Dx() != 32 {
		t.Errorf("frame %v, want 32 wide", b.Bounds())
	}
	if held.Load() <= 0 {
		t.Error("the player holds no memory against the budget")
	}
	close(p.stop)
	<-done
	if held.Load() != 0 {
		t.Errorf("%d bytes still held after it stopped", held.Load())
	}
}

// A premium sticker's effect plays once and ends; a sticker without one
// says so.
func TestEffectPlaysOnce(t *testing.T) {
	for _, premium := range []bool{true, false} {
		data := wasFile(t, premium)
		p := &stickerPlayer{ready: -1, shown: -1, stop: make(chan struct{}), fx: true}
		var held atomic.Int64
		var frames atomic.Int32
		done := make(chan struct{})
		go func() {
			p.runEffect(func() []byte { return data }, 64, &held, func() { frames.Add(1) })
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the effect never ended")
		}
		if !p.done.Load() || p.none.Load() == premium {
			t.Errorf("premium %v: done %v, none %v", premium, p.done.Load(), p.none.Load())
		}
		// 60 frames at 60 a second, drawn at 30 a second: 30, and the end.
		if premium && frames.Load() != 31 {
			t.Errorf("%d frames shown, want 30 and the end", frames.Load())
		}
		if held.Load() != 0 {
			t.Errorf("%d bytes still held", held.Load())
		}
	}
}

// An effect plays once: the messages whose effect played are kept in the
// account's prefs, the newest fxSeenMax.
func TestFXSeen(t *testing.T) {
	b := mock.New()
	u := New(b)
	for i := range fxSeenMax + 5 {
		u.markFXSeen(&model.Message{ChatID: "rina", ID: fmt.Sprint(i)})
	}
	u.markFXSeen(&model.Message{ChatID: "rina", ID: "7"}) // again: kept once
	ids := strings.Fields(b.Pref(prefFXSeen))
	if len(ids) != fxSeenMax || ids[0] != "rina/5" || ids[len(ids)-1] != fmt.Sprintf("rina/%d", fxSeenMax+4) {
		t.Fatalf("kept %d: %v ... %v", len(ids), ids[0], ids[len(ids)-1])
	}
	v := New(b)
	v.loadFXSeen()
	if !v.fx.isSeen["rina/7"] || v.fx.isSeen["rina/4"] {
		t.Error("a new window doesn't read which effects played")
	}
}

// A clicked sticker plays past the cap of stickers playing at once, and
// starts over with its effect.
func TestClickedStickerPlays(t *testing.T) {
	u := New(mock.New())
	ps := &u.players
	none := func() []byte { return nil }
	for i := range 2 * perf.gifs {
		u.stickerFrame(fmt.Sprint("m:other/", i), 64, none)
	}
	m := &model.Message{ChatID: "rina", ID: "s1", Kind: model.KindSticker, Media: model.MediaSticker, Lottie: true}
	key := stickerKey(m)
	u.stickerFrame(key, 64, none)
	if ps.m[key] != nil {
		t.Fatal("a sticker past the cap plays")
	}
	u.playSticker(m, 64)
	u.stickerFrame(key, 64, none)
	first := ps.m[key]
	if first == nil {
		t.Fatal("the clicked sticker doesn't play")
	}
	if !u.fx.want[key] {
		t.Error("the click doesn't ask for its effect")
	}
	u.playSticker(m, 64)
	if ps.m[key] == first {
		t.Error("a second click doesn't start it over")
	}
	ps.stopAll()
}

// A clicked sticker always gets room: players are stopped for it, those
// playing on their own first, then the oldest clicked, and stay stopped
// (benched) while on screen.
func TestClickMakesRoom(t *testing.T) {
	u := New(mock.New())
	ps := &u.players
	mb := int64(1 << 20)
	add := func(key string, clicked bool, cost int64) {
		p := ps.add(key, &stickerPlayer{clicked: clicked})
		p.cost.Store(cost)
		ps.bytes.Add(cost)
	}
	add("auto1", false, 6*mb)
	add("click1", true, 6*mb)
	add("auto2", false, 6*mb)
	add("click2", true, 5*mb)
	// 23 MB held: 4 more need auto1 and auto2 gone.
	ps.makeRoom(4*mb, "new")
	release := func() {
		ps.bytes.Store(0)
		for _, p := range ps.m {
			ps.bytes.Add(p.cost.Load())
		}
	}
	release()
	if ps.m["auto1"] != nil || ps.m["click1"] == nil || ps.m["click2"] == nil {
		t.Errorf("kept %v", keys(ps.m))
	}
	if _, ok := ps.benched["auto1"]; !ok {
		t.Error("a stopped sticker isn't benched")
	}
	// Then the oldest clicked.
	ps.makeRoom(14*mb, "new")
	release()
	if ps.m["click1"] != nil || ps.m["click2"] == nil {
		t.Errorf("kept %v", keys(ps.m))
	}
	// A benched sticker doesn't start again on its own.
	if _, ok := u.stickerFrame("auto1", 64, func() []byte { return nil }); ok || u.players.m["auto1"] != nil {
		t.Error("a benched sticker started again")
	}
	u.players.stopAll()
}

func keys[V any](m map[string]V) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// An effect that found no room in the players' budget on the sticker's
// first play isn't given up on: a click plays it.
func TestEffectWithoutRoomWaitsForClick(t *testing.T) {
	b := mock.New()
	u := New(b)
	m := b.SendNewSticker("rina", wasFile(t, true), nil)
	m.Lottie = true
	key := stickerKey(m)
	u.players.bytes.Store(playerBudget) // full
	gtx := layout.Context{Ops: new(op.Ops), Now: testNow()}
	deadline := time.Now().Add(roomWait + 5*time.Second)
	for {
		u.stickerEffect(gtx, m, 64)
		if u.fx.tried[key] && u.players.m["fx:"+key] == nil {
			break // started, then given up
		}
		if time.Now().After(deadline) {
			t.Fatal("the effect neither played nor gave up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if u.fx.none[key] {
		t.Fatal("a sticker whose effect found no room is taken for one without")
	}
	u.stickerEffect(gtx, m, 64)
	if u.players.m["fx:"+key] != nil {
		t.Fatal("the effect started again on its own")
	}
	u.players.bytes.Store(0)
	u.playSticker(m, 64)
	if !u.fx.want[key] {
		t.Fatal("a click doesn't ask for the effect")
	}
	u.stickerEffect(gtx, m, 64)
	if u.players.m["fx:"+key] == nil {
		t.Error("the clicked sticker's effect didn't start")
	}
	u.players.stopAll()
}
