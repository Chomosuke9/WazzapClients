package wa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"github.com/polymorfa/hypermeow/util/gcmutil"
	"github.com/polymorfa/hypermeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Polls, locations, contact cards and events: what their messages carry
// besides text, kept as JSON in the extra column, and the votes and
// answers polls and events get, in wz_votes.

const votesSchema = `
CREATE TABLE IF NOT EXISTS wz_votes (
	chat   TEXT NOT NULL,
	id     TEXT NOT NULL,            -- the poll or event
	who    TEXT NOT NULL,            -- the voter, usually a LID; meVoter for you
	ts     INTEGER NOT NULL,         -- unix milliseconds of the vote
	choice TEXT NOT NULL DEFAULT '', -- a poll's option hashes (hex, comma separated), or an event's model.RSVP
	PRIMARY KEY (chat, id, who)
);
`

// meVoter is who your own votes are stored under: they come from your
// phone number or your LID, and must replace each other.
const meVoter = "me"

// extraInfo is the extra column.
type extraInfo struct {
	Poll     *pollDef            `json:"poll,omitempty"`
	Loc      *model.Location     `json:"loc,omitempty"`
	Contacts []model.ContactCard `json:"contacts,omitempty"`
	Event    *eventDef           `json:"event,omitempty"`
}

// pollDef is a poll's options. Max is how many a voter may pick, 0 for
// any number.
type pollDef struct {
	Options []string `json:"o"`
	Max     int      `json:"max,omitempty"`
}

// eventDef is an event as created (or last edited); Start and End are
// unix seconds.
type eventDef struct {
	Name     string          `json:"name,omitempty"`
	Desc     string          `json:"desc,omitempty"`
	Start    int64           `json:"start,omitempty"`
	End      int64           `json:"end,omitempty"`
	Place    *model.Location `json:"place,omitempty"`
	Join     string          `json:"join,omitempty"`
	Canceled bool            `json:"canceled,omitempty"`
}

func (x extraInfo) empty() bool {
	return x.Poll == nil && x.Loc == nil && len(x.Contacts) == 0 && x.Event == nil
}

func (x extraInfo) marshal() string {
	if x.empty() {
		return ""
	}
	b, _ := json.Marshal(x)
	return string(b)
}

func parseExtra(s string) extraInfo {
	var x extraInfo
	if s != "" {
		_ = json.Unmarshal([]byte(s), &x)
	}
	return x
}

// apply puts what x describes on m; a poll's and an event's counts come
// from fillVotes.
func (x extraInfo) apply(m *model.Message) {
	m.Location, m.Contacts = x.Loc, x.Contacts
	if p := x.Poll; p != nil {
		m.Poll = &model.PollState{Max: p.Max}
		for _, o := range p.Options {
			m.Poll.Options = append(m.Poll.Options, model.PollOption{Name: o})
		}
	}
	if e := x.Event; e != nil {
		m.Event = &model.EventInfo{Name: e.Name, Description: e.Desc, Place: e.Place, JoinLink: e.Join,
			Canceled: e.Canceled, Start: unixTime(e.Start), End: unixTime(e.End)}
	}
}

func unixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// extraOf is what a message keeps in the extra column.
func extraOf(m *model.Message) extraInfo {
	x := extraInfo{Loc: m.Location, Contacts: m.Contacts}
	if p := m.Poll; p != nil {
		x.Poll = &pollDef{Max: p.Max}
		for _, o := range p.Options {
			x.Poll.Options = append(x.Poll.Options, o.Name)
		}
	}
	if e := m.Event; e != nil {
		x.Event = &eventDef{Name: e.Name, Desc: e.Description, Place: e.Place, Join: e.JoinLink, Canceled: e.Canceled}
		if !e.Start.IsZero() {
			x.Event.Start = e.Start.Unix()
		}
		if !e.End.IsZero() {
			x.Event.End = e.End.Unix()
		}
	}
	return x
}

// pollContent describes a poll.
func pollContent(e *waE2E.PollCreationMessage) content {
	d := &pollDef{Max: int(e.GetSelectableOptionsCount())}
	for _, o := range e.GetOptions() {
		d.Options = append(d.Options, o.GetOptionName())
	}
	if d.Max >= len(d.Options) && len(d.Options) > 1 {
		d.Max = 0 // as many as there are: any number
	}
	return content{text: e.GetName(), media: model.MediaPoll, ctx: e.GetContextInfo(), extra: extraInfo{Poll: d}}
}

// locationOf reads a location message.
func locationOf(e *waE2E.LocationMessage) *model.Location {
	return &model.Location{Lat: e.GetDegreesLatitude(), Lng: e.GetDegreesLongitude(), Name: e.GetName(),
		Address: e.GetAddress(), URL: e.GetURL(), Live: e.GetIsLive()}
}

// eventContent describes an event.
func eventContent(e *waE2E.EventMessage) content {
	d := &eventDef{Name: e.GetName(), Desc: e.GetDescription(), Start: e.GetStartTime(), End: e.GetEndTime(),
		Join: e.GetJoinLink(), Canceled: e.GetIsCanceled()}
	if l := e.GetLocation(); l != nil && (l.GetName() != "" || l.GetAddress() != "" ||
		l.GetDegreesLatitude() != 0 || l.GetDegreesLongitude() != 0) {
		d.Place = locationOf(l)
	}
	return content{text: e.GetName(), media: model.MediaEventInvite, ctx: e.GetContextInfo(), extra: extraInfo{Event: d}}
}

// parseVCard reads the name and phone numbers of a contact card; name is
// used when the card has none.
func parseVCard(card, name string) model.ContactCard {
	c := model.ContactCard{Name: name}
	// Lines that start with a space continue the one before.
	card = strings.NewReplacer("\r\n ", "", "\n ", "", "\r\n\t", "", "\n\t", "").Replace(card)
	for _, line := range strings.FieldsFunc(card, func(r rune) bool { return r == '\n' || r == '\r' }) {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		params := strings.Split(key, ";")
		prop := strings.ToUpper(params[0])
		if i := strings.LastIndexByte(prop, '.'); i >= 0 {
			prop = prop[i+1:] // item1.TEL
		}
		switch prop {
		case "FN":
			if c.Name == "" {
				c.Name = unescapeVCard(value)
			}
		case "TEL":
			p := model.ContactPhone{Number: strings.TrimSpace(value)}
			for _, pr := range params[1:] {
				if k, v, ok := strings.Cut(pr, "="); ok && strings.EqualFold(k, "waid") {
					p.WAID = v
				}
			}
			if p.Number != "" || p.WAID != "" {
				c.Phones = append(c.Phones, p)
			}
		}
	}
	return c
}

func unescapeVCard(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// optionHash is how a vote names a poll option.
func optionHash(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])
}

func hashChoice(hashes [][]byte) string {
	parts := make([]string, len(hashes))
	for i, h := range hashes {
		parts[i] = hex.EncodeToString(h)
	}
	return strings.Join(parts, ",")
}

// pollPicks turns a stored poll vote into option indexes.
func pollPicks(p *model.PollState, choice string) []int {
	var out []int
	if choice == "" {
		return nil
	}
	for _, h := range strings.Split(choice, ",") {
		for i, o := range p.Options {
			if optionHash(o.Name) == h && !slices.Contains(out, i) {
				out = append(out, i)
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

func rsvpOf(t waE2E.EventResponseMessage_EventResponseType) model.RSVP {
	switch t {
	case waE2E.EventResponseMessage_GOING:
		return model.RSVPGoing
	case waE2E.EventResponseMessage_NOT_GOING:
		return model.RSVPNotGoing
	case waE2E.EventResponseMessage_MAYBE:
		return model.RSVPMaybe
	}
	return model.RSVPNone
}

// vote is a vote in a poll or an answer to an event, for p.target.
type vote struct {
	who    string
	ts     int64 // unix milliseconds
	choice string
}

func (s *msgStore) putVote(ctx context.Context, x execer, chat, id string, v vote) error {
	_, err := x.ExecContext(ctx, `INSERT INTO wz_votes (chat, id, who, ts, choice) VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT (chat, id, who) DO UPDATE SET ts = excluded.ts, choice = excluded.choice
		WHERE excluded.ts >= wz_votes.ts`, chat, id, v.who, v.ts, v.choice)
	return err
}

// votes returns the votes of a poll or event, newest first.
func (s *msgStore) votes(ctx context.Context, chat, id string) []vote {
	rows, err := s.db.QueryContext(ctx, `SELECT who, ts, choice FROM wz_votes WHERE chat = ? AND id = ? ORDER BY ts DESC`,
		chat, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []vote
	for rows.Next() {
		var v vote
		if rows.Scan(&v.who, &v.ts, &v.choice) == nil {
			out = append(out, v)
		}
	}
	return out
}

// historyVote is a vote that history sync brought, on poll or event id.
type historyVote struct {
	id string
	vote
}

// voterOf is who a vote from j is stored under.
func (b *Backend) voterOf(ctx context.Context, j types.JID) string {
	if b.isMe(j.ToNonAD()) {
		return meVoter
	}
	return b.canonical(ctx, j).String()
}

// voterOfKey is who sent the message of key, in chat.
func (b *Backend) voterOfKey(ctx context.Context, chat types.JID, key *waCommon.MessageKey) (string, bool) {
	if key.GetFromMe() {
		return meVoter, true
	}
	s := key.GetParticipant()
	if s == "" {
		s = key.GetRemoteJID()
	}
	j, err := types.ParseJID(s)
	if err != nil || j.IsEmpty() {
		if chat.Server == types.GroupServer {
			return "", false
		}
		j = chat
	}
	return b.voterOf(ctx, j), true
}

// parseVote reads a poll vote or an answer to an event into p.
func (b *Backend) parseVote(ctx context.Context, evt *events.Message, p *parsed) bool {
	cli := b.client()
	if cli == nil {
		return false
	}
	m := evt.Message
	ts := evt.Info.Timestamp.UnixMilli()
	if pu := m.GetPollUpdateMessage(); pu != nil {
		v, err := cli.DecryptPollVote(ctx, evt)
		if err != nil {
			b.log.Warnf("decrypt vote in %s: %v", evt.Info.Chat, err)
			return false
		}
		if t := pu.GetSenderTimestampMS(); t > 0 {
			ts = t
		}
		p.target = pu.GetPollCreationMessageKey().GetID()
		p.vote = &vote{who: b.voterOf(ctx, evt.Info.Sender), ts: ts, choice: hashChoice(v.GetSelectedOptions())}
		return p.target != ""
	}
	er := m.GetEncEventResponseMessage()
	r, err := decryptEventResponse(ctx, cli, evt)
	if err != nil {
		b.log.Warnf("decrypt event response in %s: %v", evt.Info.Chat, err)
		return false
	}
	if t := r.GetTimestampMS(); t > 0 {
		ts = t
	}
	p.target = er.GetEventCreationMessageKey().GetID()
	p.vote = &vote{who: b.voterOf(ctx, evt.Info.Sender), ts: ts, choice: strconv.Itoa(int(rsvpOf(r.GetResponse())))}
	return p.target != ""
}

// decryptEventResponse decrypts an answer to an event. hypermeow decrypts
// poll votes but not these; they are encrypted the same way, with the
// event's message secret.
func decryptEventResponse(ctx context.Context, cli *whatsmeow.Client, evt *events.Message) (*waE2E.EventResponseMessage, error) {
	er := evt.Message.GetEncEventResponseMessage()
	key := er.GetEventCreationMessageKey()
	orig, err := origSender(evt, key)
	if err != nil {
		return nil, err
	}
	secret, stored, err := cli.Store.MsgSecrets.GetMessageSecret(ctx, evt.Info.Chat, orig, key.GetID())
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, whatsmeow.ErrOriginalMessageSecretNotFound
	}
	sender := evt.Info.Sender.ToNonAD().String()
	open := func(orig types.JID) ([]byte, error) {
		info := key.GetID() + orig.ToNonAD().String() + sender + string(whatsmeow.EncSecretEventResponse)
		k := hkdfutil.SHA256(secret, nil, []byte(info), 32)
		return gcmutil.Decrypt(k, er.GetEncIV(), er.GetEncPayload(), fmt.Appendf(nil, "%s\x00%s", key.GetID(), sender))
	}
	plain, err := open(orig)
	if err != nil && !stored.IsEmpty() && stored != orig {
		// The secret may have come from the event's sender under their
		// other JID (LID or phone number).
		plain, err = open(stored)
	}
	if err != nil {
		return nil, err
	}
	var r waE2E.EventResponseMessage
	if err := proto.Unmarshal(plain, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// origSender is who sent the message key points to, from a message in
// the same chat (as hypermeow works it out for poll votes).
func origSender(evt *events.Message, key *waCommon.MessageKey) (types.JID, error) {
	if key.GetFromMe() {
		return evt.Info.Sender, nil
	}
	s := key.GetParticipant()
	if evt.Info.Chat.Server == types.DefaultUserServer || evt.Info.Chat.Server == types.HiddenUserServer {
		s = key.GetRemoteJID()
	}
	j, err := types.ParseJID(s)
	if err != nil || j.IsEmpty() {
		return types.EmptyJID, fmt.Errorf("no sender in the event's key %q", s)
	}
	return j, nil
}

// historyVotes reads the votes and answers that history sync hands out
// with a poll or event message, already decrypted.
func (b *Backend) historyVotes(ctx context.Context, chat types.JID, w *waWeb.WebMessageInfo) []vote {
	var out []vote
	for _, pu := range w.GetPollUpdates() {
		who, ok := b.voterOfKey(ctx, chat, pu.GetPollUpdateMessageKey())
		if !ok {
			continue
		}
		out = append(out, vote{who: who, ts: pu.GetSenderTimestampMS(), choice: hashChoice(pu.GetVote().GetSelectedOptions())})
	}
	for _, r := range w.GetEventResponses() {
		who, ok := b.voterOfKey(ctx, chat, r.GetEventResponseMessageKey())
		if !ok {
			continue
		}
		ts := r.GetTimestampMS()
		if t := r.GetEventResponseMessage().GetTimestampMS(); t > 0 {
			ts = t
		}
		out = append(out, vote{who: who, ts: ts,
			choice: strconv.Itoa(int(rsvpOf(r.GetEventResponseMessage().GetResponse())))})
	}
	return out
}

// fillVotes counts a poll's or event's votes.
func (b *Backend) fillVotes(ctx context.Context, m *model.Message) {
	if m.Poll == nil && m.Event == nil {
		return
	}
	for _, v := range b.store.votes(ctx, m.ChatID, m.ID) {
		mine := v.who == meVoter
		if p := m.Poll; p != nil {
			picks := pollPicks(p, v.choice)
			if len(picks) > 0 {
				p.Voters++
			}
			face := v.who
			if mine {
				face = b.ownJID(m.ChatID).String()
			}
			for _, i := range picks {
				o := &p.Options[i]
				o.Votes++
				o.Mine = o.Mine || mine
				if len(o.Faces) < 3 {
					o.Faces = append(o.Faces, face)
				}
			}
		}
		if e := m.Event; e != nil {
			r := model.RSVP(atoiOr(v.choice, 0))
			switch r {
			case model.RSVPGoing:
				e.Going++
			case model.RSVPMaybe:
				e.Maybe++
			case model.RSVPNotGoing:
				e.NotGoing++
			}
			if mine {
				e.Mine = r
			}
		}
	}
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// Votes implements model.Backend.
func (b *Backend) Votes(m *model.Message) []model.Vote {
	ctx := b.ctx
	raw, ok := b.store.message(ctx, m.ChatID, m.ID)
	if !ok {
		return nil
	}
	var out []model.Vote
	for _, v := range b.store.votes(ctx, m.ChatID, m.ID) {
		mv := model.Vote{ID: v.who, Me: v.who == meVoter, Time: time.UnixMilli(v.ts)}
		if mv.Me {
			mv.ID, mv.Name = b.ownJID(m.ChatID).String(), "You"
		} else {
			mv.Name = b.senderNameStr(ctx, v.who, "", "")
		}
		switch {
		case raw.Poll != nil:
			if mv.Options = pollPicks(raw.Poll, v.choice); len(mv.Options) == 0 {
				continue // taken back
			}
		case raw.Event != nil:
			if mv.RSVP = model.RSVP(atoiOr(v.choice, 0)); mv.RSVP == model.RSVPNone {
				continue
			}
		default:
			return nil
		}
		out = append(out, mv)
	}
	return out
}

// VotePoll implements model.Backend.
func (b *Backend) VotePoll(m *model.Message, options []int) {
	cli := b.connected()
	if cli == nil || m.Poll == nil {
		return
	}
	ctx := b.ctx
	raw, ok := b.store.message(ctx, m.ChatID, m.ID)
	if !ok || raw.Poll == nil {
		return
	}
	var names []string
	for _, i := range options {
		if i >= 0 && i < len(raw.Poll.Options) {
			names = append(names, raw.Poll.Options[i].Name)
		}
	}
	jid, _ := types.ParseJID(m.ChatID)
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jid, Sender: b.senderOf(m), IsFromMe: m.FromMe,
			IsGroup: jid.Server == types.GroupServer},
		ID: m.ID,
	}
	msg, err := cli.BuildPollVote(ctx, info, names)
	if err != nil {
		b.log.Warnf("vote in %s: %v", m.ChatID, err)
		b.emit(model.NoticeEvent{Text: "Couldn't vote in this poll."})
		return
	}
	v := vote{who: meVoter, ts: msg.GetPollUpdateMessage().GetSenderTimestampMS(),
		choice: hashChoice(whatsmeow.HashPollOptions(names))}
	if err := b.store.putVote(ctx, b.db, m.ChatID, m.ID, v); err != nil {
		b.log.Warnf("store vote in %s: %v", m.ChatID, err)
	}
	b.emitMessage(m.ChatID, m.ID)
	go func() {
		if _, err := cli.SendMessage(b.ctx, jid, msg); err != nil {
			b.log.Warnf("send vote in %s: %v", m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't send your vote."})
		}
	}()
}

// editEvent applies an edit of an event (a new date, or canceling it).
func (s *msgStore) editEvent(ctx context.Context, chat, id string, e *eventDef, payload ...[]byte) error {
	var raw []byte
	if len(payload) > 0 {
		raw = payload[0]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET extra = ?, text = ?, raw_payload = ? WHERE chat = ? AND id = ? AND media = ?`,
		extraInfo{Event: e}.marshal(), e.Name, raw, chat, id, int(model.MediaEventInvite))
	return err
}

// cardMessage rebuilds a location or contact message to send again, or
// returns nil for anything else. A live location can't be.
func cardMessage(m *model.Message) *waE2E.Message {
	switch {
	case m.Media == model.MediaLocation && m.Location != nil && !m.Location.Live:
		l := m.Location
		e := &waE2E.LocationMessage{DegreesLatitude: proto.Float64(l.Lat), DegreesLongitude: proto.Float64(l.Lng),
			JPEGThumbnail: m.Thumb}
		if l.Name != "" {
			e.Name = proto.String(l.Name)
		}
		if l.Address != "" {
			e.Address = proto.String(l.Address)
		}
		if l.URL != "" {
			e.URL = proto.String(l.URL)
		}
		return &waE2E.Message{LocationMessage: e}
	case m.Media == model.MediaContact && len(m.Contacts) > 0:
		var cards []*waE2E.ContactMessage
		for _, c := range m.Contacts {
			cards = append(cards, &waE2E.ContactMessage{DisplayName: proto.String(c.Name), Vcard: proto.String(cardVCard(c))})
		}
		if len(cards) == 1 {
			return &waE2E.Message{ContactMessage: cards[0]}
		}
		return &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String(m.Text), Contacts: cards}}
	}
	return nil
}

// cardVCard writes a contact card back out as a vCard.
func cardVCard(c model.ContactCard) string {
	esc := strings.NewReplacer(`\`, `\`, ",", `\,`, ";", `\;`, "\n", `\n`).Replace(c.Name)
	var sb strings.Builder
	sb.WriteString("BEGIN:VCARD\nVERSION:3.0\nN:;" + esc + ";;;\nFN:" + esc + "\n")
	for _, p := range c.Phones {
		if p.WAID != "" {
			sb.WriteString("TEL;type=CELL;type=VOICE;waid=" + p.WAID + ":" + first(p.Number, formatPhone(p.WAID)) + "\n")
		} else {
			sb.WriteString("TEL;type=CELL:" + p.Number + "\n")
		}
	}
	sb.WriteString("END:VCARD")
	return sb.String()
}
