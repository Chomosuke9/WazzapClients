package usercmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	stdtime "time"

	"github.com/dop251/goja"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// ctx.sock is the whole account: what has one of Baileys' names takes what
// Baileys' takes. It goes through model.Backend, so ghost mode, the message
// store and the window know of all it does. Logging out, the app's
// preferences (what keeps the added commands, and App lock) and the privacy
// settings aren't in it.

// sock puts sock's functions in it.
func (r *run) sock(o *goja.Object) {
	for name, f := range map[string]func(args []goja.Value) (task, error){
		"sendMessage":             r.sockSend,
		"relayMessage":            r.sockRelay,
		"getMessage":              r.sockPayload,
		"fetchMessages":           r.sockMessages,
		"chats":                   r.sockChats,
		"contacts":                r.sockContacts,
		"pickFile":                r.sockPickFile,
		"onWhatsApp":              r.sockPhones,
		"readMessages":            r.sockRead,
		"chatModify":              r.sockChatModify,
		"star":                    r.sockStar,
		"updateBlockStatus":       r.sockBlock,
		"groupMetadata":           r.sockGroup,
		"groupParticipantsUpdate": r.sockMembers,
		"groupSettingUpdate":      r.sockGroupSetting,
		"groupUpdateSubject":      r.groupText(model.GroupName),
		"groupUpdateDescription":  r.groupText(model.GroupDescription),
		"groupInviteCode":         r.inviteCode(false),
		"groupRevokeInvite":       r.inviteCode(true),
		"groupLeave":              r.sockLeave,
	} {
		_ = o.Set(name, r.async("sock."+name, f))
	}
}

var mediaNames = map[model.Media]string{model.MediaImage: "image", model.MediaVideo: "video", model.MediaGIF: "gif",
	model.MediaVoice: "ptt", model.MediaAudio: "audio", model.MediaDocument: "document", model.MediaSticker: "sticker",
	model.MediaLocation: "location", model.MediaContact: "contact", model.MediaPoll: "poll", model.MediaEventInvite: "event",
	model.MediaStickerPack: "stickerPack"}

// ref names a message.
type ref struct{ chat, id string }

// message is m for the script, on the UI goroutine. A view once message's
// text stays unopened.
func (r *run) message(m *model.Message) fields {
	r.known[ref{m.ChatID, m.ID}] = m
	text := m.Text
	if m.Kind == model.KindViewOnce {
		text = ""
	}
	// Mentions in it are marked with isolates.
	text = strings.NewReplacer("⁨", "", "⁩", "").Replace(text)
	return fields{"key", fields{"remoteJid", m.ChatID, "id", m.ID, "fromMe", m.FromMe, "participant", m.SenderID},
		"text", text, "sender", m.Sender, "senderId", m.SenderID, "fromMe", m.FromMe, "time", m.Time.Unix(),
		"media", mediaNames[m.Media], "fileName", m.FileName}
}

// file is a file the user picked, which scripts can only send: they can't
// make one of their own.
type file struct {
	Name string
	a    model.Attachment
}

// refOf reads a message, or the key of one.
func refOf(v goja.Value) (ref, error) {
	if o, ok := v.(*goja.Object); ok {
		if k, ok := o.Get("key").(*goja.Object); ok {
			o = k
		}
		chat, err1 := str(o, "key", "remoteJid")
		id, err2 := str(o, "key", "id")
		if err1 == nil && err2 == nil && chat != "" && id != "" {
			return ref{chat, id}, nil
		}
	}
	return ref{}, errors.New("give a message, or a key with remoteJid and id" + forgot(v))
}

// find is the message a key names, on the UI goroutine.
func (r *run) find(k ref) (*model.Message, error) {
	if k.chat = r.c.Backend.ChatID(k.chat); k.chat != "" {
		if ms := r.c.Backend.MessagesFrom(k.chat, k.id, 1); len(ms) > 0 && ms[0].ID == k.id {
			return ms[0], nil
		}
		if m := r.known[k]; m != nil {
			return m, nil
		}
	}
	return nil, errors.New("that message isn't stored here")
}

// chat is the chat a jid names, on the UI goroutine.
func (r *run) chat(jid string) (string, error) {
	if id := r.c.Backend.ChatID(jid); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("%q isn't a chat", jid)
}

// in is a task done in the chat jid names.
func (r *run) in(jid string, f func(chat string) (any, error)) task {
	return now(func() (any, error) {
		id, err := r.chat(jid)
		if err != nil {
			return nil, err
		}
		return f(id)
	})
}

// online fails while there's no connection, for what the backend would
// only show a notice of.
func (r *run) online() error {
	if !r.c.Online() {
		return errors.New("you're offline")
	}
	return nil
}

// visible fails for what others see while ghost mode refuses it.
func (r *run) visible() error {
	if r.c.Backend.Pref(model.PrefGhost) == "on" {
		return errors.New(auto.GhostText)
	}
	return r.online()
}

// jidArg is an argument that names a chat or someone: a jid, or a member
// or chat that has one as its id.
func jidArg(args []goja.Value, i int) (string, error) {
	v := arg(args, i)
	if o, ok := v.(*goja.Object); ok {
		if id := o.Get("id"); !none(id) {
			v = id
		}
	}
	if s, ok := v.Export().(string); ok && s != "" {
		return s, nil
	}
	return "", errors.New("give a jid" + forgot(v))
}

// group sends a change to a group and waits for the answer.
func (r *run) group(jid string, req model.GroupRequest, f func(model.GroupEvent) any) task {
	return func(answer func(any, error)) {
		id, err := r.chat(jid)
		if err != nil {
			answer(nil, err)
			return
		}
		req.ChatID = id
		r.c.Group(req, func(e model.GroupEvent) {
			switch {
			case e.Err != "":
				answer(nil, errors.New(e.Err))
			case f != nil:
				answer(f(e), nil)
			default:
				answer(nil, nil)
			}
		})
	}
}

func (r *run) groupText(action model.GroupAction) func([]goja.Value) (task, error) {
	return func(args []goja.Value) (task, error) {
		jid, err := jidArg(args, 0)
		if err != nil {
			return nil, err
		}
		text := ""
		if v := arg(args, 1); !none(v) || action == model.GroupName {
			if text, err = textArg(args, 1); err != nil {
				return nil, err
			}
		}
		return r.group(jid, model.GroupRequest{Action: action, Text: text}, nil), nil
	}
}

func (r *run) inviteCode(reset bool) func([]goja.Value) (task, error) {
	return func(args []goja.Value) (task, error) {
		jid, err := jidArg(args, 0)
		if err != nil {
			return nil, err
		}
		return r.group(jid, model.GroupRequest{Action: model.GroupLink, On: reset}, func(e model.GroupEvent) any {
			return e.Link[strings.LastIndexByte(e.Link, '/')+1:]
		}), nil
	}
}

func (r *run) sockLeave(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	return r.in(jid, func(chat string) (any, error) {
		if err := r.online(); err != nil {
			return nil, err
		}
		r.c.Backend.LeaveGroup(chat)
		return nil, nil
	}), nil
}

// contentKinds are the keys of sendMessage's content that say what it is,
// each with the others it may have.
var contentKinds = []struct {
	name string
	keys []string
}{
	{"text", []string{"mentions", "viewOnce"}},
	{"image", []string{"caption", "mentions", "viewOnce"}},
	{"video", []string{"caption", "mentions", "viewOnce", "gifPlayback"}},
	{"audio", nil},
	{"document", []string{"caption", "mentions"}},
	{"sticker", nil}, {"react", nil}, {"delete", nil},
	{"edit", []string{"text", "mentions"}},
	{"forward", nil}, {"poll", nil}, {"location", nil}, {"contacts", nil},
	{"pin", []string{"type", "time"}},
}

// sendOptions reads the options sendMessage and relayMessage take last:
// {quoted: message}, and {messageId} too when allowID (relayMessage's).
func sendOptions(v goja.Value, allowID bool) (quoted *ref, messageID string, err error) {
	if none(v) {
		return nil, "", nil
	}
	o, ok := v.(*goja.Object)
	if !ok {
		return nil, "", errors.New("the options are an object, as { quoted: message }")
	}
	keys := []string{"quoted"}
	if allowID {
		keys = append(keys, "messageId")
	}
	if err = only(o, "options", keys...); err != nil {
		return nil, "", err
	}
	if q := o.Get("quoted"); !none(q) {
		k, err := refOf(q)
		if err != nil {
			return nil, "", fmt.Errorf("quoted: %w", err)
		}
		quoted = &k
	}
	if allowID {
		if id := o.Get("messageId"); !none(id) {
			if messageID, err = messageIDOf(id); err != nil {
				return nil, "", err
			}
		}
	}
	return quoted, messageID, nil
}

// messageIDOf reads a messageId: a text ID to send a message under, as
// Baileys' relayMessage takes. It must be safe in a stanza, so no spaces
// or anything but printable ASCII.
func messageIDOf(v goja.Value) (string, error) {
	s, ok := v.Export().(string)
	if !ok {
		return "", errors.New("messageId: give the ID as text" + forgot(v))
	}
	if s == "" || len(s) > 64 {
		return "", errors.New("messageId: 1 to 64 characters")
	}
	for _, r := range s {
		if r <= ' ' || r > '~' {
			return "", errors.New("messageId: letters, digits and punctuation only")
		}
	}
	return s, nil
}

// reply finds the message to quote, on the UI goroutine.
func (r *run) reply(k *ref) (*model.Message, error) {
	if k == nil {
		return nil, nil
	}
	m, err := r.find(*k)
	if err != nil {
		return nil, fmt.Errorf("quoted: %w", err)
	}
	return m, nil
}

// sockSend is sendMessage(jid, content, {quoted}).
func (r *run) sockSend(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	c, ok := arg(args, 1).(*goja.Object)
	if !ok {
		return nil, errors.New(`give the content as an object, as { text: "..." }`)
	}
	quoted, _, err := sendOptions(arg(args, 2), false)
	if err != nil {
		return nil, err
	}
	kind, keys, names := "", []string(nil), []string(nil)
	for _, k := range contentKinds {
		names = append(names, k.name)
		if none(c.Get(k.name)) || k.name == "text" && !none(c.Get("edit")) {
			continue
		}
		if kind != "" {
			return nil, fmt.Errorf("the content is both %s and %s", kind, k.name)
		}
		kind, keys = k.name, append([]string{k.name}, k.keys...)
	}
	if kind == "" {
		return nil, fmt.Errorf("the content needs one of %s", strings.Join(names, ", "))
	}
	if err := only(c, kind, keys...); err != nil {
		return nil, err
	}
	v := c.Get(kind)
	if kind == "location" {
		// The one the backend has no method for.
		msg := r.vm.NewObject()
		_ = msg.Set("locationMessage", v)
		return r.relay(jid, msg, quoted, "")
	}
	mentions, err := stringList(c.Get("mentions"))
	if err != nil {
		return nil, fmt.Errorf("mentions: %w", err)
	}
	textKey := "caption"
	if kind == "text" || kind == "edit" {
		textKey = "text"
	}
	text, err := str(c, kind, textKey)
	if err != nil {
		return nil, err
	}
	if len(text) > 65536 {
		return nil, errors.New("the text is too long")
	}
	s := sending{kind: kind, text: text, mentions: mentions, viewOnce: flag(c, "viewOnce"), quoted: quoted}
	if err := s.read(v, c); err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	if err := r.countSend(); err != nil {
		return nil, err
	}
	return r.in(jid, func(chat string) (any, error) { return r.sendKind(chat, &s) }), nil
}

// sending is what sendMessage was given, read from the script's values.
type sending struct {
	kind, text string
	mentions   []string
	viewOnce   bool
	quoted     *ref

	file   model.Attachment // a file's
	target ref              // the message of sticker, react, delete, edit, forward and pin
	emoji  string           // react's
	pin    stdtime.Duration // 0 unpins
	poll   model.Poll
	people []string // contacts
}

// read reads the value v of s.kind in the content c.
func (s *sending) read(v goja.Value, c *goja.Object) (err error) {
	o, isObject := v.(*goja.Object)
	switch s.kind {
	case "text":
		if strings.TrimSpace(s.text) == "" {
			return errors.New("there's nothing to send")
		}
	case "image", "video", "audio", "document":
		f, ok := v.Export().(*file)
		if !ok {
			return errors.New("give a file from sock.pickFile()" + forgot(v))
		}
		s.file = f.a
		s.file.ViewOnce = s.viewOnce
		s.file.Media = map[string]model.Media{"image": model.MediaImage, "video": model.MediaVideo,
			"audio": model.MediaAudio, "document": model.MediaDocument}[s.kind]
		if flag(c, "gifPlayback") {
			s.file.Media = model.MediaGIF
		}
	case "react":
		if !isObject || none(o.Get("key")) {
			return errors.New("give text and key")
		}
		if err = only(o, "react", "text", "key"); err != nil {
			return err
		}
		if s.emoji, err = str(o, "react", "text"); err != nil {
			return err
		}
		s.target, err = refOf(o.Get("key"))
	case "edit":
		if strings.TrimSpace(s.text) == "" {
			return errors.New("give text")
		}
		s.target, err = refOf(v)
	case "pin":
		// Baileys has had type and time beside pin, and in it.
		target, from := v, c
		if isObject && !none(o.Get("key")) && (!none(o.Get("type")) || !none(o.Get("time"))) {
			if err = only(o, "pin", "key", "type", "time"); err != nil {
				return err
			}
			target, from = o.Get("key"), o
		}
		typ, err := whole(from, "pin", "type", 1)
		if err != nil || typ != 1 && typ != 2 {
			return errors.New("type is 1 to pin and 2 to unpin")
		}
		secs, err := whole(from, "pin", "time", 7*24*60*60)
		if err != nil || secs < 0 {
			return errors.New("time is in seconds, 0 to unpin")
		}
		if typ == 1 {
			s.pin = stdtime.Duration(secs) * stdtime.Second
		}
		s.target, err = refOf(target)
		return err
	case "sticker", "delete", "forward":
		s.target, err = refOf(v)
	case "poll":
		if !isObject {
			return errors.New("give name and values")
		}
		if err = only(o, "poll", "name", "values", "selectableCount"); err != nil {
			return err
		}
		if s.poll.Question, err = str(o, "poll", "name"); err != nil {
			return err
		}
		s.poll.Options, err = stringList(o.Get("values"))
		if err != nil || len(s.poll.Options) < 2 || len(s.poll.Options) > 12 {
			return errors.New("give name and 2 to 12 values")
		}
		n, err := whole(o, "poll", "selectableCount", 0)
		s.poll.Multiple = n != 1
		return err
	case "contacts":
		s.people, err = stringList(v)
	}
	return err
}

// sendKind sends s to a chat, on the UI goroutine.
func (r *run) sendKind(chat string, s *sending) (any, error) {
	b := r.c.Backend
	d := r.draft(s.text, s.mentions)
	d.ViewOnce = s.viewOnce && s.kind == "text"
	var err error
	if d.Reply, err = r.reply(s.quoted); err != nil {
		return nil, err
	}
	sent := func(m *model.Message) (any, error) {
		if m == nil {
			return nil, errors.New("the message wasn't sent")
		}
		r.c.Sent(m)
		return r.message(m), nil
	}
	switch s.kind {
	case "text":
		return sent(b.Send(chat, d))
	case "image", "video", "audio", "document":
		return sent(b.SendFile(chat, s.file, d))
	case "poll":
		return sent(b.SendPoll(chat, s.poll))
	case "contacts":
		for i, id := range s.people {
			if s.people[i], err = r.chat(id); err != nil {
				return nil, err
			}
		}
		return sent(b.SendContacts(chat, s.people))
	}
	// The rest is done to a message.
	if s.kind == "delete" {
		return r.delete(s.target)
	}
	m, err := r.find(s.target)
	if err != nil {
		return nil, err
	}
	if err := r.visible(); err != nil {
		return nil, err
	}
	switch s.kind {
	case "sticker":
		if m.Media != model.MediaSticker {
			return nil, errors.New("sticker: give a sticker's message")
		}
		return r.newest(chat, func() { b.SendSticker(chat, m, d.Reply) })
	case "forward":
		return r.newest(chat, func() { b.Forward([]*model.Message{m}, []string{chat}) })
	case "react":
		b.React(m, s.emoji)
	case "edit":
		if !m.CanEdit(r.c.Now) {
			return nil, errors.New("edit: only a recent message of yours can be edited")
		}
		b.Edit(m, d)
	case "pin":
		b.PinMessage(m, s.pin)
	}
	return r.message(m), nil
}

// delete takes a message back for everyone, or, when the key names a group
// status (kept with the statuses, not among chat messages) rather than a
// stored message, revokes that, on the UI goroutine.
func (r *run) delete(k ref) (any, error) {
	if err := r.visible(); err != nil {
		return nil, err
	}
	if chat := r.c.Backend.ChatID(k.chat); chat != "" {
		// A stored chat message is revoked as one. A group status isn't
		// among them (it lives with the statuses), and its key often comes
		// from relayMessage, so it would be cached as a known message and
		// never reach DeleteGroupStatus; try that before the known cache.
		if ms := r.c.Backend.MessagesFrom(chat, k.id, 1); len(ms) > 0 && ms[0].ID == k.id {
			r.c.Backend.Delete(ms[0], true)
			return r.message(ms[0]), nil
		}
		if derr := r.c.Backend.DeleteGroupStatus(chat, k.id); derr == nil {
			return fields{"key", fields{"remoteJid", chat, "id", k.id, "fromMe", true}}, nil
		}
	}
	m, err := r.find(k)
	if err != nil {
		return nil, err
	}
	r.c.Backend.Delete(m, true)
	return r.message(m), nil
}

// newest is the message of yours that send adds to a chat, for what sends
// without saying which, on the UI goroutine.
func (r *run) newest(chat string, send func()) (any, error) {
	const look = 10 // someone else's may come after it
	before := map[string]bool{}
	for _, m := range r.c.Backend.Messages(chat, look) {
		before[m.ID] = true
	}
	send()
	for _, m := range slices.Backward(r.c.Backend.Messages(chat, look)) {
		if m.FromMe && !before[m.ID] {
			return r.message(m), nil
		}
	}
	return nil, errors.New("the message wasn't sent")
}

// draft is text with the mentions given and those of members the script
// knows, on the UI goroutine.
func (r *run) draft(text string, mentions []string) model.Draft {
	d := model.Draft{Text: text}
	for _, id := range mentions {
		if !slices.Contains(d.Mentions, id) {
			d.Mentions = append(d.Mentions, id)
		}
	}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		if id := r.people[m[1]]; id != "" && !slices.Contains(d.Mentions, id) {
			d.Mentions = append(d.Mentions, id)
		}
	}
	return d
}

// stringList reads a list of texts, or of what has an id, such as members.
func stringList(v goja.Value) ([]string, error) {
	vs, err := items(v)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, x := range vs {
		if o, ok := x.(*goja.Object); ok && !none(o.Get("id")) {
			x = o.Get("id")
		}
		s, ok := x.Export().(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("%s can't be in the list%s", x, forgot(x))
		}
		out = append(out, s)
	}
	return out, nil
}

// sockRelay is relayMessage(jid, message, {quoted}): any message.
func (r *run) sockRelay(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	msg, ok := arg(args, 1).(*goja.Object)
	if !ok {
		return nil, errors.New("give the message as an object")
	}
	quoted, messageID, err := sendOptions(arg(args, 2), true)
	if err != nil {
		return nil, err
	}
	return r.relay(jid, msg, quoted, messageID)
}

func (r *run) relay(jid string, msg *goja.Object, quoted *ref, messageID string) (task, error) {
	v, err := new(plain).value(msg.Export(), 0)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := r.countSend(); err != nil {
		return nil, err
	}
	return r.in(jid, func(chat string) (any, error) {
		reply, err := r.reply(quoted)
		if err != nil {
			return nil, err
		}
		m, shown, err := r.c.Backend.SendPayload(chat, string(body), messageID, reply)
		if err != nil {
			return nil, err
		}
		if shown {
			r.c.Sent(m)
		}
		return r.message(m), nil
	}), nil
}

// keyArg is an argument that names a message.
func (r *run) keyArg(args []goja.Value, f func(m *model.Message) (any, error)) (task, error) {
	k, err := refOf(arg(args, 0))
	if err != nil {
		return nil, err
	}
	return now(func() (any, error) {
		m, err := r.find(k)
		if err != nil {
			return nil, err
		}
		return f(m)
	}), nil
}

// sockPayload is getMessage(message): its protobuf as an object.
func (r *run) sockPayload(args []goja.Value) (task, error) {
	return r.keyArg(args, func(m *model.Message) (any, error) {
		s, err := r.c.Backend.MessagePayload(m.ChatID, m.ID)
		return jsonText(s), err
	})
}

func (r *run) sockMessages(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	limit := int64(20)
	if v := arg(args, 1); !none(v) {
		limit = v.ToInteger()
	}
	if limit < 1 || limit > 500 {
		return nil, errors.New("limit is 1 to 500")
	}
	return r.in(jid, func(chat string) (any, error) {
		vs := []any{}
		for _, m := range r.c.Backend.Messages(chat, int(limit)) {
			vs = append(vs, r.message(m))
		}
		return vs, nil
	}), nil
}

func (r *run) sockChats([]goja.Value) (task, error) {
	return now(func() (any, error) {
		vs := []any{}
		for _, c := range r.c.Backend.Chats() {
			vs = append(vs, fields{"id", c.ID, "name", c.Name, "group", c.IsGroup, "unread", c.Unread,
				"archived", c.Archived, "pinned", c.Pinned, "muted", c.Muted})
		}
		return vs, nil
	}), nil
}

func (r *run) sockContacts([]goja.Value) (task, error) {
	return now(func() (any, error) {
		vs := []any{}
		for _, c := range r.c.Backend.Contacts() {
			vs = append(vs, fields{"id", c.ID, "name", c.Name, "phone", c.Phone})
		}
		return vs, nil
	}), nil
}

// sockPickFile is pickFile(title): the user chooses, so a script can't
// read a file of its own choice.
func (r *run) sockPickFile(args []goja.Value) (task, error) {
	title := "Choose a file"
	if !none(arg(args, 0)) {
		var err error
		if title, err = textArg(args, 0); err != nil {
			return nil, err
		}
	}
	return func(answer func(any, error)) {
		r.c.PickFile("/"+r.cmd.Cmd.Name+": "+oneLine(title), func(a model.Attachment) {
			if a.Path == "" {
				answer(nil, nil)
				return
			}
			answer(&file{Name: a.Path[strings.LastIndexAny(a.Path, `/\`)+1:], a: a}, nil)
		})
	}, nil
}

// sockPhones is onWhatsApp(...phones): what WhatsApp has of each, asked
// one after the other.
func (r *run) sockPhones(args []goja.Value) (task, error) {
	var phones []string
	for i := range args {
		s, err := textArg(args, i)
		if err != nil {
			return nil, err
		}
		s, _, _ = strings.Cut(s, "@")
		s = strings.Map(func(ch rune) rune {
			if ch < '0' || ch > '9' {
				return -1
			}
			return ch
		}, s)
		if len(s) < 7 || len(s) > 15 {
			return nil, fmt.Errorf("%s: give a phone number with its country code", args[i])
		}
		phones = append(phones, s)
	}
	if len(phones) > 50 {
		return nil, errors.New("over 50 numbers at once")
	}
	return func(answer func(any, error)) {
		found := []any{}
		var next func()
		next = func() {
			if len(found) == len(phones) {
				answer(found, nil)
				return
			}
			if r.ctx.Err() != nil { // stopped meanwhile
				return
			}
			r.c.LookupPhone(phones[len(found)], func(e model.PhoneEvent) {
				if e.Err != "" {
					answer(nil, errors.New(e.Err))
					return
				}
				f := fields{"jid", nil, "exists", false, "name", ""}
				if e.ID != "" {
					f = fields{"jid", e.ID, "exists", true, "name", e.Name}
				}
				found = append(found, f)
				next()
			})
		}
		next()
	}, nil
}

// sockRead is readMessages(keys): it reads the whole chats they're in, as
// the app does.
func (r *run) sockRead(args []goja.Value) (task, error) {
	vs, err := items(arg(args, 0))
	if err != nil {
		return nil, err
	}
	var jids []string
	for _, v := range vs {
		if k, err := refOf(v); err == nil {
			jids = append(jids, k.chat)
			continue
		}
		jid, err := jidArg([]goja.Value{v}, 0)
		if err != nil {
			return nil, errors.New("give a list of keys, or of jids")
		}
		jids = append(jids, jid)
	}
	return now(func() (any, error) {
		var chats []string
		for _, jid := range jids {
			id, err := r.chat(jid)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(chats, id) {
				chats = append(chats, id)
			}
		}
		r.c.Backend.MarkRead(chats)
		return nil, nil
	}), nil
}

// sockChatModify is chatModify(changes, jid): archive, pin, markRead, mute
// (milliseconds, or null to unmute) and star. Baileys' lastMessages isn't
// needed.
func (r *run) sockChatModify(args []goja.Value) (task, error) {
	c, ok := arg(args, 0).(*goja.Object)
	if !ok {
		return nil, errors.New("give the changes as an object, as { archive: true }")
	}
	jid, err := jidArg(args, 1)
	if err != nil {
		return nil, err
	}
	if err := only(c, "chatModify", "archive", "pin", "markRead", "mute", "star", "lastMessages"); err != nil {
		return nil, err
	}
	b := r.c.Backend
	var todo []func(chat string) error
	add := func(f func(chat string)) {
		todo = append(todo, func(chat string) error { f(chat); return nil })
	}
	for _, k := range c.Keys() {
		v := c.Get(k)
		on := !none(v) && v.ToBoolean()
		switch k {
		case "archive":
			add(func(chat string) { b.SetArchived(chat, on) })
		case "pin":
			add(func(chat string) { b.SetPinned(chat, on) })
		case "markRead":
			add(func(chat string) {
				if on {
					b.MarkRead([]string{chat})
				} else {
					b.SetUnread(chat, true)
				}
			})
		case "mute":
			ms := int64(0)
			if !none(v) {
				if ms, ok = v.Export().(int64); !ok || ms < 0 || ms > 1e12 {
					return nil, errors.New("mute is in milliseconds, or null")
				}
			}
			add(func(chat string) { b.SetMuted(chat, ms > 0, stdtime.Duration(ms)*stdtime.Millisecond) })
		case "star":
			o, ok := v.(*goja.Object)
			if !ok {
				return nil, errors.New("star: give messages and star")
			}
			ids, err := stringList(o.Get("messages"))
			if err != nil {
				return nil, fmt.Errorf("star: %w", err)
			}
			on := flag(o, "star")
			todo = append(todo, func(chat string) error { return r.star(chat, ids, on) })
		}
	}
	return r.in(jid, func(chat string) (any, error) {
		if err := r.online(); err != nil {
			return nil, err
		}
		for _, f := range todo {
			if err := f(chat); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}), nil
}

// star stars messages of a chat, on the UI goroutine.
func (r *run) star(chat string, ids []string, on bool) error {
	if err := r.online(); err != nil {
		return err
	}
	for _, id := range ids {
		m, err := r.find(ref{chat, id})
		if err != nil {
			return err
		}
		r.c.Backend.Star(m, on)
	}
	return nil
}

// sockStar is star(jid, messages, star): messages are keys.
func (r *run) sockStar(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	vs, err := items(arg(args, 1))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, v := range vs {
		if k, err := refOf(v); err == nil {
			ids = append(ids, k.id)
		} else if id, err := jidArg([]goja.Value{v}, 0); err == nil {
			ids = append(ids, id)
		} else {
			return nil, errors.New("give a list of messages")
		}
	}
	on := arg(args, 2).ToBoolean()
	return r.in(jid, func(chat string) (any, error) { return nil, r.star(chat, ids, on) }), nil
}

func (r *run) sockBlock(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	action := arg(args, 1).String()
	if action != "block" && action != "unblock" {
		return nil, errors.New(`action is "block" or "unblock"`)
	}
	return r.in(jid, func(chat string) (any, error) {
		if err := r.online(); err != nil {
			return nil, err
		}
		r.c.Backend.SetBlocked(chat, action == "block")
		return nil, nil
	}), nil
}

// sockGroup is groupMetadata(jid). Details that aren't here yet come in
// the background, so it waits for them a while.
func (r *run) sockGroup(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	return func(answer func(any, error)) {
		chat, err := r.chat(jid)
		if err != nil {
			answer(nil, err)
			return
		}
		tries := 0
		var try func()
		try = func() {
			info := r.c.Backend.Info(chat)
			switch tries++; {
			case info == nil && tries < 20:
				stdtime.AfterFunc(500*stdtime.Millisecond, func() { r.ask(try) })
			case info == nil:
				answer(nil, errors.New("the group's details didn't load"))
			case !info.IsGroup:
				answer(nil, fmt.Errorf("%q isn't a group", jid))
			default:
				answer(fields{"id", chat, "subject", info.Name, "desc", info.About, "announce", info.Announce,
					"restrict", info.Locked, "creation", info.Created.Unix(), "size", len(info.Members),
					"participants", r.members(info.Members)}, nil)
			}
		}
		try()
	}, nil
}

func (r *run) sockMembers(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	a, ok := map[string]model.GroupAction{"add": model.GroupAdd, "remove": model.GroupRemove,
		"promote": model.GroupPromote, "demote": model.GroupDemote}[arg(args, 2).String()]
	if !ok {
		return nil, errors.New(`action is "add", "remove", "promote" or "demote"`)
	}
	ids, err := stringList(arg(args, 1))
	if err != nil {
		return nil, fmt.Errorf("participants: %w", err)
	}
	if len(ids) == 0 {
		return nil, errors.New("give a list of participants")
	}
	return r.group(jid, model.GroupRequest{Action: a, Members: ids}, func(e model.GroupEvent) any {
		vs := []any{}
		for _, m := range e.Members {
			status := "200"
			if m.Err != "" {
				status = "error"
			}
			vs = append(vs, fields{"jid", m.ID, "name", m.Name, "status", status, "error", m.Err})
		}
		return vs
	}), nil
}

func (r *run) sockGroupSetting(args []goja.Value) (task, error) {
	jid, err := jidArg(args, 0)
	if err != nil {
		return nil, err
	}
	req, ok := map[string]model.GroupRequest{
		"announcement": {Action: model.GroupAnnounce, On: true}, "not_announcement": {Action: model.GroupAnnounce},
		"locked": {Action: model.GroupLock, On: true}, "unlocked": {Action: model.GroupLock},
	}[arg(args, 1).String()]
	if !ok {
		return nil, errors.New(`setting is "announcement", "not_announcement", "locked" or "unlocked"`)
	}
	return r.group(jid, req, nil), nil
}

// maxDepth and maxSize bound a message's objects and lists: how deeply
// they nest, and how much they hold, as one can be in them many times.
const (
	maxDepth = 64
	maxSize  = model.MaxSnippetBytes
)

// plain copies an exported value as JSON can hold it. Bytes (a Uint8Array)
// become base64, as protobuf's JSON has them.
type plain struct{ size int }

func (t *plain) value(v any, depth int) (any, error) {
	t.size++
	switch v := v.(type) {
	case string:
		t.size += len(v)
	case []byte:
		t.size += len(v)
	}
	if depth > maxDepth || t.size > maxSize {
		return nil, errors.New("the message is too big")
	}
	switch v := v.(type) {
	case nil, bool, string, int64, float64, []byte, *big.Int:
		return v, nil
	case goja.ArrayBuffer:
		return t.value(v.Bytes(), depth)
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			g, err := t.value(x, depth+1)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			t.size += len(k)
			m[k] = g
		}
		return m, nil
	case []any:
		l := make([]any, len(v))
		for i, x := range v {
			g, err := t.value(x, depth+1)
			if err != nil {
				return nil, err
			}
			l[i] = g
		}
		return l, nil
	case *goja.Promise:
		return nil, errors.New("a message can't hold a promise (put await before it)")
	}
	return nil, errors.New("a message can't hold that")
}
