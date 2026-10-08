package wa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// System messages ("Alice added Bob", "You turned on disappearing
// messages", a missed call) are stored like messages, as kind
// model.KindSystem. Their payload is the stub WhatsApp's history sync
// sends them as: a waWeb.WebMessageInfo holding only MessageStubType and
// MessageStubParameters. Changes that arrive live (group notifications,
// pins, timers, security codes) are turned into the same stubs, so
// systemText words them all, when they're read, with the names known
// then. Who made the change is the message's sender.

type stubType = waWeb.WebMessageInfo_StubType

// shownStubs are the stubs the chat shows; the others aren't stored.
var shownStubs = map[stubType]bool{
	waWeb.WebMessageInfo_GROUP_CREATE:                        true,
	waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT:                true,
	waWeb.WebMessageInfo_GROUP_CHANGE_ICON:                   true,
	waWeb.WebMessageInfo_GROUP_CHANGE_INVITE_LINK:            true,
	waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION:            true,
	waWeb.WebMessageInfo_GROUP_CHANGE_RESTRICT:               true,
	waWeb.WebMessageInfo_GROUP_CHANGE_ANNOUNCE:               true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD:               true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE:            true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE:           true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE:            true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE:            true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE:             true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_CHANGE_NUMBER:     true,
	waWeb.WebMessageInfo_INDIVIDUAL_CHANGE_NUMBER:            true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_ACCEPT:            true,
	waWeb.WebMessageInfo_GROUP_PARTICIPANT_LINKED_GROUP_JOIN: true,
	waWeb.WebMessageInfo_GROUP_MEMBERSHIP_JOIN_APPROVAL_MODE: true,
	waWeb.WebMessageInfo_GROUP_MEMBER_ADD_MODE:               true,
	waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING:            true,
	waWeb.WebMessageInfo_E2E_IDENTITY_CHANGED:                true,
	waWeb.WebMessageInfo_CALL_MISSED_VOICE:                   true,
	waWeb.WebMessageInfo_CALL_MISSED_VIDEO:                   true,
	waWeb.WebMessageInfo_CALL_MISSED_GROUP_VOICE:             true,
	waWeb.WebMessageInfo_CALL_MISSED_GROUP_VIDEO:             true,
	waWeb.WebMessageInfo_PINNED_MESSAGE_IN_CHAT:              true,
}

// systemMsg makes a system message of chat: stub t with params, made by
// actor (empty when it isn't known) at at.
func systemMsg(chat, id string, at time.Time, actor types.JID, fromMe bool, t stubType, params ...string) storedMsg {
	w := &waWeb.WebMessageInfo{MessageStubType: t.Enum(), MessageStubParameters: params}
	m := storedMsg{
		Message: &model.Message{ID: id, ChatID: chat, Kind: model.KindSystem, FromMe: fromMe, Time: at,
			Receipt: model.Sent},
		rawPayload: stubPayload(w),
	}
	if !actor.IsEmpty() {
		m.senderJID = actor.ToNonAD().String()
	}
	return m
}

// systemID names a system message made from a live change, which comes
// without an ID of its own. The same change coming again gets the same ID.
func systemID(chat string, at time.Time, actor types.JID, t stubType, params []string) string {
	h := sha256.New()
	h.Write([]byte(chat + "\x00" + strconv.FormatInt(at.Unix(), 10) + "\x00" + actor.ToNonAD().String() +
		"\x00" + strconv.Itoa(int(t)) + "\x00" + strings.Join(params, "\x00")))
	return "SYS" + strings.ToUpper(hex.EncodeToString(h.Sum(nil))[:16])
}

// stubPayload marshals a stub. WebMessageInfo's key is a required field,
// which a stub stored on its own leaves out.
func stubPayload(w *waWeb.WebMessageInfo) []byte {
	b, _ := proto.MarshalOptions{AllowPartial: true}.Marshal(w)
	return b
}

// readStub returns the stub a system message's payload holds.
func readStub(raw []byte) (stubType, []string, bool) {
	var w waWeb.WebMessageInfo
	if len(raw) == 0 || (proto.UnmarshalOptions{AllowPartial: true}).Unmarshal(raw, &w) != nil || w.MessageStubType == nil {
		return 0, nil, false
	}
	return w.GetMessageStubType(), w.GetMessageStubParameters(), true
}

// historyStub turns a history sync stub of chat into a system message,
// or reports false for one the chat doesn't show. It may query the device
// store: never call it inside a msgStore transaction.
func (b *Backend) historyStub(ctx context.Context, chat types.JID, w *waWeb.WebMessageInfo) (storedMsg, bool) {
	t := w.GetMessageStubType()
	if !shownStubs[t] || w.GetKey().GetID() == "" {
		return storedMsg{}, false
	}
	fromMe := w.GetKey().GetFromMe()
	actor := types.EmptyJID
	if p := first(w.GetParticipant(), w.GetKey().GetParticipant()); p != "" {
		actor, _ = types.ParseJID(p)
	} else if chat.Server != types.GroupServer && !fromMe {
		actor = chat
	}
	if !actor.IsEmpty() {
		actor = b.canonical(ctx, actor)
		fromMe = fromMe || b.isMe(actor)
	}
	params := w.GetMessageStubParameters()
	if !b.stubShown(t, params) {
		return storedMsg{}, false
	}
	at := time.Unix(int64(w.GetMessageTimestamp()), 0)
	return systemMsg(chat.String(), w.GetKey().GetID(), at, actor, fromMe, t, params...), true
}

// stubShown reports whether stub t with params shows in the chat. A
// member made an admin, or no longer one, shows only when it's you.
func (b *Backend) stubShown(t stubType, params []string) bool {
	switch t {
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE, waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE:
		for _, p := range params {
			if j, err := types.ParseJID(p); err == nil && b.isMe(j) {
				return true
			}
		}
		return false
	}
	return shownStubs[t]
}

// putSystem stores a system message made from a live change and shows
// it. It never counts as unread or notifies.
func (b *Backend) putSystem(ctx context.Context, m storedMsg) {
	t, params, _ := readStub(m.rawPayload)
	if !b.stubShown(t, params) {
		return
	}
	chat := m.ChatID
	j, _ := types.ParseJID(chat)
	isGroup := j.Server == types.GroupServer
	if err := b.store.ensureChat(ctx, b.db, chat, isGroup, ""); err != nil {
		b.log.Warnf("store chat %s: %v", chat, err)
		return
	}
	if err := b.store.putMessage(ctx, b.db, m); err != nil {
		b.log.Warnf("store system message in %s: %v", chat, err)
		return
	}
	if r, ok := b.store.message(ctx, chat, m.ID); ok {
		b.emit(model.MessageEvent{Msg: b.resolve(ctx, r, isGroup)})
	}
	b.emitChat(chat)
}

// liveSystem stores a live change of chat as stub t with params.
func (b *Backend) liveSystem(ctx context.Context, chat string, at time.Time, actor types.JID, t stubType, params ...string) {
	if at.IsZero() {
		at = b.now()
	}
	if !actor.IsEmpty() {
		actor = b.canonical(ctx, actor)
	}
	fromMe := !actor.IsEmpty() && b.isMe(actor)
	m := systemMsg(chat, systemID(chat, at, actor, t, params), at, actor, fromMe, t, params...)
	// Being added to a group can come both as the group's notification and
	// as the join (onJoinedSystem), timed apart: show it once.
	var dup int
	_ = b.db.QueryRowContext(ctx, `SELECT 1 FROM wz_messages WHERE chat = ? AND kind = ? AND sender_jid = ?
		AND raw_payload = ? AND ts BETWEEN ? AND ? AND id != ? LIMIT 1`, chat, int(model.KindSystem), m.senderJID,
		m.rawPayload, at.Unix()-120, at.Unix()+120, m.ID).Scan(&dup)
	if dup == 0 {
		b.putSystem(ctx, m)
	}
}

// onGroupSystem records a group's changes as system messages.
func (b *Backend) onGroupSystem(ctx context.Context, e *events.GroupInfo) {
	chat := e.JID.String()
	actor := types.EmptyJID
	if e.Sender != nil {
		actor = *e.Sender
	}
	at := e.Timestamp
	put := func(t stubType, params ...string) { b.liveSystem(ctx, chat, at, actor, t, params...) }
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	if e.Name != nil {
		put(waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT, e.Name.Name)
	}
	if e.Topic != nil {
		put(waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION)
	}
	if e.Locked != nil {
		put(waWeb.WebMessageInfo_GROUP_CHANGE_RESTRICT, onOff(e.Locked.IsLocked))
	}
	if e.Announce != nil {
		put(waWeb.WebMessageInfo_GROUP_CHANGE_ANNOUNCE, onOff(e.Announce.IsAnnounce))
	}
	if e.Ephemeral != nil {
		secs := uint32(0)
		if e.Ephemeral.IsEphemeral {
			secs = e.Ephemeral.DisappearingTimer
		}
		put(waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING, strconv.Itoa(int(secs)))
	}
	if e.MembershipApprovalMode != nil {
		put(waWeb.WebMessageInfo_GROUP_MEMBERSHIP_JOIN_APPROVAL_MODE, onOff(e.MembershipApprovalMode.IsJoinApprovalRequired))
	}
	if e.NewInviteLink != nil {
		put(waWeb.WebMessageInfo_GROUP_CHANGE_INVITE_LINK)
	}
	jids := func(js []types.JID) []string {
		out := make([]string, len(js))
		for i, j := range js {
			if b.isMe(j) {
				out[i] = b.ownJID(chat).String() // as onJoinedSystem names you
			} else {
				out[i] = b.canonical(ctx, j).String()
			}
		}
		return out
	}
	if len(e.Join) > 0 {
		t := waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD
		if e.JoinReason == "invite" {
			t = waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE
		}
		put(t, jids(e.Join)...)
	}
	if len(e.Leave) > 0 {
		t := waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE
		if actor.IsEmpty() || len(e.Leave) == 1 && e.Leave[0].User == actor.User {
			t = waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE
		}
		put(t, jids(e.Leave)...)
	}
	if len(e.Promote) > 0 {
		put(waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE, jids(e.Promote)...)
	}
	if len(e.Demote) > 0 {
		put(waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE, jids(e.Demote)...)
	}
}

// onJoinedSystem records how you came to be in a group you just joined.
func (b *Backend) onJoinedSystem(ctx context.Context, e *events.JoinedGroup) {
	chat := e.JID.String()
	me := b.ownJID(chat)
	if me.IsEmpty() {
		return
	}
	actor := types.EmptyJID
	if e.Sender != nil {
		actor = *e.Sender
	}
	at := b.now()
	switch {
	case e.Type == "new":
		b.liveSystem(ctx, chat, at, actor, waWeb.WebMessageInfo_GROUP_CREATE, e.Name)
		if !actor.IsEmpty() && !b.isMe(actor) {
			b.liveSystem(ctx, chat, at, actor, waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, me.String())
		}
	case e.Reason == "invite":
		b.liveSystem(ctx, chat, at, me, waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE, me.String())
	default:
		b.liveSystem(ctx, chat, at, actor, waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, me.String())
	}
}

// onPictureSystem records a group's icon changing.
func (b *Backend) onPictureSystem(ctx context.Context, e *events.Picture) {
	if e.JID.Server != types.GroupServer {
		return
	}
	var params []string
	if e.Remove {
		params = []string{"remove"}
	}
	b.liveSystem(ctx, e.JID.String(), e.Timestamp, e.Author, waWeb.WebMessageInfo_GROUP_CHANGE_ICON, params...)
}

// onIdentitySystem records a contact's security code changing, in your
// chat with them when there is one.
func (b *Backend) onIdentitySystem(ctx context.Context, e *events.IdentityChange) {
	j := b.canonical(ctx, e.JID)
	if b.isMe(j) || !b.store.listed(ctx, j.String()) {
		return
	}
	b.liveSystem(ctx, j.String(), e.Timestamp, j, waWeb.WebMessageInfo_E2E_IDENTITY_CHANGED, j.String())
}

// timerSystem turns a one-to-one chat's disappearing messages setting,
// which comes as a message, into its system message. Groups say so in
// their own notification (onGroupSystem).
func timerSystem(evt *events.Message, chat types.JID, pm *waE2E.ProtocolMessage) (storedMsg, bool) {
	if evt.Info.IsGroup || evt.Info.ID == "" {
		return storedMsg{}, false
	}
	actor := chat
	if evt.Info.IsFromMe {
		actor = types.EmptyJID
	}
	m := systemMsg(chat.String(), evt.Info.ID, evt.Info.Timestamp, actor, evt.Info.IsFromMe,
		waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING, strconv.Itoa(int(pm.GetEphemeralExpiration())))
	return m, true
}

// systemText words a system message, and says what it is about.
func (b *Backend) systemText(ctx context.Context, r rawMsg, t stubType, params []string) (string, model.Notice) {
	m := r.Message
	// name is a person as the subject of a sentence; "You" for you.
	name := func(s string) string {
		j, err := types.ParseJID(s)
		if err != nil || j.IsEmpty() {
			return ""
		}
		if b.isMe(j) {
			return "You"
		}
		return b.memberName(ctx, b.canonical(ctx, j), types.EmptyJID)
	}
	actor := ""
	if m.FromMe {
		actor = "You"
	} else if r.senderJID != "" {
		actor = name(r.senderJID)
	}
	// object is a person after a verb: "you" for you.
	object := func(s string) string {
		if n := name(s); n != "You" {
			return n
		}
		return "you"
	}
	people := func(f func(string) string) string {
		var names []string
		for _, p := range params {
			if n := f(p); n != "" {
				names = append(names, n)
			}
		}
		return listNames(names)
	}
	param := func(i int) string {
		if i < len(params) {
			return params[i]
		}
		return ""
	}
	// by words a change with whoever made it, or without when that isn't known.
	by := func(active, passive string) string {
		if actor == "" {
			return passive
		}
		return actor + " " + active
	}
	settings := func(on bool, admins, all string) string {
		what := all
		if on {
			what = admins
		}
		return by("changed this group's settings to "+what, "This group's settings changed to "+what)
	}
	isOn := param(0) == "on" || param(0) == "true"
	switch t {
	case waWeb.WebMessageInfo_GROUP_CREATE:
		return by(`created group "`+param(0)+`"`, `Group "`+param(0)+`" was created`), 0
	case waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT:
		return by(`changed the group name to "`+param(0)+`"`, `The group name changed to "`+param(0)+`"`), 0
	case waWeb.WebMessageInfo_GROUP_CHANGE_ICON:
		if param(0) == "remove" {
			return by("deleted this group's icon", "This group's icon was deleted"), 0
		}
		return by("changed this group's icon", "This group's icon changed"), 0
	case waWeb.WebMessageInfo_GROUP_CHANGE_INVITE_LINK:
		return by("reset this group's invite link", "This group's invite link was reset"), 0
	case waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION:
		return by("changed the group description", "The group description changed"), 0
	case waWeb.WebMessageInfo_GROUP_CHANGE_RESTRICT:
		return settings(isOn, "allow only admins to edit this group's info", "allow all members to edit this group's info"), 0
	case waWeb.WebMessageInfo_GROUP_CHANGE_ANNOUNCE:
		return settings(isOn, "allow only admins to send messages to this group", "allow all members to send messages to this group"), 0
	case waWeb.WebMessageInfo_GROUP_MEMBER_ADD_MODE:
		return settings(param(0) == "admin_add", "allow only admins to add others to this group", "allow all members to add others to this group"), 0
	case waWeb.WebMessageInfo_GROUP_MEMBERSHIP_JOIN_APPROVAL_MODE:
		if isOn {
			return by("turned on admin approval to join this group", "Admin approval to join this group was turned on"), 0
		}
		return by("turned off admin approval to join this group", "Admin approval to join this group was turned off"), 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD:
		if actor == "" || len(params) == 1 && name(params[0]) == actor {
			return people(name) + " joined", 0
		}
		return actor + " added " + people(object), 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE:
		if actor == "" {
			return people(name) + " left", 0
		}
		return actor + " removed " + people(object), 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE:
		return people(name) + " left", 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE:
		return people(name) + " joined using this group's invite link", 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_ACCEPT:
		return people(name) + " joined", 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_LINKED_GROUP_JOIN:
		return people(name) + " joined from the community", 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE:
		return "You're now an admin", 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE:
		return "You're no longer an admin", 0
	case waWeb.WebMessageInfo_GROUP_PARTICIPANT_CHANGE_NUMBER, waWeb.WebMessageInfo_INDIVIDUAL_CHANGE_NUMBER:
		who := first(actor, name(param(0)))
		if who == "" || who == "You" {
			return "", 0
		}
		return who + " changed their phone number to a new number", 0
	case waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING:
		secs, _ := strconv.Atoi(param(0))
		if secs <= 0 {
			return by("turned off disappearing messages.", "Disappearing messages were turned off."), model.NoticeTimer
		}
		return by("turned on disappearing messages.", "Disappearing messages were turned on.") +
			" All new messages will disappear from this chat " + timerText(secs) + " after they're sent.", model.NoticeTimer
	case waWeb.WebMessageInfo_E2E_IDENTITY_CHANGED:
		who := name(param(0))
		if who == "" || who == "You" {
			who = actor
		}
		if who == "" || who == "You" {
			if j, err := types.ParseJID(m.ChatID); err == nil && j.Server != types.GroupServer {
				who = b.chatName(ctx, j)
			}
		}
		if who == "" {
			return "", 0
		}
		return "Your security code with " + who + " changed. Click to learn more.", model.NoticeSecurity
	case waWeb.WebMessageInfo_CALL_MISSED_VOICE:
		return "Missed voice call", model.NoticeMissedCall
	case waWeb.WebMessageInfo_CALL_MISSED_VIDEO:
		return "Missed video call", model.NoticeMissedCall
	case waWeb.WebMessageInfo_CALL_MISSED_GROUP_VOICE:
		return "Missed group voice call", model.NoticeMissedCall
	case waWeb.WebMessageInfo_CALL_MISSED_GROUP_VIDEO:
		return "Missed group video call", model.NoticeMissedCall
	case waWeb.WebMessageInfo_PINNED_MESSAGE_IN_CHAT:
		return by("pinned a message", "A message was pinned"), 0
	}
	return "", 0
}

// listNames joins names as a sentence does: "A", "A and B", "A, B and C".
func listNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// timerText words a disappearing messages timer: "24 hours", "7 days".
func timerText(secs int) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(n) + " " + unit + "s"
	}
	switch {
	case secs%86400 == 0 && secs > 86400: // a day is "24 hours"
		return plural(secs/86400, "day")
	case secs%3600 == 0:
		return plural(secs/3600, "hour")
	case secs%60 == 0:
		return plural(secs/60, "minute")
	}
	return plural(secs, "second")
}
