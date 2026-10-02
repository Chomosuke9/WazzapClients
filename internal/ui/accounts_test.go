package ui

import (
	"path/filepath"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// newAccountsHost is a host without a window whose accounts are demo
// backends.
func newAccountsHost(t *testing.T) *host {
	t.Helper()
	root := t.TempDir()
	open := func(dir string) (model.Backend, error) {
		if dir == root {
			return mock.New(), nil
		}
		n := filepath.Base(dir)
		return mock.NewAccount("Account "+n, "+1 555 "+n, n+"@s.whatsapp.net"), nil
	}
	l := accounts.Load(root)
	b, _ := open(root)
	h := &host{b: b, o: Options{Accounts: l, Open: open}, reqs: make(chan request, 16), syncPct: -1}
	h.notes = newNotifier(b, h)
	b.Start(h.poke)
	h.poll(false)
	return h
}

// settle drains the backend's events and serves what the host asked of
// itself, until nothing is left.
func (h *host) settle() {
	for range 10 {
		h.poll(false)
		select {
		case r := <-h.reqs:
			h.handle(r)
		default:
		}
	}
}

func TestAccountSwitch(t *testing.T) {
	h := newAccountsHost(t)
	l := h.o.Accounts
	if cur := l.Current(); cur.ID != "15550100@s.whatsapp.net" || cur.Name != "Me Myself" {
		t.Fatalf("first account not noted: %+v", cur)
	}
	h.b.SetPref(prefTheme, "light")

	h.addAccount()
	h.settle()
	if l.Active != "accounts/2" || l.Current().Name != "Account 2" {
		t.Fatalf("after adding: active %q, %+v", l.Active, l.Current())
	}
	if h.b.Pref(prefTheme) != "light" {
		t.Error("the theme didn't carry over to the new account")
	}
	rows := h.accountRows()
	if len(rows) != 2 || rows[0].active || !rows[1].active {
		t.Errorf("rows = %+v", rows)
	}

	h.switchAccount("")
	h.settle()
	if l.Active != "" || h.conn.MeID != "me@lid" {
		t.Fatalf("after switching back: active %q, conn %+v", l.Active, h.conn)
	}

	// Logging out with another account linked opens that one, and the
	// account logged out leaves the list.
	h.logout()
	h.settle()
	if l.Active != "accounts/2" || len(l.Accounts) != 1 {
		t.Fatalf("after logout: active %q, %+v", l.Active, l.Accounts)
	}
}

func TestAccountAddedTwice(t *testing.T) {
	h := newAccountsHost(t)
	l := h.o.Accounts
	// The demo backends of another directory are other accounts; make
	// the next one the same as the first.
	open := h.o.Open
	h.o.Open = func(dir string) (model.Backend, error) {
		if dir != l.Path("") {
			return mock.New(), nil
		}
		return open(dir)
	}
	h.addAccount()
	h.settle()
	if l.Active != "" || len(l.Accounts) != 1 {
		t.Fatalf("active %q, accounts %+v; want the first account only", l.Active, l.Accounts)
	}
}
