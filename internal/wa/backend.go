// Package wa is the WhatsApp backend, built on hypermeow
// (github.com/polymorfa/hypermeow, a whatsmeow fork).
package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waCompanionReg"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Backend implements model.Backend on top of a hypermeow client.
type Backend struct {
	dataDir string
	ctx     context.Context
	cancel  context.CancelFunc
	log     waLog.Logger
	logf    *os.File

	db        *sql.DB
	store     msgStore
	container *sqlstore.Container

	cliMu sync.Mutex
	cli   *whatsmeow.Client

	mu     sync.Mutex
	events []model.Event
	notify func()

	names     nameCache
	syncTimer *time.Timer

	avatars   *fetcher
	downloads *fetcher
	playing   sync.Map // files being downloaded for OpenMedia and MediaFile, by path

	subMu     sync.Mutex
	subtitles map[string]string // group JID → participant list

	infoMu      sync.Mutex
	infoFetched map[string]bool // info panels refreshed this session

	statusPriv statusPrivacy
}

var _ model.Backend = (*Backend)(nil)

// Open opens (or creates) the session database in dataDir.
func Open(dataDir string, debug bool) (*Backend, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dataDir, "wazzap.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 4<<20 {
		_ = os.Truncate(logPath, 0)
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	b := &Backend{
		dataDir:     dataDir,
		log:         newLogger(logf, debug),
		logf:        logf,
		avatars:     newFetcher(),
		downloads:   newFetcher(),
		subtitles:   make(map[string]string),
		infoFetched: make(map[string]bool),
	}
	b.ctx, b.cancel = context.WithCancel(context.Background())

	dsn := "file:" + filepath.ToSlash(filepath.Join(dataDir, "wazzap.db")) +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)"
	b.db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	b.container = sqlstore.NewWithDB(b.db, "sqlite", b.log.Sub("DB"))
	if err := b.container.Upgrade(b.ctx); err != nil {
		b.db.Close()
		return nil, fmt.Errorf("upgrade session database: %w", err)
	}
	b.store = msgStore{db: b.db}
	if err := b.store.init(b.ctx); err != nil {
		b.db.Close()
		return nil, fmt.Errorf("init message store: %w", err)
	}

	// How this client shows up under "Linked devices" on the phone.
	store.SetOSInfo("WazzapClients", [3]uint32{0, 1, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()

	// Create the client now (without connecting) so stored chats can be
	// shown, with names, before the connection is up.
	device, err := b.container.GetFirstDevice(b.ctx)
	if err != nil {
		b.db.Close()
		return nil, fmt.Errorf("open session: %w", err)
	}
	b.useDevice(device)
	return b, nil
}

func (b *Backend) client() *whatsmeow.Client {
	b.cliMu.Lock()
	defer b.cliMu.Unlock()
	return b.cli
}

func (b *Backend) emit(e model.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	notify := b.notify
	b.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (b *Backend) Poll() []model.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ev := b.events
	b.events = nil
	return ev
}

func (b *Backend) Start(notify func()) {
	b.mu.Lock()
	b.notify = notify
	b.mu.Unlock()
	go b.run()
	// WhatsApp rate-limits profile picture queries, so space them out.
	go b.avatars.run(b.ctx, 250*time.Millisecond)
	go b.downloads.run(b.ctx, 50*time.Millisecond)
}

func (b *Backend) run() {
	if b.client().Store.ID == nil {
		b.pair()
	} else {
		b.connect()
	}
}

func (b *Backend) useDevice(device *store.Device) {
	cli := whatsmeow.NewClient(device, b.log.Sub("Client"))
	cli.EnableAutoReconnect = true
	// Pins, mutes and archives only arrive through app state; the initial
	// full sync must emit them too.
	cli.EmitAppStateEventsOnFullSync = true
	cli.AddEventHandler(b.handle)
	b.cliMu.Lock()
	b.cli = cli
	b.cliMu.Unlock()
}

func (b *Backend) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	b.log.Errorf("%s", msg)
	b.emit(model.ConnEvent{State: model.StateError, Err: msg})
}

func (b *Backend) connect() {
	b.emit(model.ConnEvent{State: model.StateConnecting, Me: b.client().Store.PushName})
	if err := b.client().Connect(); err != nil {
		b.log.Warnf("connect: %v", err)
		b.emit(model.ConnEvent{State: model.StateOffline})
	}
}

// pair shows QR codes until the phone links this device or the codes run out.
func (b *Backend) pair() {
	cli := b.client()
	b.emit(model.ConnEvent{State: model.StateStarting})
	ch, err := cli.GetQRChannel(b.ctx)
	if err != nil {
		b.fail("Couldn't start linking: %v", err)
		return
	}
	if err := cli.Connect(); err != nil {
		b.fail("Couldn't connect to WhatsApp: %v", err)
		return
	}
	for item := range ch {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			b.emit(model.ConnEvent{State: model.StateQR, QR: item.Code})
		case whatsmeow.QRChannelSuccess.Event:
			b.emit(model.ConnEvent{State: model.StateConnecting})
			b.emit(model.SyncEvent{Percent: 0})
		case whatsmeow.QRChannelTimeout.Event:
			b.emit(model.ConnEvent{State: model.StateQRExpired})
		case whatsmeow.QRChannelEventPasskeyRequest:
			cli.Disconnect()
			b.fail("Your phone asked for passkey verification, which WazzapClients doesn't support yet.")
		case whatsmeow.QRChannelEventError:
			b.fail("Linking failed: %v", item.Error)
		default:
			b.fail("Linking failed (%s).", item.Event)
		}
	}
}

// Retry restarts linking after the QR codes expired, or reconnects an
// existing session after an error.
func (b *Backend) Retry() {
	go func() {
		cli := b.client()
		cli.Disconnect()
		if cli.Store.ID == nil {
			b.pair()
		} else {
			b.connect()
		}
	}()
}

// Logout unlinks this device on the phone and starts linking again.
func (b *Backend) Logout() {
	go func() {
		cli := b.client()
		if cli.Store.ID == nil {
			return
		}
		if err := cli.Logout(b.ctx); err != nil {
			b.log.Warnf("logout: %v", err)
		}
		b.resetSession()
	}()
}

func (b *Backend) Close() {
	if cli := b.client(); cli != nil {
		cli.Disconnect()
	}
	b.cancel()
	b.db.Close()
	b.logf.Close()
}

// resetSession wipes local data after the phone unlinked this device and
// starts linking again.
func (b *Backend) resetSession() {
	b.client().Disconnect()
	if err := b.store.wipe(b.ctx); err != nil {
		b.log.Errorf("wipe message store: %v", err)
	}
	b.names.clear()
	b.emit(model.ChatsEvent{})
	b.useDevice(b.container.NewDevice())
	b.pair()
}

func (b *Backend) Chats() []*model.Chat {
	raw, err := b.store.chats(b.ctx)
	if err != nil {
		b.log.Errorf("load chats: %v", err)
	}
	chats := make([]*model.Chat, len(raw))
	for i, rc := range raw {
		chats[i] = b.resolveChat(b.ctx, rc)
	}
	return chats
}

func (b *Backend) chat(jid string) *model.Chat {
	rc, ok := b.store.chat(b.ctx, jid)
	if !ok {
		return nil
	}
	return b.resolveChat(b.ctx, rc)
}

func (b *Backend) Messages(chatID string, limit int) []*model.Message {
	raw, err := b.store.messages(b.ctx, chatID, limit)
	return b.resolveMessages(chatID, raw, err)
}

func (b *Backend) MessagesBefore(chatID, id string, limit int) []*model.Message {
	raw, err := b.store.messagesBefore(b.ctx, chatID, id, limit)
	return b.resolveMessages(chatID, raw, err)
}

func (b *Backend) MessagesFrom(chatID, id string, limit int) []*model.Message {
	raw, err := b.store.messagesFrom(b.ctx, chatID, id, limit)
	return b.resolveMessages(chatID, raw, err)
}

func (b *Backend) PinnedMessage(chatID string) *model.Message {
	r, ok := b.store.pinnedMessage(b.ctx, chatID)
	if !ok {
		return nil
	}
	return b.resolveMessages(chatID, []rawMsg{r}, nil)[0]
}

func (b *Backend) resolveMessages(chatID string, raw []rawMsg, err error) []*model.Message {
	if err != nil {
		b.log.Errorf("load messages for %s: %v", chatID, err)
	}
	isGroup := false
	if j, err := types.ParseJID(chatID); err == nil {
		isGroup = j.Server == types.GroupServer
	}
	msgs := make([]*model.Message, len(raw))
	for i, r := range raw {
		msgs[i] = b.resolve(b.ctx, r, isGroup)
	}
	return msgs
}

// Open marks a chat read and subscribes to the contact's presence.
func (b *Backend) Open(chatID string) {
	go func() {
		ctx := b.ctx
		jid, err := types.ParseJID(chatID)
		if err != nil {
			return
		}
		if isChannel(jid) {
			_ = b.store.setField(ctx, chatID, "unread", 0)
			if cli := b.client(); cli != nil && cli.IsConnected() {
				b.fetchChannelPosts(ctx, cli, jid)
			}
			b.emit(model.ChannelsEvent{})
			return
		}
		c := b.chat(chatID)
		if c == nil {
			return
		}
		cli := b.client()
		if c.Unread < 0 && cli.IsConnected() {
			// Marked as unread: opening it marks it read again.
			ts, key := b.lastKey(chatID)
			b.sendAppState(appstate.BuildMarkChatAsRead(jid, true, ts, key))
			_ = b.store.setField(ctx, chatID, "unread", 0)
		}
		if c.Unread > 0 {
			ids, senders, err := b.store.unreadIncoming(ctx, chatID, min(c.Unread, 100))
			if err == nil && cli.IsConnected() {
				bySender := map[string][]types.MessageID{}
				for i, id := range ids {
					bySender[senders[i]] = append(bySender[senders[i]], id)
				}
				for s, ids := range bySender {
					sender, _ := types.ParseJID(s)
					if err := cli.MarkRead(ctx, ids, time.Now(), jid, sender); err != nil {
						b.log.Warnf("mark read in %s: %v", chatID, err)
					}
				}
			}
			_ = b.store.setField(ctx, chatID, "unread", 0)
		}
		switch {
		case !cli.IsConnected():
		case c.IsGroup:
			b.subMu.Lock()
			sub, ok := b.subtitles[chatID]
			b.subMu.Unlock()
			if !ok {
				info, err := cli.GetGroupInfo(ctx, jid)
				if err != nil {
					b.log.Debugf("group info %s: %v", chatID, err)
					return
				}
				sub = b.groupSubtitle(ctx, info.Participants)
				_ = b.store.setMembers(ctx, info)
				b.subMu.Lock()
				b.subtitles[chatID] = sub
				b.subMu.Unlock()
			}
			b.emit(model.PresenceEvent{ChatID: chatID, Text: sub})
		default:
			if err := cli.SubscribePresence(ctx, jid); err != nil {
				b.log.Debugf("subscribe presence %s: %v", chatID, err)
			}
		}
	}()
}

func (b *Backend) emitChat(jid string) {
	if strings.HasSuffix(jid, "@"+types.NewsletterServer) {
		b.emit(model.ChannelsEvent{})
		return
	}
	if c := b.chat(jid); c != nil {
		b.emit(model.ChatEvent{Chat: c})
	}
}

func (b *Backend) emitAllChats() {
	b.emit(model.ChatsEvent{Chats: b.Chats()})
}

// handle runs on hypermeow's event goroutine.
func (b *Backend) handle(evt any) {
	ctx := b.ctx
	switch e := evt.(type) {
	case *events.Connected:
		cli := b.client()
		var meID string
		if cli.Store.ID != nil {
			meID = cli.Store.ID.ToNonAD().String()
		}
		b.emit(model.ConnEvent{State: model.StateOnline, Me: cli.Store.PushName, MeID: meID})
		go func() {
			// Being "available" is what makes WhatsApp send typing notifications.
			if err := cli.SendPresence(ctx, types.PresenceAvailable); err != nil {
				b.log.Debugf("send presence: %v", err)
			}
			b.refreshGroupNames()
			b.resyncAppStateOnce()
			b.refreshChannels()
		}()
	case *events.Disconnected, *events.KeepAliveTimeout:
		b.emit(model.ConnEvent{State: model.StateOffline})
	case *events.KeepAliveRestored:
		b.emit(model.ConnEvent{State: model.StateOnline})
	case *events.LoggedOut:
		go b.resetSession()
	case *events.StreamReplaced:
		b.fail("WhatsApp is open on another computer with this session.")
	case *events.TemporaryBan:
		b.fail("%s", e.String())
	case *events.ClientOutdated:
		b.fail("WhatsApp says this client is outdated. Update hypermeow and try again.")
	case *events.ConnectFailure:
		b.fail("Couldn't connect: %s", e.PermanentDisconnectDescription())
	case *events.PairSuccess:
		b.log.Infof("paired as %s (%s)", e.ID, e.Platform)

	case *events.HistorySync:
		b.onHistory(e)
	case *events.Message:
		b.onMessage(e)
	case *events.Receipt:
		b.onReceipt(e)

	case *events.ChatPresence:
		chat := b.canonical(ctx, e.Chat)
		// Who must not be empty: the UI reads an empty Typing as nobody.
		who, whoID := b.chatName(ctx, chat), ""
		if e.IsGroup {
			who = b.senderName(ctx, e.Sender, "", "")
			whoID = b.canonical(ctx, e.Sender.ToNonAD()).String()
		}
		b.emit(model.TypingEvent{ChatID: chat.String(), Who: who, WhoID: whoID, Typing: e.State == types.ChatPresenceComposing})
	case *events.Presence:
		text := "online"
		if e.Unavailable {
			text = ""
			if !e.LastSeen.IsZero() {
				text = "last seen " + lastSeen(e.LastSeen, time.Now())
			}
		}
		b.emit(model.PresenceEvent{ChatID: b.canonical(ctx, e.From).String(), Text: text})

	case *events.Pin:
		var ts int64
		if e.Action.GetPinned() {
			ts = e.Timestamp.Unix()
		}
		b.updateChat(e.JID, "pinned", ts, e.FromFullSync)
	case *events.Mute:
		var until int64
		if e.Action.GetMuted() {
			until = e.Action.GetMuteEndTimestamp() / 1000 // milliseconds
			if e.Action.GetMuteEndTimestamp() < 0 {
				until = -1
			}
		}
		b.updateChat(e.JID, "muted_until", until, e.FromFullSync)
	case *events.Archive:
		b.updateChat(e.JID, "archived", boolInt(e.Action.GetArchived()), e.FromFullSync)
	case *events.MarkChatAsRead:
		if e.Action.GetRead() {
			b.updateChat(e.JID, "unread", 0, e.FromFullSync)
		} else if c, ok := b.store.chat(ctx, b.canonical(ctx, e.JID).String()); ok && c.Unread == 0 {
			b.updateChat(e.JID, "unread", -1, e.FromFullSync)
		}
	case *events.Star:
		chat := b.canonical(ctx, e.ChatJID).String()
		_ = b.store.setMessageFlag(ctx, chat, e.MessageID, "starred", e.Action.GetStarred())
		if !e.FromFullSync {
			b.emitMessage(chat, e.MessageID)
		}
	case *events.DeleteForMe:
		chat := b.canonical(ctx, e.ChatJID).String()
		_ = b.store.deleteMessage(ctx, chat, e.MessageID)
		if !e.FromFullSync {
			b.emit(model.DeletedEvent{ChatID: chat, IDs: []string{e.MessageID}})
			b.emitChat(chat)
		}
	case *events.ClearChat:
		upTo, ok := clearedUpTo(e.Action.GetMessageRange(), e.Timestamp)
		if !ok {
			return
		}
		chat := b.canonical(ctx, e.JID).String()
		_ = b.store.clearChat(ctx, chat, upTo)
		if !e.FromFullSync {
			b.emit(model.DeletedEvent{ChatID: chat})
			b.emitChat(chat)
		}
	case *events.DeleteChat:
		upTo, ok := clearedUpTo(e.Action.GetMessageRange(), e.Timestamp)
		if !ok {
			return
		}
		_ = b.store.deleteChat(ctx, b.canonical(ctx, e.JID).String(), upTo)
		if !e.FromFullSync {
			b.emitAllChats()
		}
	case *events.LabelEdit:
		b.onLabelEdit(e.LabelID, e.Action)
	case *events.LabelAssociationChat:
		b.onLabelChat(e.JID, e.LabelID, e.Action.GetLabeled(), e.FromFullSync)
	case *events.AppState:
		if len(e.Index) > 0 && e.Index[0] == appstate.IndexFavorites && e.GetFavoritesAction() != nil {
			b.onFavorites(e.GetFavoritesAction())
		}
		b.onStickerAppState(e)

	case *events.PushName, *events.Contact, *events.BusinessName:
		b.names.clear()
	case *events.AppStateSyncError:
		if errors.Is(e.Error, appstate.ErrMismatchingLTHash) {
			b.requestAppStateRecovery(e.Name)
		}
	case *events.AppStateSyncComplete:
		if e.Recovery {
			b.log.Infof("app state %s repaired by the phone (v%d)", e.Name, e.Version)
			_ = b.store.setMetaValue(ctx, appStateResyncKey+":"+string(e.Name), time.Now().Format(time.RFC3339))
		}
		// Contact names arrive through app state; re-title chats once they're in.
		b.names.clear()
		b.refreshChatNames()
	case *events.GroupInfo:
		if len(e.Join) > 0 || len(e.Leave) > 0 {
			b.store.updateMembers(ctx, e.JID.String(), e.Join, e.Leave)
		}
		for _, j := range e.Leave {
			if b.isMe(j) {
				// A group you left isn't one you share with anyone.
				b.store.clearMembers(ctx, e.JID.String())
			}
		}
		if e.Name != nil {
			jid := e.JID.String()
			_ = b.store.setName(ctx, jid, e.Name.Name)
			b.emitChat(jid)
		}
	case *events.JoinedGroup:
		jid := e.JID.String()
		_ = b.store.ensureChat(ctx, b.db, jid, true, e.Name)
		_ = b.store.setField(ctx, jid, "last_ts", time.Now().Unix())
		_ = b.store.setMembers(ctx, &e.GroupInfo)
		b.emitChat(jid)
	}
}

// updateChat stores one chat setting. During a full app state sync there are
// thousands of these, so they're applied quietly and the chat list is
// refreshed once when the sync completes.
func (b *Backend) updateChat(j types.JID, field string, v any, quiet bool) {
	jid := b.canonical(b.ctx, j).String()
	if err := b.store.setField(b.ctx, jid, field, v); err != nil {
		b.log.Warnf("update %s of %s: %v", field, jid, err)
	}
	if !quiet {
		b.emitChat(jid)
	}
}

// clearedUpTo returns the time (unix seconds) up to which a clear or delete
// from another device removes messages: the last message it covered, or
// else when it happened. Messages after it stay, as on the phone. App state
// keeps these actions for good and a full sync replays them, so deleting
// everything would wipe a chat's newer messages on every resync.
func clearedUpTo(r *waSyncAction.SyncActionMessageRange, at time.Time) (int64, bool) {
	if ts := r.GetLastMessageTimestamp(); ts > 0 {
		return ts, true
	}
	if !at.IsZero() && at.Unix() > 0 {
		return at.Unix(), true
	}
	return 0, false
}

// appStateResyncKey marks resyncAppStateOnce as done, in wz_meta. v2 also
// picks up lists and favourites, which older versions ignored; v3 favourite
// stickers; v4 again, for the ones v3 dropped.
const appStateResyncKey = "appstate_resynced_v4"

// recoveryGap is how long to wait before asking the phone again to repair
// the same app state collection.
const recoveryGap = time.Hour

// requestAppStateRecovery asks the phone for a fresh copy of an app state
// collection whose sync data no longer verifies (an LTHash mismatch), as
// WhatsApp's own linked devices do. Until then the collection can't sync,
// and WhatsApp rejects every change sent to it. The copy arrives as an
// AppStateSyncComplete with Recovery set.
func (b *Backend) requestAppStateRecovery(name appstate.WAPatchName) {
	key := "appstate_recovery:" + string(name)
	if t, err := time.Parse(time.RFC3339, b.store.meta(b.ctx, key)); err == nil && time.Since(t) < recoveryGap {
		return
	}
	cli := b.client()
	if cli == nil {
		return
	}
	_ = b.store.setMetaValue(b.ctx, key, time.Now().Format(time.RFC3339))
	go func() {
		if _, err := cli.SendPeerMessage(b.ctx, whatsmeow.BuildAppStateRecoveryRequest(name)); err != nil {
			b.log.Warnf("ask phone to repair app state %s: %v", name, err)
			return
		}
		b.log.Infof("asked the phone to repair app state %s", name)
	}()
}

// resyncAppStateOnce refetches all app state (pins, mutes, archives,
// contacts) once per session database. Sessions linked before app state
// events were enabled never received their pins and mutes.
func (b *Backend) resyncAppStateOnce() {
	key := appStateResyncKey
	if b.store.meta(b.ctx, key) != "" {
		return
	}
	cli := b.client()
	for _, name := range appstate.AllPatchNames {
		// Remember each patch that synced, so that one which keeps failing
		// doesn't refetch all the others on every connect.
		done := key + ":" + string(name)
		if b.store.meta(b.ctx, done) != "" {
			continue
		}
		if err := cli.FetchAppState(b.ctx, name, true, false); err != nil {
			b.log.Warnf("resync app state %s: %v", name, err)
			return
		}
		_ = b.store.setMetaValue(b.ctx, done, time.Now().Format(time.RFC3339))
	}
	_ = b.store.setMetaValue(b.ctx, key, time.Now().Format(time.RFC3339))
	b.names.clear()
	b.refreshChatNames()
}

func (b *Backend) onMessage(e *events.Message) {
	ctx := b.ctx
	if isStatus(e.Info.Chat) {
		b.onStatus(e)
		return
	}
	p, ok := b.parse(ctx, e)
	if !ok {
		return
	}
	chat := p.msg.ChatID
	isNew := false
	switch {
	case p.revoke:
		_ = b.store.markDeleted(ctx, chat, p.target)
	case p.edit != "":
		_ = b.store.editText(ctx, chat, p.target, p.edit)
	case p.pin != 0:
		if p.pin > 0 {
			_, _ = b.db.ExecContext(ctx, `UPDATE wz_messages SET pinned = 0 WHERE chat = ?`, chat)
		}
		_ = b.store.setMessageFlag(ctx, chat, p.target, "pinned", p.pin > 0)
		b.emitAllMessages(chat)
		return
	case p.target != "":
		_ = b.store.setReaction(ctx, chat, p.target, p.reaction)
	default:
		name := ""
		chatJID, _ := types.ParseJID(chat)
		if !e.Info.IsGroup && !isChannel(chatJID) {
			name = b.chatName(ctx, chatJID)
		}
		_, exists := b.store.chat(ctx, chat)
		// A message can come again (a retry); it counts and notifies once.
		_, seen := b.store.message(ctx, chat, p.msg.ID)
		if err := b.store.ensureChat(ctx, b.db, chat, e.Info.IsGroup, name); err != nil {
			b.log.Errorf("store chat %s: %v", chat, err)
			return
		}
		if err := b.store.putMessage(ctx, b.db, p.msg); err != nil {
			b.log.Errorf("store message %s: %v", p.msg.ID, err)
			return
		}
		if !p.msg.FromMe {
			if !seen {
				_ = b.store.addUnread(ctx, chat)
				isNew = true
			}
		} else if p.msg.Media == model.MediaSticker && len(p.msg.mediaBlob) > 0 {
			b.recentSticker(p.msg.mediaBlob, p.msg.Time, "", "") // sent from another device
		}
		if !exists && e.Info.IsGroup {
			go b.fetchGroupName(chatJID)
		}
		p.target = p.msg.ID
	}
	if r, ok := b.store.message(ctx, chat, p.target); ok {
		b.emit(model.MessageEvent{Msg: b.resolve(ctx, r, e.Info.IsGroup), New: isNew})
		b.emitChat(chat)
	}
}

func (b *Backend) onReceipt(e *events.Receipt) {
	ctx := b.ctx
	chat := b.canonical(ctx, e.Chat).String()
	var r model.Receipt
	if isStatus(e.Chat) {
		// Statuses seen on another device.
		if e.Type == types.ReceiptTypeReadSelf {
			ids := make([]string, len(e.MessageIDs))
			for i, id := range e.MessageIDs {
				ids[i] = string(id)
			}
			_ = b.store.setStatusViewed(ctx, ids)
			b.emit(model.StatusEvent{})
		}
		return
	}
	switch e.Type {
	case types.ReceiptTypeReadSelf:
		b.updateChat(e.Chat, "unread", 0, false)
		return
	case types.ReceiptTypeDelivered:
		r = model.Delivered
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
		r = model.Read
	default:
		return
	}
	if e.IsFromMe {
		return // our own other device reading/receiving someone else's message
	}
	ids := make([]string, len(e.MessageIDs))
	for i, id := range e.MessageIDs {
		ids[i] = string(id)
	}
	if err := b.store.setReceipt(ctx, chat, ids, r); err != nil {
		b.log.Warnf("store receipt: %v", err)
	}
	b.emit(model.ReceiptEvent{ChatID: chat, IDs: ids, Receipt: r})
}

// onHistory stores a history sync chunk. All device-store lookups (message
// parsing, names) happen before the write transaction starts: hypermeow may
// write to the same SQLite file while parsing, and doing that inside our
// transaction would deadlock until the busy timeout.
func (b *Backend) onHistory(e *events.HistorySync) {
	ctx := b.ctx
	cli := b.client()
	data := e.Data

	type convData struct {
		jid     types.JID
		isGroup bool
		name    string
		meta    chatMeta
		msgs    []storedMsg
		edits   []parsed
	}
	var convs []convData
	var statuses []storedStatus
	for _, conv := range data.GetConversations() {
		raw, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		if isStatus(raw) {
			for _, hm := range conv.GetMessages() {
				if evt, err := cli.ParseWebMessage(raw, hm.GetMessage()); err == nil {
					if st, ok := b.parseStatus(ctx, evt); ok && st.revoke == "" {
						statuses = append(statuses, st)
					}
				}
			}
			continue
		}
		jid := b.canonical(ctx, raw)
		if lid, err := types.ParseJID(conv.GetLidJID()); err == nil && lid.Server == types.HiddenUserServer {
			jid = lid
		}
		if skipChat(jid) {
			continue
		}
		cd := convData{jid: jid, isGroup: jid.Server == types.GroupServer}
		if cd.isGroup || isChannel(jid) {
			cd.name = first(conv.GetName(), conv.GetDisplayName())
		} else {
			cd.name = b.chatName(ctx, jid)
		}
		mute := int64(conv.GetMuteEndTime())
		if conv.GetMuteEndTime() == ^uint64(0) {
			mute = -1
		} else if mute > 1e11 {
			mute /= 1000 // milliseconds
		}
		cd.meta = chatMeta{
			pinned:     int64(conv.GetPinned()),
			mutedUntil: mute,
			archived:   conv.GetArchived(),
			unread:     int(conv.GetUnreadCount()),
			lastTS:     int64(max(conv.GetConversationTimestamp(), conv.GetLastMsgTimestamp())),
		}
		for _, hm := range conv.GetMessages() {
			evt, err := cli.ParseWebMessage(raw, hm.GetMessage())
			if err != nil {
				continue
			}
			p, ok := b.parse(ctx, evt)
			if !ok {
				continue
			}
			p.msg.ChatID = jid.String()
			if p.target != "" {
				cd.edits = append(cd.edits, p)
				continue
			}
			cd.msgs = append(cd.msgs, p.msg)
		}
		convs = append(convs, cd)
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		b.log.Errorf("history sync: begin: %v", err)
		return
	}
	for _, st := range statuses {
		if err := b.store.putStatus(ctx, tx, st); err != nil {
			b.log.Warnf("history sync: status %s: %v", st.id, err)
		}
	}
	for _, cd := range convs {
		jid := cd.jid.String()
		if err := b.store.ensureChat(ctx, tx, jid, cd.isGroup, cd.name); err != nil {
			b.log.Errorf("history sync: chat %s: %v", jid, err)
			continue
		}
		_ = b.store.setMeta(ctx, tx, jid, cd.meta)
		for _, m := range cd.msgs {
			if err := b.store.putMessage(ctx, tx, m); err != nil {
				b.log.Warnf("history sync: message %s: %v", m.ID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.log.Errorf("history sync: commit: %v", err)
		return
	}
	for _, cd := range convs {
		for _, p := range cd.edits {
			chat := cd.jid.String()
			switch {
			case p.revoke:
				_ = b.store.markDeleted(ctx, chat, p.target)
			case p.edit != "":
				_ = b.store.editText(ctx, chat, p.target, p.edit)
			case p.pin != 0:
				_ = b.store.setMessageFlag(ctx, chat, p.target, "pinned", p.pin > 0)
			default:
				_ = b.store.setReaction(ctx, chat, p.target, p.reaction)
			}
		}
	}
	b.onRecentStickers(data.GetRecentStickers())
	b.log.Infof("history sync %s: %d conversations, progress %d%%",
		data.GetSyncType(), len(convs), data.GetProgress())
	if data.GetSyncType() == waHistorySync.HistorySync_PUSH_NAME {
		// hypermeow stores these push names; re-resolve titles once it has.
		time.AfterFunc(2*time.Second, func() {
			b.names.clear()
			b.refreshChatNames()
		})
		return
	}
	b.emitAllChats()
	if len(statuses) > 0 {
		b.emit(model.StatusEvent{})
	}

	switch data.GetSyncType() {
	case waHistorySync.HistorySync_INITIAL_BOOTSTRAP, waHistorySync.HistorySync_RECENT, waHistorySync.HistorySync_FULL:
		b.emit(model.SyncEvent{Percent: int(min(data.GetProgress(), 99))})
		// WhatsApp doesn't reliably send a final 100%; call it done once chunks stop.
		b.mu.Lock()
		if b.syncTimer != nil {
			b.syncTimer.Stop()
		}
		b.syncTimer = time.AfterFunc(20*time.Second, func() {
			// Names and LID mappings from history are stored in the
			// background, so re-resolve everything once the chunks stop.
			b.names.clear()
			b.refreshChatNames()
			b.emit(model.SyncEvent{Percent: 100})
			// History sync allocates a lot briefly; give it back to the OS.
			debug.FreeOSMemory()
		})
		b.mu.Unlock()
	}
}

// refreshChatNames re-resolves one-to-one chat titles from the contact store.
func (b *Backend) refreshChatNames() {
	ctx := b.ctx
	jids, err := b.store.chatJIDs(ctx, false)
	if err != nil {
		return
	}
	for _, s := range jids {
		j, err := types.ParseJID(s)
		if err != nil {
			continue
		}
		_ = b.store.setName(ctx, s, b.chatName(ctx, j))
	}
	b.emitAllChats()
}

func (b *Backend) refreshGroupNames() {
	groups, err := b.client().GetJoinedGroups(b.ctx)
	if err != nil {
		b.log.Warnf("get joined groups: %v", err)
		return
	}
	joined := make([]string, len(groups))
	for i, g := range groups {
		joined[i] = g.JID.String()
	}
	b.store.keepMembers(b.ctx, joined)
	for _, g := range groups {
		if err := b.store.setGroupShape(b.ctx, g); err != nil {
			b.log.Warnf("store group %s: %v", g.JID, err)
		}
		if err := b.store.setMembers(b.ctx, g); err != nil {
			b.log.Warnf("store members of %s: %v", g.JID, err)
		}
		if g.Name != "" {
			_ = b.store.setName(b.ctx, g.JID.String(), g.Name)
		}
	}
	b.emitAllChats()
	b.emit(model.CommunitiesEvent{})
}

func (b *Backend) fetchGroupName(j types.JID) {
	info, err := b.client().GetGroupInfo(b.ctx, j)
	if err != nil || info.Name == "" {
		return
	}
	_ = b.store.setName(b.ctx, j.String(), info.Name)
	b.emitChat(j.String())
}

func lastSeen(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "today at " + t.Format("15:04")
	case now.Sub(t) < 48*time.Hour && d2-d1 == 1:
		return "yesterday at " + t.Format("15:04")
	default:
		return t.Format("02/01/2006") + " at " + t.Format("15:04")
	}
}
