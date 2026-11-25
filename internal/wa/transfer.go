package wa

import (
	"context"
	"io"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// How far a message's file has got, uploading or downloading. hypermeow
// tells nothing of it, but its media requests carry the context they were
// asked for with: one that names a transfer has its body counted as it
// goes through the HTTP client (progressTransport). Canceling a transfer
// cancels that context.

const (
	// transferTick is the least time between two TransferEvents of one
	// transfer: each redraws the window.
	transferTick = 100 * time.Millisecond
	// The speed is that of the last transferWindow, once transferWarm of
	// it is known. An upload's first megabytes only fill the system's
	// buffers, and a connection's speed changes.
	transferWindow = 5 * time.Second
	transferWarm   = time.Second
)

type transfers struct {
	mu sync.Mutex
	m  map[string]*transfer // by chat and message ID
}

// transfer is a message's file on its way. Nil is one nobody follows.
type transfer struct {
	b      *Backend
	ctx    context.Context
	cancel context.CancelFunc
	upload bool

	// Guarded by b.transfers.mu:
	t       model.Transfer
	speed   transferSpeed
	told    time.Time // when the last TransferEvent went
	shown   int       // the percentage it told
	stopped bool      // by the user (CancelTransfer)
}

type transferKey struct{}

// beginTransfer announces the transfer of a message's file, which lasts
// until end. It returns nil when that message has one already.
func (b *Backend) beginTransfer(chatID, msgID string, upload bool) *transfer {
	b.transfers.mu.Lock()
	defer b.transfers.mu.Unlock()
	key := chatID + "/" + msgID
	if b.transfers.m[key] != nil {
		return nil
	}
	if b.transfers.m == nil {
		b.transfers.m = make(map[string]*transfer)
	}
	t := &transfer{b: b, upload: upload, t: model.Transfer{ChatID: chatID, MsgID: msgID, Upload: upload}}
	ctx, cancel := context.WithCancel(b.ctx)
	t.ctx, t.cancel = context.WithValue(ctx, transferKey{}, t), cancel
	b.transfers.m[key] = t
	b.emit(model.TransferEvent{Transfer: t.t})
	return t
}

// context is what the transfer's requests are made with: parent for one
// nobody follows.
func (t *transfer) context(parent context.Context) context.Context {
	if t == nil {
		return parent
	}
	return t.ctx
}

// end ends the transfer, and reports whether the user canceled it.
func (t *transfer) end() bool {
	if t == nil {
		return false
	}
	ts := &t.b.transfers
	ts.mu.Lock()
	defer ts.mu.Unlock()
	key := t.t.ChatID + "/" + t.t.MsgID
	if ts.m[key] == t {
		delete(ts.m, key)
		t.cancel()
		t.b.emit(model.TransferEvent{Transfer: t.t, End: true})
	}
	return t.stopped
}

// canceled reports whether the user canceled the transfer.
func (t *transfer) canceled() bool {
	if t == nil {
		return false
	}
	t.b.transfers.mu.Lock()
	defer t.b.transfers.mu.Unlock()
	return t.stopped
}

// start begins counting a request's body, total bytes long (less than 1
// when that isn't known). A request tried again starts over.
func (t *transfer) start(total int64) {
	ts := &t.b.transfers
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t.t.Done, t.t.Total, t.t.Left = 0, max(0, total), 0
	t.speed = transferSpeed{}
	t.speed.add(t.b.now(), 0)
	t.tell(true)
}

// moved counts n more bytes of the body.
func (t *transfer) moved(n int) {
	ts := &t.b.transfers
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t.t.Done += int64(n)
	if t.t.Total > 0 {
		t.t.Done = min(t.t.Done, t.t.Total)
		now := t.b.now()
		t.speed.add(now, t.t.Done)
		t.t.Left = t.speed.left(now, t.t.Done, t.t.Total)
	}
	t.tell(false)
}

// tell emits a TransferEvent if there's something new to show, or anyway.
func (t *transfer) tell(anyway bool) {
	ts := &t.b.transfers
	if ts.m[t.t.ChatID+"/"+t.t.MsgID] != t {
		return // over: a canceled request's last bytes
	}
	now, pct := t.b.now(), t.t.Percent()
	if !anyway && (pct == t.shown || now.Sub(t.told) < transferTick && t.t.Done < t.t.Total) {
		return
	}
	t.told, t.shown = now, pct
	t.b.emit(model.TransferEvent{Transfer: t.t})
}

// transferSpeed works out how long the rest of a transfer takes from how
// far it had got lately.
type transferSpeed struct {
	samples []transferSample // oldest first, at least a quarter second apart
}

type transferSample struct {
	at   time.Time
	done int64
}

func (s *transferSpeed) add(now time.Time, done int64) {
	if n := len(s.samples); n > 0 && now.Sub(s.samples[n-1].at) < 250*time.Millisecond {
		return
	}
	s.samples = append(s.samples, transferSample{now, done})
	for len(s.samples) > 2 && now.Sub(s.samples[1].at) >= transferWindow {
		s.samples = slices.Delete(s.samples, 0, 1)
	}
}

// left is about how long the rest takes, in whole seconds (at least one);
// 0 when that isn't known yet, or nothing is left.
func (s *transferSpeed) left(now time.Time, done, total int64) time.Duration {
	if len(s.samples) == 0 || done >= total {
		return 0
	}
	first := s.samples[0]
	took, moved := now.Sub(first.at), done-first.done
	if took < transferWarm || moved <= 0 {
		return 0
	}
	left := time.Duration(float64(took) * float64(total-done) / float64(moved))
	return max(time.Second, left.Round(time.Second))
}

// Transfers implements model.Backend.
func (b *Backend) Transfers() []model.Transfer {
	b.transfers.mu.Lock()
	defer b.transfers.mu.Unlock()
	var out []model.Transfer
	for t := range maps.Values(b.transfers.m) {
		out = append(out, t.t)
	}
	return out
}

// CancelTransfer implements model.Backend.
func (b *Backend) CancelTransfer(chatID, msgID string) {
	key := chatID + "/" + msgID
	b.transfers.mu.Lock()
	t := b.transfers.m[key]
	if t != nil {
		t.stopped = true
		t.cancel()
	}
	b.transfers.mu.Unlock()
	if t != nil && !t.upload && b.downloads.drop(key) {
		t.end() // it was waiting for others: nothing else ends it
	}
}

// takeBack removes a message of yours whose upload was canceled: nobody
// else has it.
func (b *Backend) takeBack(chatID, id string) {
	if err := b.store.deleteMessage(b.ctx, chatID, id); err != nil {
		b.log.Errorf("remove the canceled message %s: %v", id, err)
	}
	b.emit(model.DeletedEvent{ChatID: chatID, IDs: []string{id}})
	b.emitChat(chatID)
}

// progressTransport counts the bodies of the requests made with a
// transfer's context: what an upload sends, or what a download gets.
type progressTransport struct{ base http.RoundTripper }

// mediaClient is what media goes through, for every account.
var mediaClient = sync.OnceValue(func() *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	// As hypermeow's own, which it doesn't export.
	base.MaxIdleConns, base.MaxIdleConnsPerHost = 128, 32
	return &http.Client{Transport: progressTransport{base}}
})

func (p progressTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t, _ := req.Context().Value(transferKey{}).(*transfer)
	if t == nil {
		return p.base.RoundTrip(req)
	}
	if t.upload {
		if req.Body == nil || req.Body == http.NoBody {
			return p.base.RoundTrip(req)
		}
		total := req.ContentLength
		counted := req.WithContext(req.Context()) // the request isn't ours to change
		counted.Body = &countedBody{req.Body, t}
		if get := req.GetBody; get != nil {
			counted.GetBody = func() (io.ReadCloser, error) {
				body, err := get()
				if err != nil {
					return nil, err
				}
				t.start(total)
				return &countedBody{body, t}, nil
			}
		}
		t.start(total)
		return p.base.RoundTrip(counted)
	}
	resp, err := p.base.RoundTrip(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		t.start(resp.ContentLength)
		resp.Body = &countedBody{resp.Body, t}
	}
	return resp, err
}

type countedBody struct {
	io.ReadCloser
	t *transfer
}

func (c *countedBody) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		c.t.moved(n)
	}
	return n, err
}
