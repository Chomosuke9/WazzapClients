package wa

import (
	"encoding/json"
	"runtime/debug"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow/proto/waHistorySync"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// History sync comes in chunks, each with how far its kind has got: first
// the chats (INITIAL_BOOTSTRAP), then their older messages (RECENT, and FULL
// when linking asked for all of them). The phone doesn't reliably send a
// last one at 100%, and can stop for minutes. So the chats are done once
// chunks stop, and older messages at 100%, or once the phone has sent
// nothing for long. How far those got is kept in wz_meta, since the phone
// goes on sending after the app was closed.

const (
	// historySettle is how long chunks must have stopped coming for what
	// came to be gone through, and the phone to be waited for.
	historySettle = 20 * time.Second
	// historyGiveUp is how much longer the phone is waited for.
	historyGiveUp = 5 * time.Minute

	metaHistoryProgress = "history_progress"
)

type historyProgress struct {
	mu      sync.Mutex
	stage   model.SyncStage // SyncNone while nothing syncs
	kind    waHistorySync.HistorySync_HistorySyncType
	percent int
	timer   *time.Timer
	gen     int // counts the timers set, so that a late one does nothing
}

// savedProgress is metaHistoryProgress.
type savedProgress struct {
	Kind    waHistorySync.HistorySync_HistorySyncType
	Percent int
}

// noteHistory takes in the progress of a stored chunk.
func (b *Backend) noteHistory(kind waHistorySync.HistorySync_HistorySyncType, percent int) {
	var stage model.SyncStage
	switch kind {
	case waHistorySync.HistorySync_INITIAL_BOOTSTRAP:
		stage = model.SyncChats
	case waHistorySync.HistorySync_RECENT, waHistorySync.HistorySync_FULL:
		stage = model.SyncOlder
	default:
		return
	}
	percent = min(percent, 100)
	h := &b.history
	h.mu.Lock()
	defer h.mu.Unlock()
	if stage == model.SyncChats && h.stage == model.SyncOlder {
		return // a late chunk of the chats
	}
	if h.stage == stage && h.kind == kind {
		percent = max(percent, h.percent)
	}
	h.stage, h.kind, h.percent = stage, kind, percent
	b.emit(model.SyncEvent{Stage: stage, Percent: min(percent, 99)})
	if stage == model.SyncOlder {
		b.saveProgress(savedProgress{kind, percent})
	}
	b.historyTimer(historySettle, b.historyQuiet)
}

// historyTimer runs f (with h.mu held) after d, unless another timer is set
// by then. The caller holds h.mu.
func (b *Backend) historyTimer(d time.Duration, f func()) {
	h := &b.history
	if h.timer != nil {
		h.timer.Stop()
	}
	h.gen++
	gen := h.gen
	h.timer = time.AfterFunc(d, func() {
		b.goWork(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.gen == gen && b.ctx.Err() == nil {
				f()
			}
		})
	})
}

// historyQuiet runs once chunks stopped coming.
func (b *Backend) historyQuiet() {
	h := &b.history
	if h.stage == model.SyncOlder && h.percent < 100 {
		b.emit(model.SyncEvent{Stage: h.stage, Percent: h.percent, Waiting: true})
		b.historyTimer(historyGiveUp, b.historyGaveUp)
	} else {
		b.historyEnd(100)
	}
	// Names and LID mappings from history are stored in the background, so
	// re-resolve everything.
	b.names.clear()
	b.refreshChatNames()
	// History sync allocates a lot briefly; give it back to the OS.
	debug.FreeOSMemory()
}

// historyGaveUp runs once the phone has sent nothing for long. Offline
// nothing could come, so that doesn't count.
func (b *Backend) historyGaveUp() {
	if cli := b.client(); cli != nil && !cli.IsConnected() {
		b.historyTimer(historyGiveUp, b.historyGaveUp)
		return
	}
	b.historyEnd(b.history.percent)
}

// historyEnd ends the sync at percent. The caller holds h.mu.
func (b *Backend) historyEnd(percent int) {
	h := &b.history
	if h.stage == model.SyncNone {
		return
	}
	b.emit(model.SyncEvent{Stage: h.stage, Percent: percent, Done: true})
	if h.stage == model.SyncOlder {
		_ = b.store.setMetaValue(b.ctx, metaHistoryProgress, "")
	}
	h.stage, h.percent = model.SyncNone, 0
}

// resumeHistory goes on waiting for the older messages that were still
// coming when the app was closed.
func (b *Backend) resumeHistory() {
	h := &b.history
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stage != model.SyncNone || b.ctx.Err() != nil {
		return
	}
	var s savedProgress
	if v := b.store.meta(b.ctx, metaHistoryProgress); v == "" || json.Unmarshal([]byte(v), &s) != nil {
		return
	}
	h.stage, h.kind, h.percent = model.SyncOlder, s.Kind, s.Percent
	b.emit(model.SyncEvent{Stage: h.stage, Percent: min(h.percent, 99), Waiting: true})
	if h.percent >= 100 {
		b.historyTimer(historySettle, b.historyQuiet) // closed before it settled
		return
	}
	b.historyTimer(historyGiveUp, b.historyGaveUp)
}

// dropHistory forgets the sync: the account was logged out.
func (b *Backend) dropHistory() {
	h := &b.history
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.timer != nil {
		h.timer.Stop()
	}
	h.gen++
	b.historyEnd(h.percent)
}

func (b *Backend) saveProgress(s savedProgress) {
	v, err := json.Marshal(s)
	if err == nil {
		err = b.store.setMetaValue(b.ctx, metaHistoryProgress, string(v))
	}
	if err != nil {
		b.log.Warnf("history sync: keep progress: %v", err)
	}
}
