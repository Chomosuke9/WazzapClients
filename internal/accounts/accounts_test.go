package accounts

import (
	"os"
	"path/filepath"
	"testing"
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
