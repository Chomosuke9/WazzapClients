package wa

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Status privacy and hidden statuses live in app state (regular_high), as
// on WhatsApp's own linked devices. One status_privacy action holds who
// sees your status updates: its mode, the people of that mode's list, and
// your own audiences (custom lists), the chosen one marked. A
// userStatusMute action per contact hides their status updates.
//
// The action names only the chosen mode's people, so the lists of the
// others are kept here from the actions that named them, as WhatsApp Web
// keeps them: the phone shows "8 excluded" while another audience is on.

// wz_meta keys.
const (
	metaStatusPrivacy = "status_privacy" // the latest StatusPrivacyAction, base64
	metaStatusList    = "status_list:"   // + a mode's name: its people, as the phone named them
	metaStatusMute    = "status_mute:"   // + canonical JID: the JID its userStatusMute index named
)

// Versions of the actions sent. WhatsApp's clients set aside a mutation
// newer than every action they know and take any other, so these stay low.
const (
	statusPrivacyVersion = 1
	statusMuteVersion    = 2
)

type privacyMode = waSyncAction.StatusPrivacyAction_StatusDistributionMode

// modeOf reads an action's mode: mode, or the first of modes.
func modeOf(a *waSyncAction.StatusPrivacyAction) privacyMode {
	if a.Mode != nil {
		return a.GetMode()
	}
	if len(a.GetModes()) > 0 {
		return a.GetModes()[0]
	}
	return waSyncAction.StatusPrivacyAction_CONTACTS
}

// listKey names the wz_meta key of a mode's people, or "" for a mode
// without its own list.
func listKey(m privacyMode) string {
	switch m {
	case waSyncAction.StatusPrivacyAction_ALLOW_LIST:
		return metaStatusList + "allow"
	case waSyncAction.StatusPrivacyAction_DENY_LIST:
		return metaStatusList + "deny"
	case waSyncAction.StatusPrivacyAction_CLOSE_FRIENDS:
		return metaStatusList + "close"
	}
	return ""
}

// onStatusPrivacy keeps the status privacy synced from the phone.
func (b *Backend) onStatusPrivacy(a *waSyncAction.StatusPrivacyAction) {
	if a == nil {
		return
	}
	if err := b.keepStatusPrivacy(b.ctx, a); err != nil {
		b.log.Warnf("store status privacy: %v", err)
		return
	}
	b.emit(model.StatusEvent{})
}

// keepStatusPrivacy stores a, and its mode's people as that mode's list.
func (b *Backend) keepStatusPrivacy(ctx context.Context, a *waSyncAction.StatusPrivacyAction) error {
	raw, err := proto.Marshal(a)
	if err != nil {
		return err
	}
	if err := b.store.setMetaValue(ctx, metaStatusPrivacy, base64.StdEncoding.EncodeToString(raw)); err != nil {
		return err
	}
	if k := listKey(modeOf(a)); k != "" {
		return b.store.setMetaValue(ctx, k, strings.Join(a.GetUserJID(), ","))
	}
	return nil
}

// statusPrivacyAction returns the latest status privacy synced from the
// phone. A phone that never changed it syncs none: once app state has
// synced in full (resyncAppStateOnce), the setting WhatsApp's server keeps
// stands for it. Before, it returns nil.
func (b *Backend) statusPrivacyAction(ctx context.Context) *waSyncAction.StatusPrivacyAction {
	v := b.store.meta(ctx, metaStatusPrivacy)
	if v == "" {
		if b.store.meta(ctx, appStateResyncKey) == "" {
			return nil
		}
		return b.serverPrivacyAction()
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil
	}
	var a waSyncAction.StatusPrivacyAction
	if proto.Unmarshal(raw, &a) != nil {
		return nil
	}
	return &a
}

// serverPrivacyAction is the setting WhatsApp's server keeps, as a
// status_privacy action, or nil until it was fetched (StatusPrivacy).
func (b *Backend) serverPrivacyAction() *waSyncAction.StatusPrivacyAction {
	sp := &b.statusPriv
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.val == nil {
		return nil
	}
	a := &waSyncAction.StatusPrivacyAction{Mode: waSyncAction.StatusPrivacyAction_CONTACTS.Enum()}
	switch sp.val.Type {
	case types.StatusPrivacyTypeBlacklist:
		a.Mode = waSyncAction.StatusPrivacyAction_DENY_LIST.Enum()
	case types.StatusPrivacyTypeWhitelist:
		a.Mode = waSyncAction.StatusPrivacyAction_ALLOW_LIST.Enum()
	}
	for _, j := range sp.val.List {
		a.UserJID = append(a.UserJID, j.String())
	}
	return a
}

// statusList returns the people kept for a mode, as the phone named them,
// or those a's mode names for its own. known is false while no action has
// named them: the phone has its own list then, and they aren't nobody.
func (b *Backend) statusList(ctx context.Context, a *waSyncAction.StatusPrivacyAction, m privacyMode) (jids []string, known bool) {
	if modeOf(a) == m {
		return a.GetUserJID(), true
	}
	var v string
	if err := b.db.QueryRowContext(ctx, `SELECT value FROM wz_meta WHERE key = ?`, listKey(m)).Scan(&v); err != nil {
		// A phone that never synced its setting has none to keep.
		return nil, b.store.meta(ctx, metaStatusPrivacy) == ""
	}
	if v == "" {
		return nil, true
	}
	return strings.Split(v, ","), true
}

// chatIDs turns JIDs as an action names them into one-to-one chat IDs.
func (b *Backend) chatIDs(ctx context.Context, jids []string) []string {
	out := make([]string, 0, len(jids))
	for _, s := range jids {
		j, err := types.ParseJID(s)
		if err != nil || j.IsEmpty() {
			continue
		}
		if id := b.canonical(ctx, j).String(); !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// syncedStatusPrivacy is the status privacy synced from the phone, or nil.
func (b *Backend) syncedStatusPrivacy(ctx context.Context) *model.StatusPrivacy {
	a := b.statusPrivacyAction(ctx)
	if a == nil {
		return nil
	}
	p := &model.StatusPrivacy{Editable: true}
	for _, l := range []struct {
		mode     privacyMode
		audience model.StatusAudience
		ids      *[]string
	}{
		{waSyncAction.StatusPrivacyAction_DENY_LIST, model.AudienceExcept, &p.Except},
		{waSyncAction.StatusPrivacyAction_ALLOW_LIST, model.AudienceOnly, &p.Only},
		{waSyncAction.StatusPrivacyAction_CLOSE_FRIENDS, model.AudienceClose, &p.Close},
	} {
		jids, known := b.statusList(ctx, a, l.mode)
		*l.ids = b.chatIDs(ctx, jids)
		if !known {
			p.Unknown = append(p.Unknown, l.audience)
		}
	}
	selected := ""
	for _, l := range a.GetCustomLists() {
		p.Lists = append(p.Lists, model.StatusList{ID: l.GetListID(), Name: l.GetName(), Emoji: l.GetEmoji(),
			Members: b.chatIDs(ctx, l.GetUserJID())})
		if l.GetIsSelected() && selected == "" {
			selected = l.GetListID()
		}
	}
	switch modeOf(a) {
	case waSyncAction.StatusPrivacyAction_DENY_LIST:
		p.Audience, p.Count = model.AudienceExcept, len(p.Except)
	case waSyncAction.StatusPrivacyAction_ALLOW_LIST:
		p.Audience, p.Count = model.AudienceOnly, len(p.Only)
	case waSyncAction.StatusPrivacyAction_CLOSE_FRIENDS:
		p.Audience, p.Count = model.AudienceClose, len(p.Close)
	case waSyncAction.StatusPrivacyAction_CUSTOM_LIST:
		if l := p.List(selected); l != nil {
			p.Audience, p.ListID, p.Count = model.AudienceList, selected, len(l.Members)
		}
	}
	return p
}

// SetStatusPrivacy implements model.Backend. The setting is kept here at
// once and sent to the phone as a whole new status_privacy action, built
// on the last one so that what this app doesn't show (sharing to other
// apps) stays as it was.
func (b *Backend) SetStatusPrivacy(p model.StatusPrivacy) {
	ctx := b.ctx
	cur := b.statusPrivacyAction(ctx)
	if cur == nil || !p.Editable {
		b.emit(model.NoticeEvent{Text: "Your status privacy hasn't synced from your phone yet."})
		return
	}
	if b.connected() == nil {
		return
	}
	a, others := b.privacyAction(ctx, cur, p)
	if err := b.keepStatusPrivacy(ctx, a); err != nil {
		b.log.Warnf("store status privacy: %v", err)
	}
	// The other modes' lists change only here (the action names the
	// chosen one's people).
	for m, jids := range others {
		_ = b.store.setMetaValue(ctx, listKey(m), strings.Join(jids, ","))
	}
	b.emit(model.StatusEvent{})
	b.sendAppState(appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexStatusPrivacy},
			Version: statusPrivacyVersion,
			Value:   &waSyncAction.SyncActionValue{StatusPrivacy: a},
		}},
	})
}

// privacyAction builds the status_privacy action for p on top of cur, and
// the people of the modes it doesn't name.
func (b *Backend) privacyAction(ctx context.Context, cur *waSyncAction.StatusPrivacyAction, p model.StatusPrivacy) (*waSyncAction.StatusPrivacyAction, map[privacyMode][]string) {
	a := proto.CloneOf(cur)
	mode := waSyncAction.StatusPrivacyAction_CONTACTS
	var people []string
	switch p.Audience {
	case model.AudienceExcept:
		mode, people = waSyncAction.StatusPrivacyAction_DENY_LIST, p.Except
	case model.AudienceOnly:
		mode, people = waSyncAction.StatusPrivacyAction_ALLOW_LIST, p.Only
	case model.AudienceClose:
		mode, people = waSyncAction.StatusPrivacyAction_CLOSE_FRIENDS, p.Close
	case model.AudienceList:
		if p.List(p.ListID) != nil {
			mode = waSyncAction.StatusPrivacyAction_CUSTOM_LIST
		}
	}
	a.Mode, a.Modes = mode.Enum(), []privacyMode{mode}
	name := b.jidNamer(ctx, cur)
	if listKey(mode) != "" {
		a.UserJID = name(people)
	}
	// The other modes leave the people the phone sent as they were.
	old := map[string]*waSyncAction.StatusPrivacyAction_CustomList{}
	for _, l := range cur.GetCustomLists() {
		old[l.GetListID()] = l
	}
	a.CustomLists = nil
	for _, l := range p.Lists {
		cl := &waSyncAction.StatusPrivacyAction_CustomList{}
		if o := old[l.ID]; o != nil {
			cl = proto.CloneOf(o)
		}
		chosen := l.ID == p.ListID
		if l.ID == "" {
			l.ID = newListID()
		}
		cl.ListID, cl.Name, cl.Emoji = proto.String(l.ID), proto.String(l.Name), proto.String(l.Emoji)
		cl.UserJID = name(l.Members)
		cl.IsSelected = proto.Bool(mode == waSyncAction.StatusPrivacyAction_CUSTOM_LIST && chosen)
		a.CustomLists = append(a.CustomLists, cl)
	}
	// The other modes' people, those known: an unknown list stays the
	// phone's.
	others := map[privacyMode][]string{}
	for _, l := range []struct {
		mode     privacyMode
		audience model.StatusAudience
		ids      []string
	}{
		{waSyncAction.StatusPrivacyAction_DENY_LIST, model.AudienceExcept, p.Except},
		{waSyncAction.StatusPrivacyAction_ALLOW_LIST, model.AudienceOnly, p.Only},
		{waSyncAction.StatusPrivacyAction_CLOSE_FRIENDS, model.AudienceClose, p.Close},
	} {
		if l.mode != mode && p.Knows(l.audience) {
			others[l.mode] = name(l.ids)
		}
	}
	return a, others
}

// jidNamer returns a function that names chat IDs the way the phone names
// people in its status privacy: as it named them before, and new people
// by phone number unless the phone names most by LID.
func (b *Backend) jidNamer(ctx context.Context, cur *waSyncAction.StatusPrivacyAction) func(ids []string) []string {
	known := map[string]string{} // chat ID -> as the phone named it
	lids, pns := 0, 0
	note := func(jids []string) {
		for _, s := range jids {
			j, err := types.ParseJID(s)
			if err != nil || j.IsEmpty() {
				continue
			}
			known[b.canonical(ctx, j).String()] = s
			if j.Server == types.HiddenUserServer {
				lids++
			} else {
				pns++
			}
		}
	}
	note(cur.GetUserJID())
	for _, l := range cur.GetCustomLists() {
		note(l.GetUserJID())
	}
	for _, m := range []privacyMode{waSyncAction.StatusPrivacyAction_ALLOW_LIST, waSyncAction.StatusPrivacyAction_DENY_LIST,
		waSyncAction.StatusPrivacyAction_CLOSE_FRIENDS} {
		jids, _ := b.statusList(ctx, cur, m)
		note(jids)
	}
	byLID := lids > pns
	cli := b.client()
	return func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if s, ok := known[id]; ok {
				out = append(out, s)
				continue
			}
			j, err := types.ParseJID(id)
			if err != nil || j.IsEmpty() {
				continue
			}
			if !byLID && j.Server == types.HiddenUserServer && cli != nil {
				if pn, err := cli.Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
					j = pn
				}
			}
			out = append(out, j.ToNonAD().String())
		}
		return out
	}
}

// newListID makes the ID of a new audience: a random UUID.
func newListID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	h := hex.EncodeToString(u[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// errStatusAudience means a status would reach someone outside the
// audience you chose.
var errStatusAudience = errors.New("status recipients outside the chosen audience")

// statusAudience checks, before a status update goes out, that it reaches
// only the audience you chose. hypermeow sends it to the list WhatsApp's
// server keeps (GetStatusPrivacy), which may lag behind the setting synced
// from the phone, or not know audiences of your own. If anyone outside the
// chosen audience would get it, it returns errStatusAudience. For close
// friends and audiences of your own it also returns what tells the people
// who get it which audience it was shared with.
func (b *Backend) statusAudience(ctx context.Context, cli *whatsmeow.Client) (*waE2E.ContextInfo_StatusAudienceMetadata, error) {
	p := b.syncedStatusPrivacy(ctx)
	if p == nil {
		return nil, nil // not synced yet: the server's list, as always
	}
	recipients, err := cli.DangerousInternals().GetStatusBroadcastRecipients(ctx)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	allow := func(ids []string) {
		for _, id := range ids {
			allowed[id] = true
		}
	}
	var meta *waE2E.ContextInfo_StatusAudienceMetadata
	switch p.Audience {
	case model.AudienceContacts, model.AudienceExcept:
		for _, c := range b.Contacts() {
			allowed[c.ID] = true
		}
		if p.Audience == model.AudienceExcept {
			for _, id := range p.Except {
				delete(allowed, id)
			}
		}
	case model.AudienceOnly:
		allow(p.Only)
	case model.AudienceClose:
		allow(p.Close)
		meta = &waE2E.ContextInfo_StatusAudienceMetadata{AudienceType: waE2E.ContextInfo_StatusAudienceMetadata_CLOSE_FRIENDS.Enum()}
	case model.AudienceList:
		l := p.List(p.ListID)
		allow(l.Members)
		meta = &waE2E.ContextInfo_StatusAudienceMetadata{ListName: proto.String(l.Name), ListEmoji: proto.String(l.Emoji)}
	}
	for _, r := range recipients {
		if b.isMe(r) {
			continue
		}
		if !allowed[b.canonical(ctx, r).String()] {
			b.log.Warnf("status would reach %s, outside the audience (%d)", r, p.Audience)
			return nil, errStatusAudience
		}
	}
	return meta, nil
}

// audienceLabel is how an update shows the audience it was shared with:
// its emoji and name.
func audienceLabel(m *waE2E.ContextInfo_StatusAudienceMetadata) (emoji, name string) {
	if m == nil {
		return "", ""
	}
	emoji, name = m.GetListEmoji(), m.GetListName()
	if m.GetAudienceType() == waE2E.ContextInfo_StatusAudienceMetadata_CLOSE_FRIENDS && name == "" {
		emoji, name = "💜", "Close friends"
	}
	return emoji, name
}

// onStatusMute keeps a contact's status updates hidden or shown, as set on
// another device.
func (b *Backend) onStatusMute(e *events.UserStatusMute) {
	if e.JID.IsEmpty() {
		return
	}
	if err := b.keepStatusMute(b.ctx, e.JID, e.Action.GetMuted()); err != nil {
		b.log.Warnf("store status mute %s: %v", e.JID, err)
		return
	}
	b.emit(model.StatusEvent{})
}

func (b *Backend) keepStatusMute(ctx context.Context, j types.JID, muted bool) error {
	key := metaStatusMute + b.canonical(ctx, j).String()
	if muted {
		return b.store.setMetaValue(ctx, key, j.ToNonAD().String())
	}
	_, err := b.db.ExecContext(ctx, `DELETE FROM wz_meta WHERE key = ?`, key)
	return err
}

// statusMutes returns the contacts whose status updates are hidden, by
// canonical JID.
func (b *Backend) statusMutes(ctx context.Context) map[string]string {
	rows, err := b.db.QueryContext(ctx, `SELECT key, value FROM wz_meta WHERE key LIKE ?`, metaStatusMute+"%")
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[strings.TrimPrefix(k, metaStatusMute)] = v
		}
	}
	return out
}

// MuteStatus implements model.Backend. The index names the contact as the
// phone's last mutation of them did, so it replaces that one.
func (b *Backend) MuteStatus(id string, muted bool) {
	ctx := b.ctx
	j, err := types.ParseJID(id)
	if err != nil || j.IsEmpty() {
		return
	}
	if b.connected() == nil {
		return
	}
	target := j
	if s, ok := b.statusMutes(ctx)[b.canonical(ctx, j).String()]; ok {
		if t, err := types.ParseJID(s); err == nil {
			target = t
		}
	}
	if err := b.keepStatusMute(ctx, target, muted); err != nil {
		b.log.Warnf("store status mute %s: %v", id, err)
	}
	b.emit(model.StatusEvent{})
	b.sendAppState(appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexUserStatusMute, target.String()},
			Version: statusMuteVersion,
			Value:   &waSyncAction.SyncActionValue{UserStatusMuteAction: &waSyncAction.UserStatusMuteAction{Muted: proto.Bool(muted)}},
		}},
	})
}

// statusPrivacy caches your status privacy setting as WhatsApp's server
// has it, for before the phone has synced it.
type statusPrivacy struct {
	mu       sync.Mutex
	val      *types.StatusPrivacy
	at       time.Time
	fetching bool
}

// statusPrivacyTTL is how long the setting is trusted before it's fetched
// again (it changes on the phone).
const statusPrivacyTTL = time.Minute

// StatusPrivacy implements model.Backend: the setting synced from the
// phone, or the server's until it has.
func (b *Backend) StatusPrivacy() *model.StatusPrivacy {
	p := b.syncedStatusPrivacy(b.ctx)
	if p != nil && b.store.meta(b.ctx, metaStatusPrivacy) != "" {
		return p
	}
	// Not synced, or only the server's: fetch that (again, after a while).
	sp := &b.statusPriv
	sp.mu.Lock()
	defer sp.mu.Unlock()
	cli := b.client()
	if !sp.fetching && time.Since(sp.at) > statusPrivacyTTL && cli != nil && cli.IsConnected() {
		sp.fetching = true
		go func() {
			ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
			defer cancel()
			list, err := cli.GetStatusPrivacy(ctx)
			sp.mu.Lock()
			sp.fetching = false
			if err != nil || len(list) == 0 {
				sp.mu.Unlock()
				b.log.Warnf("get status privacy: %v", err)
				return
			}
			sp.val, sp.at = &list[0], time.Now()
			sp.mu.Unlock()
			b.emit(model.StatusEvent{})
		}()
	}
	if p != nil {
		return p
	}
	if sp.val == nil {
		return nil
	}
	return audienceOf(*sp.val)
}

// audienceOf converts the default status privacy list.
func audienceOf(p types.StatusPrivacy) *model.StatusPrivacy {
	out := &model.StatusPrivacy{Count: len(p.List)}
	switch p.Type {
	case types.StatusPrivacyTypeBlacklist:
		out.Audience = model.AudienceExcept
	case types.StatusPrivacyTypeWhitelist:
		out.Audience = model.AudienceOnly
	default:
		out.Audience, out.Count = model.AudienceContacts, 0
	}
	return out
}
