package ui

import (
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

func TestUpdateRows(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.applyEvents()
	u.ShowPage("help")
	if r := settingRowByKey(t, u, "version"); r.sub != "Development build" {
		t.Errorf("a build without a version shows %q", r.sub)
	}
	for _, sec := range u.settingsRows() {
		for _, r := range sec.rows {
			if r.key == "update" {
				t.Error("a development build offers to update")
			}
		}
	}

	h := &host{o: Options{Version: "v0.9.0", Relaunch: []string{}}, reqs: make(chan request, 16)}
	h.upd.h, h.upd.exe = h, "WazzapClients.exe"
	u.host = h
	rows := func() map[string]settingRow {
		u.settings.stale = true
		m := map[string]settingRow{}
		for _, sec := range u.settingsRows() {
			for _, r := range sec.rows {
				m[r.key] = r
			}
		}
		return m
	}
	m := rows()
	if m["version"].sub != "v0.9.0" || m["update"].title != "Check for updates" || m["update"].run == nil {
		t.Fatalf("release build rows: version %q, update %q", m["version"].sub, m["update"].title)
	}
	h.upd.step = updLatest
	if r := rows()["update"]; !strings.Contains(r.title, "up to date") || r.run == nil {
		t.Errorf("up to date row: %q", r.title)
	}
	if _, ok := rows()["whatsnew"]; ok {
		t.Error("What's new shows without a newer release")
	}
}
