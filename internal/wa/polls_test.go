package wa

import (
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestPollVotes checks that a poll keeps its options, that each voter's
// newest vote counts (whatever order votes come in) and that taking a vote
// back takes it off the count.
func TestPollVotes(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	c := pollContent(&waE2E.PollCreationMessage{Name: proto.String("Lunch?"), SelectableOptionsCount: proto.Uint32(1),
		Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Pizza")}, {OptionName: proto.String("Sushi")}}})
	m := &model.Message{ID: "p", ChatID: "g@g.us", Text: c.text, Media: c.media, Time: time.Unix(100, 0)}
	c.extra.apply(m)
	if err := b.store.ensureChat(ctx, b.db, m.ChatID, true, "G"); err != nil {
		t.Fatal(err)
	}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	pick := func(names ...string) string {
		var h [][]byte
		for _, n := range names {
			x, _ := hex.DecodeString(optionHash(n))
			h = append(h, x)
		}
		return hashChoice(h)
	}
	for _, v := range []vote{
		{who: "a@lid", ts: 2000, choice: pick("Sushi")},
		{who: "a@lid", ts: 1000, choice: pick("Pizza")}, // older: ignored
		{who: "b@lid", ts: 1500, choice: pick("Pizza")},
		{who: "c@lid", ts: 1600, choice: pick("Pizza")},
		{who: "c@lid", ts: 1700, choice: ""}, // taken back
		{who: meVoter, ts: 1800, choice: pick("Sushi")},
	} {
		if err := b.store.putVote(ctx, b.db, m.ChatID, m.ID, v); err != nil {
			t.Fatal(err)
		}
	}
	got := b.Messages(m.ChatID, 10)
	if len(got) != 1 || got[0].Poll == nil {
		t.Fatalf("messages = %+v, want the poll", got)
	}
	p := got[0].Poll
	if p.Max != 1 || p.Multiple() || len(p.Options) != 2 || p.Options[0].Name != "Pizza" || p.Options[1].Name != "Sushi" {
		t.Fatalf("poll = %+v", p)
	}
	if p.Voters != 3 || p.Options[0].Votes != 1 || p.Options[1].Votes != 2 || p.Options[0].Mine || !p.Options[1].Mine {
		t.Errorf("counts = %d voters, %+v", p.Voters, p.Options)
	}
	votes := b.Votes(got[0])
	if len(votes) != 3 || !votes[1].Me || votes[0].ID != "a@lid" || len(votes[2].Options) != 1 || votes[2].Options[0] != 0 {
		t.Errorf("votes = %+v", votes)
	}
}

// TestEventAnswers checks an event's counts and its edits.
func TestEventAnswers(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	c := eventContent(&waE2E.EventMessage{Name: proto.String("Picnic"), StartTime: proto.Int64(5000),
		Location: &waE2E.LocationMessage{Name: proto.String("Park")}})
	m := &model.Message{ID: "e", ChatID: "g@g.us", Text: c.text, Media: c.media, Time: time.Unix(100, 0)}
	c.extra.apply(m)
	_ = b.store.ensureChat(ctx, b.db, m.ChatID, true, "G")
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []vote{
		{who: "a@lid", ts: 1, choice: strconv.Itoa(int(model.RSVPGoing))},
		{who: "b@lid", ts: 2, choice: strconv.Itoa(int(model.RSVPMaybe))},
		{who: meVoter, ts: 3, choice: strconv.Itoa(int(model.RSVPGoing))},
	} {
		_ = b.store.putVote(ctx, b.db, m.ChatID, m.ID, v)
	}
	if err := b.store.editEvent(ctx, m.ChatID, m.ID, &eventDef{Name: "Picnic!", Start: 6000, Canceled: true}); err != nil {
		t.Fatal(err)
	}
	// The event coming again (history) keeps the edit.
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	e := b.Messages(m.ChatID, 10)[0].Event
	if e == nil || e.Name != "Picnic!" || !e.Canceled || e.Start.Unix() != 6000 {
		t.Fatalf("event = %+v", e)
	}
	if e.Going != 2 || e.Maybe != 1 || e.NotGoing != 0 || e.Mine != model.RSVPGoing {
		t.Errorf("answers = %+v", e)
	}
}

func TestParseVCard(t *testing.T) {
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:;Budi;;;\r\nFN:Budi\\, the\r\n  courier\r\nitem1.TEL;waid=6281234567890:+62 812-3456-7890\r\n" +
		"item1.X-ABLabel:Mobile\r\nTEL;type=HOME:021 555 1234\r\nEND:VCARD"
	c := parseVCard(card, "")
	if c.Name != "Budi, the courier" || len(c.Phones) != 2 || c.WhatsApp() != "6281234567890" ||
		c.Phones[0].Number != "+62 812-3456-7890" || c.Phones[1].WAID != "" {
		t.Errorf("card = %+v", c)
	}
	// Writing it back out keeps it.
	if back := parseVCard(cardVCard(c), ""); back.Name != c.Name || len(back.Phones) != 2 || back.WhatsApp() != c.WhatsApp() {
		t.Errorf("round trip = %+v", back)
	}
}
