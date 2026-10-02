// Package accounts keeps the list of WhatsApp accounts linked on this
// computer. Each account has its own data directory: the first one is the
// data directory itself, so an install that had a single account keeps
// its data where it was, and the ones added later live in accounts/<n>.
// Only one account is open (connected) at a time.
package accounts

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
)

const (
	listFile    = "accounts.json"
	pictureFile = "account.jpg"
)

// Account is a linked account, or one being linked.
type Account struct {
	// Dir is the account's data directory, relative to the root and
	// slash-separated; "" is the root itself.
	Dir string `json:"dir"`
	// ID is the account's own JID, once linked.
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"` // international format, with a leading +
}

// Linked reports whether the account was linked.
func (a *Account) Linked() bool { return a.ID != "" }

// List is the accounts of a root data directory.
type List struct {
	root     string
	Active   string    `json:"active"` // the open account's Dir
	Accounts []Account `json:"accounts"`
}

// Load reads the accounts of root. Without a list (the first run, or an
// install from before accounts), root itself is the only account.
func Load(root string) *List {
	l := &List{root: root}
	if data, err := os.ReadFile(filepath.Join(root, listFile)); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if len(l.Accounts) == 0 {
		l.Accounts = []Account{{Dir: ""}}
	}
	if l.Find(l.Active) == nil {
		l.Active = l.Accounts[0].Dir
	}
	return l
}

// Save writes the list.
func (l *List) Save() error {
	data, err := json.MarshalIndent(l, "", "\t")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.root, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(l.root, listFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(l.root, listFile))
}

// Path returns the data directory of the account with dir.
func (l *List) Path(dir string) string {
	return filepath.Join(l.root, filepath.FromSlash(dir))
}

// PicturePath is where an account's profile picture is kept, for showing
// it while another account is open.
func (l *List) PicturePath(dir string) string {
	return filepath.Join(l.Path(dir), pictureFile)
}

// Find returns the account with dir, or nil.
func (l *List) Find(dir string) *Account {
	for i := range l.Accounts {
		if l.Accounts[i].Dir == dir {
			return &l.Accounts[i]
		}
	}
	return nil
}

// Current returns the open account.
func (l *List) Current() *Account { return l.Find(l.Active) }

// Others lists the linked accounts besides the open one.
func (l *List) Others() []Account {
	var out []Account
	for _, a := range l.Accounts {
		if a.Dir != l.Active && a.Linked() {
			out = append(out, a)
		}
	}
	return out
}

// Add appends a new account, not linked yet, with an unused data
// directory, and returns its Dir. Leftovers of a removed account in that
// directory are deleted first.
func (l *List) Add() (string, error) {
	dir := ""
	for n := 2; l.Find(dir) != nil; n++ {
		dir = path.Join("accounts", strconv.Itoa(n))
	}
	if dir != "" {
		if err := os.RemoveAll(l.Path(dir)); err != nil {
			return "", err
		}
	}
	l.Accounts = append(l.Accounts, Account{Dir: dir})
	return dir, nil
}

// Remove takes an account off the list and deletes its data directory,
// which must not be open. The root's data stays: it holds the other
// accounts too, and logging out already wiped its session and messages.
func (l *List) Remove(dir string) error {
	i := slices.IndexFunc(l.Accounts, func(a Account) bool { return a.Dir == dir })
	if i < 0 {
		return nil
	}
	l.Accounts = slices.Delete(l.Accounts, i, i+1)
	if len(l.Accounts) == 0 {
		l.Accounts = []Account{{Dir: ""}}
	}
	if l.Find(l.Active) == nil {
		l.Active = l.Accounts[0].Dir
	}
	if dir == "" {
		err := os.Remove(l.PicturePath(dir))
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		return err
	}
	return os.RemoveAll(l.Path(dir))
}
