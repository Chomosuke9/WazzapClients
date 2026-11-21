package wa

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
)

// Linking with a phone number: instead of scanning the QR code, the phone
// takes an 8-character code (WhatsApp > Linked devices > Link a device >
// Link with phone number instead). WhatsApp wants the request on a client
// that is linking already, after its first QR code (pair).

// pairAttempt is a running pair: its client, and ready, closed once the
// first QR code came.
type pairAttempt struct {
	cli       *whatsmeow.Client
	ready     chan struct{}
	readyOnce sync.Once
}

// ErrLinked is PairPhone's error once this device is linked.
var ErrLinked = errors.New("this device is linked already")

// attempt keeps cli as the running pair's client.
func (b *Backend) attempt(cli *whatsmeow.Client) *pairAttempt {
	at := &pairAttempt{cli: cli, ready: make(chan struct{})}
	b.pairMu.Lock()
	b.pairing = at
	b.pairMu.Unlock()
	return at
}

// endAttempt forgets at once its pair loop ended, unless a newer one took
// over.
func (b *Backend) endAttempt(at *pairAttempt) {
	b.pairMu.Lock()
	if b.pairing == at {
		b.pairing = nil
	}
	b.pairMu.Unlock()
}

// pairWait is how long PairPhone waits for a pair to show its first code.
const pairWait = 30 * time.Second

// PairPhone asks WhatsApp for the code that links this device to the
// account of phone (international format, digits; "+62 812-..." is fine)
// and returns it, "ABCD-EFGH". The phone gets a notification to enter it.
// When the QR codes ran out it starts linking again first. It blocks on
// the network: call it from a goroutine of its own, not the UI's.
func (b *Backend) PairPhone(phone string) (string, error) {
	if b.client().Store.ID != nil {
		return "", ErrLinked
	}
	ctx, cancel := context.WithTimeout(b.ctx, pairWait)
	defer cancel()
	started := false
	for {
		b.pairMu.Lock()
		at := b.pairing
		b.pairMu.Unlock()
		if at == nil {
			// Not linking (the QR codes ran out): start, and wait for it.
			if b.client().Store.ID != nil {
				return "", ErrLinked
			}
			if !started {
				started = true
				go b.pair()
			}
			select {
			case <-ctx.Done():
				return "", errors.New("linking didn't start in time")
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return "", errors.New("WhatsApp didn't answer in time")
		case <-at.ready:
		}
		code, err := at.cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, pairDisplayName())
		if err != nil {
			return "", err
		}
		return code, nil
	}
}

// pairDisplayName is how the phone names this device while it links, in
// the form WhatsApp wants ("Browser (OS)").
func pairDisplayName() string {
	switch runtime.GOOS {
	case "windows":
		return "Chrome (Windows)"
	case "darwin":
		return "Chrome (Mac OS)"
	}
	return "Chrome (Linux)"
}
