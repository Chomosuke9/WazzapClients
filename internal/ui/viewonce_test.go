package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// viewOnceMsg returns the open chat's view once message.
func (st *slashTest) viewOnceMsg() *model.Message {
	st.t.Helper()
	for _, m := range st.u.msgs {
		if m.Kind == model.KindViewOnce {
			return m
		}
	}
	st.t.Fatal("no view once message")
	return nil
}

// TestViewOnceOnce checks that a view once photo opens once, with
// screenshots blocked, and as often as you like with Replay view once on.
func TestViewOnceOnce(t *testing.T) {
	st := newSlashTest(t, "rina")
	m := st.viewOnceMsg()
	if !st.u.canOpenViewOnce(m) {
		t.Fatal("an unopened view once photo doesn't open")
	}
	st.u.openViewOnce(m)
	st.frame()
	if !st.u.viewer.open || !st.u.viewer.viewOnce || !st.u.captureBlocked {
		t.Fatalf("viewer open %v view once %v capture blocked %v", st.u.viewer.open, st.u.viewer.viewOnce, st.u.captureBlocked)
	}
	st.u.applyEvents()
	if m = st.viewOnceMsg(); !m.Opened || st.u.canOpenViewOnce(m) {
		t.Errorf("after opening: opened %v, opens again %v", m.Opened, st.u.canOpenViewOnce(m))
	}
	st.u.hideViewer()
	st.frame()
	if st.u.captureBlocked {
		t.Error("screenshots still blocked with the viewer closed")
	}
	st.u.openViewOnce(m)
	if st.u.viewer.open {
		t.Error("an opened view once photo opened again")
	}

	st.b.SetPref(model.PrefViewOnceReplay, "on")
	st.u.loadExtras()
	st.u.openViewOnce(m)
	st.frame()
	if !st.u.viewer.open {
		t.Fatal("Replay view once didn't open it again")
	}
	if st.u.captureBlocked {
		t.Error("Replay view once still blocks screenshots")
	}
}

// TestViewOnceOnPhone checks that a view once message whose media only
// the phone got doesn't open.
func TestViewOnceOnPhone(t *testing.T) {
	st := newSlashTest(t, "family")
	st.b.SetPref(model.PrefViewOnceReplay, "on")
	st.u.loadExtras()
	m := st.viewOnceMsg()
	st.u.openViewOnce(m)
	if st.u.canOpenViewOnce(m) || st.u.viewer.open {
		t.Error("a view once message on the phone opened")
	}
}
