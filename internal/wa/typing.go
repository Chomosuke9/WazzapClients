package wa

import (
	"context"
	"sync"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/polymorfa/hypermeow/types"
)

const typingIdle = 3 * time.Second
const typingRefresh = 4 * time.Second

// A single worker serializes presence updates. Activity is coalesced, so
// fast typing never creates a goroutine or network request per keystroke.
type outgoingTyping struct {
	mu    sync.Mutex
	chat  string
	until time.Time
	wake  chan struct{}
}

func (p *outgoingTyping) report(chat string, now time.Time) {
	p.mu.Lock()
	p.chat, p.until = chat, now.Add(typingIdle)
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *outgoingTyping) current(now time.Time) (string, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !now.Before(p.until) {
		return "", p.until
	}
	return p.chat, p.until
}

func (p *outgoingTyping) run(ctx context.Context, send func(string, bool)) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var active string
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
		case <-timer.C:
		}
		now := time.Now()
		chat, until := p.current(now)
		if active != "" && active != chat {
			send(active, false)
			active = ""
		}
		if chat != "" && (active != chat || now.Sub(last) >= typingRefresh) {
			send(chat, true)
			active, last = chat, now
		}
		timer.Stop()
		if chat != "" {
			// Refresh only on new input; the timer solely ends idle typing.
			timer.Reset(time.Until(until))
		}
	}
}

func (b *Backend) ReportTyping(chatID string) {
	b.typing.report(chatID, time.Now())
}

func (b *Backend) sendTyping(chatID string, composing bool) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() || composing && b.Pref(model.PrefGhost) == "on" {
		return
	}
	jid, err := types.ParseJID(chatID)
	if err != nil || jid.User == "" {
		return
	}
	switch jid.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
	default:
		return // newsletters and status broadcasts have no typing presence
	}
	state := types.ChatPresencePaused
	if composing {
		state = types.ChatPresenceComposing
	}
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	if err := cli.SendChatPresence(ctx, jid, state, types.ChatPresenceMediaText); err != nil {
		b.log.Debugf("chat presence: %v", err)
	}
}
