// Package auto sends messages for you: the ones you schedule (/schedule)
// and, while you're away (/afk), a reply to whoever messages or mentions
// you. It wraps the Backend, so it sees every event and every message you
// send, and keeps its state in the backend's prefs: it goes on with the
// window closed, and after a restart.
//
// Like the Backend, it's used from the UI goroutine only; its timer just
// pokes the backend's notify function, and the work happens in Poll.
package auto

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Pref keys for the state it keeps.
const (
	prefJobs = "auto_scheduled"
	prefAway = "auto_away"
)

// Job is a scheduled message.
type Job struct {
	ID   string    `json:"id"`
	Chat string    `json:"chat"`
	At   time.Time `json:"at"`
	// Text is the message as sent, with mentions as "@<user>", and Shown
	// as you typed it ("@Budi"), for listing it.
	Text          string   `json:"text"`
	Shown         string   `json:"shown,omitempty"`
	Mentions      []string `json:"mentions,omitempty"`
	MentionAll    bool     `json:"all,omitempty"`
	MentionAdmins bool     `json:"admins,omitempty"`
	// Reply is the ID of the message it replies to, or "".
	Reply string `json:"reply,omitempty"`
}

// Away is the AFK state.
type Away struct {
	Reason string    `json:"reason,omitempty"`
	Since  time.Time `json:"since"`
	// Told lists who got the reply, as "chat|sender": each once.
	Told []string `json:"told,omitempty"`
}

// Backend is a model.Backend that also sends scheduled messages and AFK
// replies.
type Backend struct {
	model.Backend
	now    func() time.Time
	jobs   []Job // by At
	away   *Away
	meID   string
	online bool
	// ours are the IDs of the messages it sent: they don't end AFK.
	ours map[string]bool
	// groups caches which chats are groups (see isGroup).
	groups map[string]bool
	out    []model.Event // for the next Poll
	notify func()
	timer  *time.Timer
	seq    int
}

// Wrap returns b with scheduled messages and AFK, reading the time from
// now.
func Wrap(b model.Backend, now func() time.Time) *Backend {
	a := &Backend{Backend: b, now: now, ours: map[string]bool{}, groups: map[string]bool{}}
	if s := b.Pref(prefJobs); s != "" {
		json.Unmarshal([]byte(s), &a.jobs)
	}
	if s := b.Pref(prefAway); s != "" {
		var w Away
		if json.Unmarshal([]byte(s), &w) == nil {
			a.away = &w
		}
	}
	return a
}

// Start starts the backend.
func (a *Backend) Start(notify func()) {
	a.notify = notify
	a.Backend.Start(notify)
	a.arm()
}

// Close stops the backend.
func (a *Backend) Close() {
	if a.timer != nil {
		a.timer.Stop()
	}
	a.Backend.Close()
}

// Poll returns the backend's events, plus the messages it sent and its
// notices.
func (a *Backend) Poll() []model.Event {
	evs := a.Backend.Poll()
	for _, ev := range evs {
		switch e := ev.(type) {
		case model.ConnEvent:
			if e.MeID != "" {
				a.meID = e.MeID
			}
			a.online = e.State == model.StateOnline
		case model.ChatsEvent:
			for _, c := range e.Chats {
				a.groups[c.ID] = c.IsGroup
			}
		case model.ChatEvent:
			a.groups[e.Chat.ID] = e.Chat.IsGroup
		case model.MessageEvent:
			a.seen(e)
		}
	}
	a.sendDue()
	if len(a.out) > 0 {
		evs = append(evs, a.out...)
		a.out = nil
	}
	return evs
}

// Schedule adds j to the queue, giving it an ID, and returns it.
func (a *Backend) Schedule(j Job) Job {
	a.seq++
	j.ID = strconv.FormatInt(a.now().UnixNano(), 36) + "-" + strconv.Itoa(a.seq)
	i := 0
	for i < len(a.jobs) && !a.jobs[i].At.After(j.At) {
		i++
	}
	a.jobs = append(a.jobs[:i:i], append([]Job{j}, a.jobs[i:]...)...)
	a.saveJobs()
	a.arm()
	return j
}

// Jobs lists the messages scheduled in chat ("" for every chat), soonest
// first.
func (a *Backend) Jobs(chat string) []Job {
	var out []Job
	for _, j := range a.jobs {
		if chat == "" || j.Chat == chat {
			out = append(out, j)
		}
	}
	return out
}

// Cancel takes the job with id off the queue, and reports whether it was
// still on it.
func (a *Backend) Cancel(id string) bool {
	for i, j := range a.jobs {
		if j.ID == id {
			a.jobs = append(a.jobs[:i:i], a.jobs[i+1:]...)
			a.saveJobs()
			a.arm()
			return true
		}
	}
	return false
}

// SendNow sends the job with id right away, and returns the message (nil
// when the job is gone).
func (a *Backend) SendNow(id string) *model.Message {
	for i, j := range a.jobs {
		if j.ID == id {
			a.jobs = append(a.jobs[:i:i], a.jobs[i+1:]...)
			a.saveJobs()
			a.arm()
			return a.send(j)
		}
	}
	return nil
}

// sendDue sends the jobs whose time has come, once online.
func (a *Backend) sendDue() {
	if !a.online {
		return
	}
	now := a.now()
	n := 0
	for n < len(a.jobs) && !a.jobs[n].At.After(now) {
		n++
	}
	if n == 0 {
		return
	}
	due := a.jobs[:n:n]
	a.jobs = append([]Job(nil), a.jobs[n:]...)
	a.saveJobs()
	for _, j := range due {
		if m := a.send(j); m != nil {
			a.out = append(a.out, model.MessageEvent{Msg: m})
		}
	}
	a.arm()
}

func (a *Backend) send(j Job) *model.Message {
	d := model.Draft{Text: j.Text, Mentions: j.Mentions, MentionAll: j.MentionAll, MentionAdmins: j.MentionAdmins}
	if j.Reply != "" {
		if ms := a.Backend.MessagesFrom(j.Chat, j.Reply, 1); len(ms) > 0 && ms[0].ID == j.Reply {
			d.Reply = ms[0]
		}
	}
	m := a.Backend.Send(j.Chat, d)
	if m != nil {
		a.ours[m.ID] = true
	}
	return m
}

// arm sets the timer for the next job. It wakes at least hourly, in case
// the clock jumped (the computer slept).
func (a *Backend) arm() {
	if a.notify == nil {
		return
	}
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	if len(a.jobs) == 0 {
		return
	}
	d := min(max(a.jobs[0].At.Sub(a.now()), 0), time.Hour)
	a.timer = time.AfterFunc(d, a.notify)
}

func (a *Backend) saveJobs() {
	if len(a.jobs) == 0 {
		a.Backend.SetPref(prefJobs, "")
		return
	}
	if b, err := json.Marshal(a.jobs); err == nil {
		a.Backend.SetPref(prefJobs, string(b))
	}
}

// Away returns the AFK state, or nil when you're not away.
func (a *Backend) Away() *Away { return a.away }

// SetAway marks you away for reason, from now on, until you send a
// message.
func (a *Backend) SetAway(reason string) {
	a.away = &Away{Reason: reason, Since: a.now()}
	a.saveAway()
}

// Back ends AFK, and returns how many got the reply.
func (a *Backend) Back() int {
	if a.away == nil {
		return 0
	}
	n := len(a.away.Told)
	a.away = nil
	a.saveAway()
	return n
}

func (a *Backend) saveAway() {
	if a.away == nil {
		a.Backend.SetPref(prefAway, "")
		return
	}
	if b, err := json.Marshal(a.away); err == nil {
		a.Backend.SetPref(prefAway, string(b))
	}
}

// seen looks at a message coming in or going out.
func (a *Backend) seen(e model.MessageEvent) {
	w, m := a.away, e.Msg
	if w == nil || m == nil || m.Time.Before(w.Since) {
		return
	}
	if m.FromMe {
		if !a.ours[m.ID] {
			a.cameBack() // sent from another device
		}
		return
	}
	if !e.New || !a.answers(m) {
		return
	}
	who := m.SenderID
	if who == "" {
		who = m.Sender
	}
	key := m.ChatID + "|" + who
	for _, k := range w.Told {
		if k == key {
			return
		}
	}
	w.Told = append(w.Told, key)
	a.saveAway()
	if r := a.Backend.Send(m.ChatID, model.Draft{Text: AwayText(w, a.now()), Reply: m}); r != nil {
		a.ours[r.ID] = true
		a.out = append(a.out, model.MessageEvent{Msg: r})
	}
}

// answers reports whether m gets the AFK reply: a message to you, or one
// in a group that mentions you by name (not @all, nor a reply to you).
// Channels, status and broadcasts don't.
func (a *Backend) answers(m *model.Message) bool {
	id := m.ChatID
	switch {
	case strings.HasSuffix(id, "@newsletter"), strings.HasSuffix(id, "@broadcast"):
		return false
	case a.isGroup(id):
		return mentionsMe(m.Text)
	}
	return true
}

// mentionsMe reports whether text, as a backend resolved it, mentions you:
// a mention marked model.MentionNotifies that isn't "@all".
func mentionsMe(text string) bool {
	mark := string(model.MentionNotifies)
	for {
		i := strings.Index(text, mark)
		if i < 0 {
			return false
		}
		text = text[i+len(mark):]
		if !strings.HasPrefix(text, "@all\u2069") {
			return true
		}
	}
}

// isGroup reports whether chat id is a group, asking the backend about
// chats it hasn't heard of.
func (a *Backend) isGroup(id string) bool {
	if strings.HasSuffix(id, "@g.us") {
		return true
	}
	g, ok := a.groups[id]
	if !ok {
		for _, c := range a.Backend.Chats() {
			a.groups[c.ID] = c.IsGroup
		}
		g = a.groups[id]
		a.groups[id] = g
	}
	return g
}

// AwayText is the AFK reply.
func AwayText(w *Away, now time.Time) string {
	title, since := awayLines(w, now)
	return "💤 *AFK*" + title + "\n_" + since + "_"
}

// AwayPlain is the AFK reply without its formatting, as notes show it.
func AwayPlain(w *Away, now time.Time) string {
	title, since := awayLines(w, now)
	return "💤 AFK" + title + "\n" + since
}

func awayLines(w *Away, now time.Time) (reason, since string) {
	if w.Reason != "" {
		reason = ": " + w.Reason
	}
	t := w.Since.In(now.Location())
	layout := "15:04"
	switch y, m, d := t.Date(); {
	case y != now.Year():
		layout = "2 Jan 2006 15:04"
	case m != now.Month() || d != now.Day():
		layout = "Mon 2 Jan 15:04"
	}
	return reason, "Away since " + t.Format(layout)
}

// cameBack ends AFK because you sent a message, with a notice saying so.
func (a *Backend) cameBack() {
	n := a.Back()
	text := "Welcome back! You're no longer AFK."
	if n > 0 {
		text = "Welcome back! The AFK reply went to " + strconv.Itoa(n) + " "
		if n == 1 {
			text += "person."
		} else {
			text += "people."
		}
	}
	a.out = append(a.out, model.NoticeEvent{Text: text})
}

// sent ends AFK when you send something yourself.
func (a *Backend) sent() {
	if a.away != nil {
		a.cameBack()
		if a.notify != nil {
			a.notify() // the notice shows on the next Poll
		}
	}
}

// The ways you send a message, which end AFK.

func (a *Backend) Send(chatID string, d model.Draft) *model.Message {
	a.sent()
	return a.Backend.Send(chatID, d)
}

func (a *Backend) SendFile(chatID string, at model.Attachment, d model.Draft) *model.Message {
	a.sent()
	return a.Backend.SendFile(chatID, at, d)
}

func (a *Backend) SendSticker(chatID string, sticker, reply *model.Message) {
	a.sent()
	a.Backend.SendSticker(chatID, sticker, reply)
}

func (a *Backend) SendNewSticker(chatID string, webp []byte, reply *model.Message) *model.Message {
	a.sent()
	return a.Backend.SendNewSticker(chatID, webp, reply)
}

func (a *Backend) SendContacts(chatID string, contactIDs []string) *model.Message {
	a.sent()
	return a.Backend.SendContacts(chatID, contactIDs)
}

func (a *Backend) SendPoll(chatID string, p model.Poll) *model.Message {
	a.sent()
	return a.Backend.SendPoll(chatID, p)
}

func (a *Backend) Forward(msgs []*model.Message, chatIDs []string) {
	a.sent()
	a.Backend.Forward(msgs, chatIDs)
}
