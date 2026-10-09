package accounts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccounts(t *testing.T) {
	root := t.TempDir()
	l := Load(root)
	if len(l.Accounts) != 1 || l.Active != "" || l.Current().Linked() {
		t.Fatalf("first run: %+v", l)
	}
	l.Current().ID = "1@s.whatsapp.net"

	// A leftover of a removed account is cleared for the new one.
	stale := filepath.Join(root, "accounts", "2", "wazzap.db")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := l.Add()
	if err != nil || dir != "accounts/2" {
		t.Fatalf("Add = %q, %v", dir, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale data kept: %v", err)
	}
	if len(l.Others()) != 0 {
		t.Errorf("Others = %+v, want none: the new account isn't linked", l.Others())
	}
	l.Active = dir
	if o := l.Others(); len(o) != 1 || o[0].Dir != "" {
		t.Errorf("Others = %+v, want the first account", o)
	}
	l.Find(dir).ID = "2@s.whatsapp.net"
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}

	l = Load(root)
	if l.Active != "accounts/2" || len(l.Accounts) != 2 || l.Current().ID != "2@s.whatsapp.net" {
		t.Fatalf("reloaded: %+v", l)
	}
	if got, want := l.Path(dir), filepath.Join(root, "accounts", "2"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}

	// Removing the root account keeps its directory (the others live in
	// it), and the next account added takes it again.
	if err := l.Remove(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root removed: %v", err)
	}
	if dir, _ := l.Add(); dir != "" {
		t.Errorf("Add after removing the root = %q, want the root", dir)
	}
	if err := l.Remove("accounts/2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "accounts", "2")); !os.IsNotExist(err) {
		t.Errorf("removed account's data kept: %v", err)
	}
	if l.Active != "" {
		t.Errorf("Active = %q after removing the open account, want the root", l.Active)
	}
}

func TestBackgroundMode(t *testing.T) {
	root := t.TempDir()
	l := Load(root)
	l.Current().ID = "1@s.whatsapp.net"
	dir, _ := l.Add()
	a := l.Find(dir)
	a.ID, a.Name, a.Background = "2@s.whatsapp.net", "Work", 15
	at := time.Date(2026, 10, 9, 10, 32, 0, 0, time.UTC)
	a.Checked = at
	l.Find("").Background = Always
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "accounts.json"))
	if !strings.Contains(string(data), `"background": 15`) || strings.Count(string(data), `"checked"`) != 1 {
		t.Errorf("saved:\n%s", data)
	}

	l = Load(root)
	if a := l.Find(dir); a.Background != 15 || !a.Checked.Equal(at) || !a.InBackground() || a.Label() != "Work" {
		t.Errorf("reloaded: %+v", a)
	}
	if a := l.Find(""); a.Background != Always || !a.InBackground() || a.Label() != "" {
		t.Errorf("reloaded root: %+v", a)
	}
	if Off.Every() != 0 || Always.Every() != 0 || Mode(15).Every() != 15*time.Minute {
		t.Error("Every")
	}
	for m, want := range map[Mode]string{Off: "Only while open", Always: "Always connected", 5: "Check every 5 minutes", 60: "Check every hour"} {
		if m.String() != want {
			t.Errorf("%d: %q, want %q", m, m.String(), want)
		}
	}
	// A new account isn't linked yet: it can't run in the background.
	dir, _ = l.Add()
	l.Find(dir).Background = Always
	if l.Find(dir).InBackground() {
		t.Error("an account that isn't linked runs in the background")
	}
}
