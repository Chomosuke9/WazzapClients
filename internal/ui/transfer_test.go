package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestTransferText(t *testing.T) {
	for _, c := range []struct {
		done, total int64
		left        time.Duration
		want        string
	}{
		{0, 0, 0, ""},
		{0, 100, 0, "0%"},
		{13, 100, 16 * time.Second, "13% (16s left)"},
		{200, 100, 0, "100%"},
		{5, 100, 95 * time.Second, "5% (2m left)"},
		{5, 100, 3600 * time.Second, "5% (1h 0m left)"},
	} {
		if got := transferText(model.Transfer{Done: c.done, Total: c.total, Left: c.left}); got != c.want {
			t.Errorf("%d of %d: %q, want %q", c.done, c.total, got, c.want)
		}
	}
}

// pillTest draws the picture of m alone, at 0,0, with its pill.
func pillTest(st *slashTest, m *model.Message, size image.Point) part {
	var ops op.Ops
	gtx := C{Ops: &ops, Now: st.now, Source: st.r.Source()}
	gtx.Metric.PxPerDp, gtx.Metric.PxPerSp = 1, 1
	gtx.Constraints.Max = size
	p := st.u.layoutPicture(gtx, image.Rectangle{Max: size}, m, nil, image.Point{})
	p.at(gtx, 0, 0)
	st.r.Frame(&ops)
	st.now = st.now.Add(10 * time.Millisecond)
	return p
}

func pillClick(st *slashTest, m *model.Message, size image.Point) {
	st.t.Helper()
	at := f32.Pt(20, float32(size.Y-20))
	st.r.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at})
	pillTest(st, m, size)
}

// A picture's pill downloads it, then shows how far it has got and cancels
// it.
func TestPicturePill(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	size := image.Pt(330, 248)
	m := &model.Message{ID: "v1", ChatID: "rina", Kind: model.KindImage, Media: model.MediaVideo, Duration: 10, FileSize: 4_200_000}
	offer := pillTest(st, m, size)
	if offer.size.X <= offer.size.Y {
		t.Fatal("a video that isn't here should offer to download it")
	}
	st.b.DemoTransfer(model.Transfer{ChatID: "rina", MsgID: "v1"})
	u.applyEvents()
	if p := pillTest(st, m, size); p.size.X != p.size.Y {
		t.Fatalf("before it moves there's just the ring, not %v", p.size)
	}
	st.b.DemoTransfer(model.Transfer{ChatID: "rina", MsgID: "v1", Done: 13, Total: 100, Left: 16 * time.Second})
	u.applyEvents()
	long := pillTest(st, m, size).size.X
	if long < 120 {
		t.Fatalf("the percentage and the time left are %d wide", long)
	}
	if p := pillTest(st, m, image.Pt(100, 100)); p.size.X >= long || p.size.X <= p.size.Y {
		t.Fatalf("a small tile keeps the percentage alone: %v", p.size)
	}
	if p := pillTest(st, m, image.Pt(70, 70)); p.size.X != p.size.Y {
		t.Fatalf("a tiny one keeps the ring alone: %v", p.size)
	}
	pillTest(st, m, size)
	pillClick(st, m, size)
	if len(st.b.Transfers()) != 0 {
		t.Fatal("a click on the pill should cancel the download")
	}
	u.applyEvents()
	if _, ok := u.transferOf(m); ok {
		t.Fatal("it still shows")
	}
	if got := pillTest(st, m, size); got.size != offer.size {
		t.Fatal("it should offer to download it again")
	}
	// Yours, going out or failed, has nothing to download.
	mine := *m
	mine.FromMe = true
	for _, r := range []model.Receipt{model.Pending, model.Failed} {
		mine.Receipt = r
		if u.offersDownload(&mine) {
			t.Errorf("offers your own file with receipt %d", r)
		}
	}
}

// A photo that downloads by itself does so unseen at first, and one whose
// download was canceled doesn't start over.
func TestPhotoTransfer(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	size := image.Pt(330, 248)
	m := &model.Message{ID: "p1", ChatID: "rina", Kind: model.KindImage, Media: model.MediaImage, FileSize: 200_000}
	if !u.photoWanted(m) || pillTest(st, m, size).size.X != 0 {
		t.Fatal("photos download by themselves")
	}
	st.b.DemoTransfer(model.Transfer{ChatID: "rina", MsgID: "p1", Done: 1, Total: 4})
	u.applyEvents()
	if pillTest(st, m, size).size.X != 0 {
		t.Fatal("it shows at once")
	}
	since := u.transfers.m["rina/p1"]
	since.since = since.since.Add(-time.Second)
	u.transfers.m["rina/p1"] = since
	if pillTest(st, m, size).size.X == 0 {
		t.Fatal("one that takes longer should show")
	}
	u.cancelTransfer(m)
	u.applyEvents()
	if u.photoWanted(m) || pillTest(st, m, size).size.X == 0 {
		t.Fatal("a canceled photo waits for a click")
	}
}

// Canceling an upload takes the message back.
func TestCancelUpload(t *testing.T) {
	st := newSlashTest(t, "mom")
	u := st.u
	ids := st.b.DemoTransfers("mom")
	u.applyEvents()
	st.frame()
	has := func(id string) *model.Message {
		for _, m := range u.msgs {
			if m.ID == id {
				return m
			}
		}
		return nil
	}
	m := has(ids[0])
	if m == nil {
		t.Fatal("the demo's upload isn't in the chat")
	}
	u.cancelTransfer(m)
	u.applyEvents()
	st.frame()
	if has(ids[0]) != nil {
		t.Fatal("it's still there")
	}
	// A new window shows what goes on.
	u2 := New(st.b)
	u2.loadTransfers()
	if len(u2.transfers.m) != len(st.b.Transfers()) || len(u2.transfers.m) == 0 {
		t.Fatalf("a new window knows %d of %d", len(u2.transfers.m), len(st.b.Transfers()))
	}
}
