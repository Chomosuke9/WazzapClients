package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestVotePoll checks that clicking an option votes for it, that another
// option takes its place in a poll of one choice, and that clicking your
// pick again takes the vote back.
func TestVotePoll(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("design")
	u.applyEvents()
	poll := func() *model.Message {
		t.Helper()
		for _, m := range u.msgs {
			if m.Poll != nil {
				return m
			}
		}
		t.Fatal("no poll in the chat")
		return nil
	}
	votes := func() (out []int) {
		for _, o := range poll().Poll.Options {
			out = append(out, o.Votes)
		}
		return out
	}
	start := votes()
	u.votePoll(poll(), 1)
	u.applyEvents()
	if p := poll().Poll; !p.Options[1].Mine || p.Options[1].Votes != start[1]+1 {
		t.Fatalf("after voting: %+v", p.Options)
	}
	u.votePoll(poll(), 0)
	u.applyEvents()
	if p := poll().Poll; p.Options[1].Mine || !p.Options[0].Mine || p.Options[1].Votes != start[1] ||
		p.Options[0].Votes != start[0]+1 {
		t.Fatalf("after changing the vote: %+v", p.Options)
	}
	u.votePoll(poll(), 0)
	u.applyEvents()
	if got := votes(); got[0] != start[0] || poll().Poll.Options[0].Mine {
		t.Fatalf("after taking it back: %v, want %v", got, start)
	}

	// The votes panel lists who voted.
	u.pressCardButton(poll(), cardButtons(poll())[0])
	if !u.msgInfo.open || !u.msgInfo.votes || len(u.msgInfo.voteList) == 0 {
		t.Errorf("View votes opened %+v", u.msgInfo)
	}
}

// TestVoteMultiple checks a poll of any number of choices: each click
// adds or removes one.
func TestVoteMultiple(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("design")
	u.applyEvents()
	m := u.backend.SendPoll("design", model.Poll{Question: "Snacks?", Options: []string{"A", "B", "C"}, Multiple: true})
	u.upsertMessage(m)
	u.applyEvents()
	find := func() *model.Message {
		for _, x := range u.msgs {
			if x.ID == m.ID {
				return x
			}
		}
		t.Fatal("the poll isn't loaded")
		return nil
	}
	u.votePoll(find(), 0)
	u.applyEvents()
	u.votePoll(find(), 2)
	u.applyEvents()
	if o := find().Poll.Options; !o[0].Mine || o[1].Mine || !o[2].Mine || find().Poll.Voters != 1 {
		t.Fatalf("after two picks: %+v", o)
	}
	u.votePoll(find(), 0)
	u.applyEvents()
	if o := find().Poll.Options; o[0].Mine || !o[2].Mine {
		t.Fatalf("after taking one back: %+v", o)
	}
}

func TestCardButtons(t *testing.T) {
	one := &model.Message{Media: model.MediaContact, Contacts: []model.ContactCard{
		{Name: "Joko", Phones: []model.ContactPhone{{Number: "+62 812", WAID: "62812"}}}}}
	if b := cardButtons(one); len(b) != 1 || b[0].Kind != buttonMessage || b[0].Value != "62812" {
		t.Errorf("contact on WhatsApp: %+v", b)
	}
	off := &model.Message{Media: model.MediaContact, Contacts: []model.ContactCard{{Name: "Joko"}}}
	if b := cardButtons(off); len(b) != 0 {
		t.Errorf("contact not on WhatsApp: %+v", b)
	}
	old := &model.Message{Media: model.MediaPoll, Text: "Stored before polls were kept"}
	if hasCard(old) || len(cardButtons(old)) != 0 {
		t.Error("a poll without its options shows as a card")
	}
	quiet := &model.Message{Media: model.MediaEventInvite, Event: &model.EventInfo{Name: "Picnic"}}
	if b := cardButtons(quiet); len(b) != 0 {
		t.Errorf("an event nobody answered: %+v", b)
	}
}
