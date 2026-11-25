package wa

import (
	"sync"
	"time"

	waBinary "github.com/polymorfa/hypermeow/binary"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// When this device connects after a while, WhatsApp sends what it missed:
// an <ib><offline_preview count message receipt …/></ib> says how much,
// each missed stanza has an offline attribute, and an <ib><offline/></ib>
// ends it. The messages among them are counted as they're handled, which
// comes after they arrive: hypermeow queues them and handles them in order,
// so once one is handled, those before it are too. (Some, such as a
// duplicate, bring no event of their own.)

// catchUpEvery is how often the count goes to the UI, at most.
const catchUpEvery = 100 * time.Millisecond

type catchUp struct {
	mu    sync.Mutex
	total int            // missed messages; 0 while there's nothing to catch up on
	seen  int            // arrived
	done  int            // handled
	nth   map[string]int // the place of those that arrived and weren't handled
	sent  time.Time      // when the count last went out
}

// noteOffline reads the start of a catch-up, and the messages it brings,
// from the raw stanzas.
func (b *Backend) noteOffline(node *waBinary.Node) {
	switch node.Tag {
	case "success":
		b.endCatchUp(false) // connected again
	case "ib":
		if p, ok := node.GetOptionalChildByTag("offline_preview"); ok {
			ag := p.AttrGetter()
			b.startCatchUp(ag.OptionalInt("message"))
		}
	case "message":
		if _, ok := node.Attrs["offline"]; !ok {
			return
		}
		id, _ := node.Attrs["id"].(string)
		c := &b.catchUp
		c.mu.Lock()
		if c.total > 0 && id != "" {
			c.seen++
			c.nth[id] = c.seen
		}
		c.mu.Unlock()
	}
}

func (b *Backend) startCatchUp(total int) {
	b.endCatchUp(false)
	if total <= 0 {
		return
	}
	c := &b.catchUp
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total, c.seen, c.done, c.nth, c.sent = total, 0, 0, map[string]int{}, b.now()
	b.emit(model.CatchUpEvent{Total: total})
}

// caughtUpTo notes that the message id was handled.
func (b *Backend) caughtUpTo(id string) {
	c := &b.catchUp
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nth[id]
	if !ok {
		return
	}
	delete(c.nth, id)
	// More can come than the preview said.
	if n = min(n, c.total); n <= c.done {
		return
	}
	c.done = n
	if now := b.now(); now.Sub(c.sent) >= catchUpEvery && n < c.total {
		c.sent = now
		b.emit(model.CatchUpEvent{Done: n, Total: c.total})
	}
}

// endCatchUp ends the catch-up: all of it came, or the connection was lost
// (the next one starts over, with what's left).
func (b *Backend) endCatchUp(all bool) {
	c := &b.catchUp
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.total == 0 {
		return
	}
	e := model.CatchUpEvent{}
	if all {
		e = model.CatchUpEvent{Done: c.total, Total: c.total}
	}
	c.total, c.nth = 0, nil
	b.emit(e)
}
