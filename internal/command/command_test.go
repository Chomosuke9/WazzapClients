package command

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

var testMembers = []model.Member{
	{ID: "me@lid", Name: "You", Admin: true, Me: true},
	{ID: "budi@lid", Name: "Budi Santoso"},
	{ID: "siti@lid", Name: "Siti", Admin: true},
	{ID: "sigit@lid", Name: "Sigit"},
}

func TestParseNotCommands(t *testing.T) {
	for _, s := range []string{"", "hello", " /kick", "/nope arg", "/kickx @a"} {
		if _, ok := Parse(s, len([]rune(s)), nil, testMembers); ok {
			t.Errorf("Parse(%q) is a command", s)
		}
	}
}

func TestParseNaming(t *testing.T) {
	in, ok := Parse("/ki", 3, nil, testMembers)
	if !ok || !in.Naming || in.Cmd != nil || in.Word != "ki" {
		t.Fatalf("Parse(/ki) = %+v, %v", in, ok)
	}
	in, ok = Parse("/kick", 5, nil, testMembers)
	if !ok || !in.Naming || in.Cmd == nil || in.Cmd.Name != "kick" {
		t.Fatalf("Parse(/kick) = %+v, %v", in, ok)
	}
	if _, ok := Parse("/zz", 3, nil, testMembers); ok {
		t.Fatal("Parse(/zz) matches a command")
	}
}

func TestParseMembers(t *testing.T) {
	mentions := []Mention{{Name: "Budi Santoso", ID: "budi@lid"}}
	s := "/kick @Budi Santoso @si"
	in, ok := Parse(s, len([]rune(s)), mentions, testMembers)
	if !ok || in.Cmd.Name != "kick" || in.Naming {
		t.Fatalf("not parsed: %+v", in)
	}
	vs := in.Values[0]
	if len(vs) != 2 || vs[0].ID != "budi@lid" || vs[0].Text != "@Budi Santoso" {
		t.Fatalf("values = %+v", vs)
	}
	// "@si" could be Siti or Sigit.
	if vs[1].Err == "" {
		t.Fatalf("@si resolved to %q", vs[1].ID)
	}
	if in.Current != 0 || in.Word != "@si" {
		t.Fatalf("current %d, word %q", in.Current, in.Word)
	}
	s = "/kick @sig"
	in, _ = Parse(s, len([]rune(s)), nil, testMembers)
	if v := in.Values[0][0]; v.ID != "sigit@lid" || v.Err != "" {
		t.Fatalf("@sig = %+v", v)
	}
	if p := in.Problem(); p != "" {
		t.Fatalf("problem %q", p)
	}
}

func TestParseFilter(t *testing.T) {
	// Siti is an admin already.
	s := "/promote @Siti"
	in, _ := Parse(s, len([]rune(s)), []Mention{{Name: "Siti", ID: "siti@lid"}}, testMembers)
	if in.Values[0][0].Err == "" {
		t.Fatal("promoted an admin")
	}
	s = "/demote @Siti"
	in, _ = Parse(s, len([]rune(s)), []Mention{{Name: "Siti", ID: "siti@lid"}}, testMembers)
	if p := in.Problem(); p != "" {
		t.Fatalf("problem %q", p)
	}
}

func TestParseMissing(t *testing.T) {
	in, ok := Parse("/kick ", 6, nil, testMembers)
	if !ok || in.Current != 0 || in.Word != "" || len(in.Missing()) != 1 || in.Problem() == "" {
		t.Fatalf("Parse(/kick ) = %+v", in)
	}
}

func TestParseContacts(t *testing.T) {
	s := "/add +62 812 5550 1234, @Mom 0812"
	in, _ := Parse(s, len([]rune(s)), []Mention{{Name: "Mom", ID: "mom@lid"}}, testMembers)
	vs := in.Values[0]
	if len(vs) != 3 {
		t.Fatalf("values = %+v", vs)
	}
	if vs[0].Text != "+62 812 5550 1234" || vs[0].ID != "" || vs[0].Err != "" {
		t.Errorf("phone = %+v", vs[0])
	}
	if vs[1].ID != "mom@lid" {
		t.Errorf("contact = %+v", vs[1])
	}
	if vs[2].Err == "" {
		t.Errorf("short number accepted: %+v", vs[2])
	}
}

func TestParseChoiceAndText(t *testing.T) {
	in, _ := Parse("/lockdown OFF", 13, nil, testMembers)
	if v := in.Values[0]; len(v) != 1 || v[0].Text != "off" {
		t.Fatalf("mode = %+v", v)
	}
	in, _ = Parse("/lockdown", 9, nil, testMembers)
	if !in.Naming {
		t.Fatal("not naming")
	}
	in, _ = Parse("/lockdown maybe", 15, nil, testMembers)
	if len(in.Extra) != 1 || in.Problem() == "" {
		t.Fatalf("maybe accepted: %+v", in)
	}
	s := "/description Hello  *world*  "
	in, _ = Parse(s, len([]rune(s)), nil, testMembers)
	if v := in.Values[0]; len(v) != 1 || v[0].Text != "Hello  *world*" {
		t.Fatalf("text = %+v", v)
	}
	if in.Current != 0 {
		t.Fatalf("current %d", in.Current)
	}
}

func TestUsage(t *testing.T) {
	if u := Lookup("lockdown").Usage(); u != "/lockdown [mode]" {
		t.Errorf("usage %q", u)
	}
	if u := Lookup("kick").Usage(); u != "/kick member" {
		t.Errorf("usage %q", u)
	}
}

func TestMatching(t *testing.T) {
	if got := Matching("", false); len(got) != 1 || got[0].Name != "sticker" {
		t.Errorf("outside groups: %v", got)
	}
	if got := Matching("d", true); len(got) != 2 {
		t.Errorf("d: %v", got)
	}
}

func TestParseSeparator(t *testing.T) {
	for _, c := range []struct {
		text, top, bottom string
		current           int
	}{
		{"/sticker When the code#works ", "When the code", "works", 1},
		{"/sticker  only the top", "only the top", "", 0},
		{"/sticker #only the bottom", "", "only the bottom", 1},
		{"/sticker top #", "top", "", 1},
		{"/sticker a#b#c", "a", "b#c", 1},
	} {
		in, ok := Parse(c.text, len([]rune(c.text)), nil, nil)
		if !ok || in.Problem() != "" {
			t.Errorf("%q: ok %v, problem %q", c.text, ok, in.Problem())
			continue
		}
		get := func(i int) string {
			if len(in.Values[i]) == 0 {
				return ""
			}
			return in.Values[i][0].Text
		}
		if get(0) != c.top || get(1) != c.bottom || in.Current != c.current {
			t.Errorf("%q: top %q, bottom %q, current %d; want %q, %q, %d",
				c.text, get(0), get(1), in.Current, c.top, c.bottom, c.current)
		}
	}
}
