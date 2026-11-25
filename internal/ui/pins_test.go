package ui

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// floatTest is a demo chat drawn frame by frame.
type floatTest struct {
	*UI
	b   *mock.Backend
	now time.Time
	ops op.Ops
	r   input.Router
}

func newFloatTest(chat string) *floatTest {
	b := mock.New()
	var backend model.Backend = b
	if chat == "long" {
		// Rina's chat, with a long history.
		chat = "rina"
		lc := &longChat{Backend: b, id: chat}
		for i := range 90 {
			lc.msgs = append(lc.msgs, &model.Message{ID: fmt.Sprintf("long-%d", i), ChatID: chat, FromMe: i%3 == 0,
				Text: fmt.Sprintf("message %d", i), Time: testNow().Add(time.Duration(i-100) * time.Minute)})
		}
		backend = lc
	}
	ft := &floatTest{UI: New(backend), b: b, now: testNow()}
	ft.Start(func() {})
	ft.SelectID(chat)
	ft.frames(3)
	return ft
}

func (ft *floatTest) frames(n int) {
	for range n {
		ft.ops.Reset()
		ft.Layout(layout.Context{Ops: &ft.ops, Now: ft.now, Source: ft.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		ft.r.Frame(&ft.ops)
		ft.now = ft.now.Add(100 * time.Millisecond)
	}
}

// onScreen reports whether message id is in the list's visible rows.
func (ft *floatTest) onScreen(id string) bool {
	rows, pos := ft.rows(ft.selected), ft.conv.list.Position
	for i := pos.First; i < min(pos.First+pos.Count, len(rows)); i++ {
		if rows[i].has(id) {
			return true
		}
	}
	return false
}

// A chat keeps its latest pins, and the banner goes through them: a click
// jumps to the one shown and shows the next.
func TestPins(t *testing.T) {
	ft := newFloatTest("long")
	lc := ft.backend.(*auto.Backend).Backend.(*longChat)
	var ids []string
	for i := range model.PinsMax {
		m := lc.msgs[len(lc.msgs)-60+10*i]
		lc.pins = append([]*model.Message{m}, lc.pins...)
		ids = append(ids, m.ID)
	}
	ft.loadPins(lc.id)
	ft.frames(2)
	pins := &ft.conv.pins
	for i := range model.PinsMax {
		want := ids[len(ids)-1-i]
		if got := pins.shown(); got == nil || got.ID != want {
			t.Fatalf("banner %d shows %+v, want %s", i, got, want)
		}
		ft.btn("pinned:" + pins.chat).Click()
		ft.frames(10)
		if !ft.onScreen(want) {
			t.Fatalf("click %d didn't go to %s", i, want)
		}
	}
	if pins.shown().ID != ids[len(ids)-1] {
		t.Error("the banner didn't come around")
	}
	// Another pin shows at once; unpinning the one shown moves on.
	pins.at = 2
	lc.pins = append([]*model.Message{lc.msgs[3]}, lc.pins[:3]...)
	ft.loadPins(lc.id)
	if pins.shown() != lc.msgs[3] {
		t.Errorf("a new pin isn't shown: %+v", pins.shown())
	}
	lc.pins = lc.pins[1:]
	ft.loadPins(lc.id)
	if len(pins.list) != 3 || pins.shown() == nil {
		t.Errorf("after unpinning: %d pins, shown %v", len(pins.list), pins.shown())
	}
}

// The demo keeps the latest pins, as WhatsApp does.
func TestDemoPins(t *testing.T) {
	ft := newFloatTest("work")
	var ids []string
	for i := range model.PinsMax + 1 {
		ft.b.PinMessage(ft.msgs[i], 24*time.Hour)
		ids = append(ids, ft.msgs[i].ID)
	}
	ft.applyEvents()
	if pins := &ft.conv.pins; len(pins.list) != model.PinsMax || pins.has(ids[0]) || pins.shown().ID != ids[len(ids)-1] {
		t.Errorf("%d pins kept, the oldest %v, shown %v", len(pins.list), pins.has(ids[0]), pins.shown())
	}
}

// After a click on a quote, the down button goes back to the reply, and
// then to the newest message.
func TestJumpBack(t *testing.T) {
	ft := newFloatTest("long")
	old, reply := ft.msgs[5], ft.msgs[len(ft.msgs)-30]
	reply.Quote = &model.Quote{ID: old.ID, Text: "x"}
	ft.jumpTo(reply.ID)
	ft.frames(10)
	if !ft.onScreen(reply.ID) || len(ft.conv.float.back) != 0 {
		t.Fatal("not at the reply")
	}
	ft.btn("quote:" + reply.ID).Click()
	ft.frames(12)
	if !ft.onScreen(old.ID) || len(ft.conv.float.back) != 1 {
		t.Fatalf("the quote didn't jump: %v", ft.conv.float.back)
	}
	ft.conv.float.down.Click()
	ft.frames(12)
	if !ft.onScreen(reply.ID) || len(ft.conv.float.back) != 0 {
		t.Fatalf("the button didn't go back: %v", ft.conv.float.back)
	}
	ft.conv.float.down.Click()
	ft.frames(12)
	if last := ft.msgs[len(ft.msgs)-1]; !ft.onScreen(last.ID) || ft.conv.float.away {
		t.Error("the button didn't go to the newest message")
	}

	// Scrolling back to the reply forgets it.
	ft.jumpTo(reply.ID)
	ft.frames(12)
	ft.btn("quote:" + reply.ID).Click()
	ft.frames(12)
	if len(ft.conv.float.back) != 1 {
		t.Fatal("the quote didn't jump again")
	}
	ft.jumpTo(reply.ID)
	ft.frames(12)
	if len(ft.conv.float.back) != 0 {
		t.Error("the reply is on screen, but the button still goes back to it")
	}
}

// After clicks on several replies' quotes, the down button goes back to
// them from the oldest, whatever the order they were clicked in, with the
// single arrow until the newest message is next.
func TestJumpBackOrder(t *testing.T) {
	ft := newFloatTest("long")
	f := &ft.conv.float
	old, first, second := ft.msgs[5], ft.msgs[30], ft.msgs[60]
	for _, m := range []*model.Message{first, second, first} {
		ft.jumpFrom(m, old.ID)
	}
	ft.frames(12)
	if !ft.onScreen(old.ID) || len(f.back) != 2 || f.backAnim.v != 1 {
		t.Fatalf("after the clicks: back %v, single arrow %v", f.back, f.backAnim.v)
	}
	f.down.Click()
	ft.frames(12)
	if !ft.onScreen(first.ID) || len(f.back) != 1 || f.backAnim.v != 1 {
		t.Fatalf("not at the oldest reply: back %v, single arrow %v", f.back, f.backAnim.v)
	}
	f.down.Click()
	ft.frames(12)
	if !ft.onScreen(second.ID) || len(f.back) != 0 || f.backAnim.v != 0 || !f.away {
		t.Fatalf("not at the next reply: back %v, single arrow %v", f.back, f.backAnim.v)
	}
	f.down.Click()
	ft.frames(12)
	if last := ft.msgs[len(ft.msgs)-1]; !ft.onScreen(last.ID) || f.away {
		t.Error("not at the newest message")
	}

	// Scrolled past, a reply no longer counts, but those below it do.
	ft.jumpFrom(second, old.ID)
	ft.jumpFrom(first, old.ID)
	ft.frames(12)
	ft.jumpTo(ft.msgs[40].ID)
	ft.frames(12)
	if len(f.back) != 1 || f.back[0].id != second.ID {
		t.Errorf("past the first reply: back %v", f.back)
	}
}

// The @ button counts a group's unread messages for you and goes to them
// in turn, oldest first; one that comes on screen no longer counts.
func TestMentionButton(t *testing.T) {
	ft := newFloatTest("work")
	f := &ft.conv.float
	if len(f.mentions) != 0 {
		t.Fatalf("read, but %v", f.mentions)
	}
	older := ft.msgs[0]
	f.away = true
	f.mentions = []string{older.ID}
	ft.newBelow(model.MessageEvent{New: true, Msg: &model.Message{ID: "n1", ChatID: "work",
		Text: "\u2068" + string(model.MentionNotifies) + "@Me\u2069 look"}})
	ft.newBelow(model.MessageEvent{New: true, Msg: &model.Message{ID: "n2", ChatID: "work", Text: "not for you"}})
	if len(f.mentions) != 2 || f.mentions[1] != "n1" {
		t.Fatalf("mentions %v", f.mentions)
	}
	f.mentions = f.mentions[:1]
	if ft.onScreen(older.ID) {
		t.Skip("the demo's group fits on screen")
	}
	ft.frames(3)
	if f.atAnim.v == 0 || len(f.mentions) != 1 {
		t.Fatalf("button %v, mentions %v", f.atAnim.v, f.mentions)
	}
	f.at.Click()
	ft.frames(12)
	if !ft.onScreen(older.ID) || len(f.mentions) != 0 {
		t.Errorf("at the mention %v, left %v", ft.onScreen(older.ID), f.mentions)
	}
}

// A reply's quote of a status update opens it, and a ringed picture in the
// chat list its poster's.
func TestOpenStatusFromChat(t *testing.T) {
	ft := newFloatTest("rina")
	var th *model.StatusThread
	for _, s := range ft.statuses {
		if !s.Mine && !s.Group {
			th = s
		}
	}
	if th == nil {
		t.Fatal("no status in the demo")
	}
	m := ft.msgs[len(ft.msgs)-1]
	m.Quote = &model.Quote{ID: th.Updates[0].ID, Sender: th.Name, Text: "status", Status: true}
	ft.msgsVer++
	ft.frames(2)
	ft.btn("quote:" + m.ID).Click()
	ft.frames(2)
	v := &ft.status.viewer
	if !v.isOpen() || v.thread.Updates[v.index].ID != th.Updates[0].ID {
		t.Fatal("the quote didn't open the status")
	}
	v.close()
	ft.frames(10)

	for _, c := range ft.chats {
		if s := ft.listStatus(c); s != nil {
			ft.btn("rowring:" + c.ID).Click()
			ft.frames(2)
			if !v.isOpen() || v.thread.ID != s.ID {
				t.Errorf("the picture of %s didn't open its status", c.ID)
			}
			return
		}
	}
	t.Log("no ringed chat in the demo's list")
}

// A reply with nothing written yet waits for its chat without "Draft:".
func TestReplyDraft(t *testing.T) {
	ft := newFloatTest("rina")
	m := ft.msgs[len(ft.msgs)-1]
	ft.startReply(m)
	ft.SelectID("work")
	if ft.drafts["rina"] == nil || ft.drafts["rina"].listed() {
		t.Fatalf("draft %+v", ft.drafts["rina"])
	}
	ft.SelectID("rina")
	if ft.conv.reply == nil || ft.conv.reply.ID != m.ID {
		t.Error("the reply was lost")
	}
}

// A reply to an album jumps to its grid.
func TestAlbumQuote(t *testing.T) {
	ft := newFloatTest("family")
	var album string
	var m *model.Message
	for _, x := range ft.msgs {
		if x.Album != "" && album == "" {
			album = x.Album
		}
		if x.Kind == model.KindText {
			m = x
		}
	}
	if album == "" || m == nil {
		t.Fatal("no album in the demo")
	}
	m.Quote = &model.Quote{ID: album, Text: "3 photos", Media: model.MediaImage, Album: true}
	if qm := ft.quotedMessage(m.Quote); qm == nil || qm.Album != album {
		t.Fatalf("quoted %+v", qm)
	}
	if !ft.jumpTo(album) {
		t.Error("didn't jump to the album")
	}
	if ft.b.AlbumMessage("family", album) == "" {
		t.Error("the album isn't stored")
	}
}

// Message info of a view once message in a group says who opened, who saw
// and who got it.
func TestViewOnceInfo(t *testing.T) {
	ft := newFloatTest("work")
	m := &model.Message{ID: "vo", ChatID: "work", Kind: model.KindViewOnce, Media: model.MediaImage, FromMe: true,
		Time: testNow(), Receipt: model.Delivered}
	at := testNow()
	ft.msgInfo.chatID, ft.msgInfo.msg = "work", m
	ft.msgInfo.data = &model.MessageInfo{Members: 3, Receipts: []model.PersonReceipt{
		{ID: "a", Name: "A", Delivered: at, Read: at, Played: at},
		{ID: "b", Name: "B", Delivered: at, Read: at},
		{ID: "c", Name: "C", Delivered: at},
	}}
	// The preview, then three headings with a person each, and those left
	// under the first two.
	if n := len(ft.msgInfoRows()); n != 1+3*2+2 {
		t.Errorf("%d rows", n)
	}
	m.Kind = model.KindText
	m.Media = model.MediaNone
	if n := len(ft.msgInfoRows()); n != 1+1+2+1+1+1 {
		t.Errorf("%d rows for a text", n)
	}
}
