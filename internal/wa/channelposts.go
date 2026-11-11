package wa

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A channel's posts are stored like a chat's messages. What the server
// says of each besides is kept in wz_channel_posts: its server ID, which
// reactions, views and paging name it by, and its reaction counts. Nobody
// sees who reacted to a post, only how many did with each emoji, so the
// message's reaction column (reactionSummary's format) is made from those
// counts and your own reaction, kept in wz_reactions under meVoter.
const channelPostsSchema = `
CREATE TABLE IF NOT EXISTS wz_channel_posts (
	chat      TEXT NOT NULL,
	id        TEXT NOT NULL,
	server_id INTEGER NOT NULL,
	reactions TEXT NOT NULL DEFAULT '', -- emoji TAB count, a line each, as the server counted them
	PRIMARY KEY (chat, id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS wz_channel_posts_sid ON wz_channel_posts (chat, server_id);
`

// channelPostPage is how many posts a channel's page fetch asks for.
const channelPostPage = 50

// putPost records a post's server ID and, when counts isn't nil, its
// reaction counts, and updates the message's reaction summary.
func (s *msgStore) putPost(ctx context.Context, chat, id string, sid types.MessageServerID, counts map[string]int) error {
	if sid == 0 && counts == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO wz_channel_posts (chat, id, server_id, reactions) VALUES (?1, ?2, ?3, ?4)
		ON CONFLICT (chat, id) DO UPDATE SET
			server_id = CASE WHEN ?3 != 0 THEN ?3 ELSE server_id END,
			reactions = CASE WHEN ?5 THEN ?4 ELSE reactions END`,
		chat, id, int64(sid), encodeCounts(counts), counts != nil)
	if err != nil || counts == nil {
		return err
	}
	return s.postSummary(ctx, chat, id)
}

// encodeCounts writes reaction counts the way wz_channel_posts keeps them,
// the most given first.
func encodeCounts(counts map[string]int) string {
	var cs []model.ReactionCount
	for e, n := range counts {
		if e != "" && n > 0 {
			cs = append(cs, model.ReactionCount{Emoji: e, Count: n})
		}
	}
	slices.SortFunc(cs, func(a, b model.ReactionCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Emoji, b.Emoji)
	})
	var sb strings.Builder
	for i, c := range cs {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(c.Emoji + "\t" + strconv.Itoa(c.Count))
	}
	return sb.String()
}

func decodeCounts(s string) map[string]int {
	out := map[string]int{}
	for _, l := range strings.Split(s, "\n") {
		e, n, ok := strings.Cut(l, "\t")
		if c, err := strconv.Atoi(n); ok && err == nil && e != "" && c > 0 {
			out[e] = c
		}
	}
	return out
}

// postCounts returns a post's reaction counts and server ID.
func (s *msgStore) postCounts(ctx context.Context, chat, id string) (map[string]int, types.MessageServerID) {
	var enc string
	var sid int64
	_ = s.db.QueryRowContext(ctx, `SELECT reactions, server_id FROM wz_channel_posts WHERE chat = ? AND id = ?`,
		chat, id).Scan(&enc, &sid)
	return decodeCounts(enc), types.MessageServerID(sid)
}

// myReaction is your reaction to a message, or "".
func (s *msgStore) myReaction(ctx context.Context, chat, id string) string {
	var e string
	_ = s.db.QueryRowContext(ctx, `SELECT emoji FROM wz_reactions WHERE chat = ? AND id = ? AND who = ?`,
		chat, id, meVoter).Scan(&e)
	return e
}

// postSummary writes a post's reaction summary from its counts and your
// own reaction, which counts at least once.
func (s *msgStore) postSummary(ctx context.Context, chat, id string) error {
	counts, _ := s.postCounts(ctx, chat, id)
	mine := s.myReaction(ctx, chat, id)
	if mine != "" && counts[mine] == 0 {
		counts[mine] = 1
	}
	summary := ""
	if enc := encodeCounts(counts); enc != "" {
		summary = mine + "\n" + enc
	}
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET reaction = ? WHERE chat = ? AND id = ?`, summary, chat, id)
	return err
}

// postBySID returns the ID of the post of chat with server ID sid.
func (s *msgStore) postBySID(ctx context.Context, chat string, sid types.MessageServerID) (string, bool) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM wz_channel_posts WHERE chat = ? AND server_id = ?`,
		chat, int64(sid)).Scan(&id)
	return id, err == nil
}

// oldestPost returns the server ID of the oldest post of chat whose server
// ID is known, or 0.
func (s *msgStore) oldestPost(ctx context.Context, chat string) types.MessageServerID {
	var sid sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT MIN(server_id) FROM wz_channel_posts WHERE chat = ? AND server_id > 0`,
		chat).Scan(&sid)
	return types.MessageServerID(sid.Int64)
}

// newestPosts returns the server IDs of chat's newest n posts.
func (s *msgStore) newestPosts(ctx context.Context, chat string, n int) []types.MessageServerID {
	rows, err := s.db.QueryContext(ctx, `SELECT server_id FROM wz_channel_posts WHERE chat = ? AND server_id > 0
		ORDER BY server_id DESC LIMIT ?`, chat, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.MessageServerID
	for rows.Next() {
		var sid int64
		if rows.Scan(&sid) == nil {
			out = append(out, types.MessageServerID(sid))
		}
	}
	return out
}

// fetchChannelPosts stores a page of a channel's posts: the newest, or
// those before server ID before. It returns how many it hadn't stored,
// and how many the server sent.
func (b *Backend) fetchChannelPosts(ctx context.Context, cli *whatsmeow.Client, jid types.JID, before types.MessageServerID) (added, got int) {
	msgs, err := cli.GetNewsletterMessages(ctx, jid, &whatsmeow.GetNewsletterMessagesParams{Count: channelPostPage, Before: before})
	if err != nil {
		b.log.Debugf("channel posts %s: %v", jid, err)
		return 0, 0
	}
	chat := jid.String()
	_ = b.store.ensureChat(ctx, b.db, chat, false, "")
	for _, nm := range msgs {
		id := string(nm.MessageID)
		if id == "" {
			id = strconv.Itoa(int(nm.MessageServerID))
		}
		if nm.Message == nil {
			// A post deleted since: the server keeps its place only.
			if byServer, ok := b.store.postBySID(ctx, chat, nm.MessageServerID); ok {
				id = byServer
			}
			if old, ok := b.store.message(ctx, chat, id); ok && old.Kind != model.KindDeleted {
				_ = b.store.markDeleted(ctx, chat, id, "")
				b.emitMessage(chat, id)
			}
			// Its place still counts for paging past it.
			_ = b.store.putPost(ctx, chat, id, nm.MessageServerID, nil)
			continue
		}
		c := describe(nm.Message)
		if c.text == "" && c.media == model.MediaNone {
			continue
		}
		old, seen := b.store.message(ctx, chat, id)
		changed := !seen
		if seen && c.text != "" && old.Kind != model.KindDeleted && old.Text != c.text {
			// Edited since it was stored: fetched posts come as they are now.
			if err := b.store.editText(ctx, chat, id, c.text, b.now(), marshal(nm.Message)); err == nil {
				changed = true
			}
		} else if !seen {
			m := storedMsg{Message: &model.Message{
				ID: id, ChatID: chat, Kind: c.kind, Media: c.media, Text: c.text, Time: nm.Timestamp, Receipt: model.Read,
			}, senderJID: chat, rawPayload: marshal(nm.Message)}
			if err := b.store.putMessage(ctx, b.db, m); err != nil {
				b.log.Debugf("store channel post: %v", err)
				continue
			}
			added++
		}
		counts := nm.ReactionCounts
		if counts == nil {
			counts = map[string]int{}
		}
		oldCounts, _ := b.store.postCounts(ctx, chat, id)
		if !maps.Equal(oldCounts, counts) {
			changed = true
		}
		_ = b.store.putPost(ctx, chat, id, nm.MessageServerID, counts)
		if changed && before == 0 {
			// An older page shows through OlderEvent instead.
			b.emitMessage(chat, id)
		}
	}
	return added, len(msgs)
}

// olderPosts fetches the page of a channel's posts before the oldest
// stored, unless it's fetching it already or the channel has no more. An
// OlderEvent follows when it brought any.
func (b *Backend) olderPosts(chat string) {
	jid, err := types.ParseJID(chat)
	cli := b.client()
	if err != nil || !isChannel(jid) || cli == nil || !cli.IsConnected() {
		return
	}
	b.chanMu.Lock()
	if b.olderBusy[chat] || b.olderDone[chat] {
		b.chanMu.Unlock()
		return
	}
	b.olderBusy[chat] = true
	b.chanMu.Unlock()
	go func() {
		// Posts stored by an earlier version have no server ID: their
		// pages bring nothing new, and the next one is asked for, until
		// one does or the server has no more.
		n, done := 0, false
		for page := 0; page < 40 && n == 0 && !done; page++ {
			sid := b.store.oldestPost(b.ctx, chat)
			if sid == 0 {
				break // none fetched yet: opening the channel does
			}
			got := 0
			if sid > 1 {
				n, got = b.fetchChannelPosts(b.ctx, cli, jid, sid)
			}
			// The channel's first post is here, or the server keeps no
			// more (it drops updates after a month).
			done = got == 0 || b.store.oldestPost(b.ctx, chat) >= sid
		}
		b.chanMu.Lock()
		delete(b.olderBusy, chat)
		if done {
			b.olderDone[chat] = true
		}
		b.chanMu.Unlock()
		if n > 0 {
			b.emit(model.OlderEvent{ChatID: chat})
		}
	}()
}

// openChannel brings a channel's newest posts when it opens, counts you
// among their viewers and asks for live reaction counts.
func (b *Backend) openChannel(ctx context.Context, jid types.JID) {
	chat := jid.String()
	_ = b.store.setField(ctx, chat, "unread", 0)
	b.emit(model.ChannelsEvent{})
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return
	}
	b.fetchChannelPosts(ctx, cli, jid, 0)
	b.emit(model.ChannelsEvent{})
	if raw, err := b.store.messages(ctx, chat, 100); err == nil && len(raw) < 100 {
		b.olderPosts(chat)
	}
	b.goWork(func() { b.liveUpdates(cli, jid) })
	if !b.ghost() {
		if sids := b.store.newestPosts(ctx, chat, 20); len(sids) > 0 {
			if err := cli.NewsletterMarkViewed(ctx, jid, sids); err != nil {
				b.log.Debugf("mark channel posts viewed %s: %v", jid, err)
			}
		}
	}
}

// liveUpdates keeps asking for a channel's live reaction counts, which the
// server sends for a minute or two at a time, while it's the channel
// opened last and for at most liveFor.
func (b *Backend) liveUpdates(cli *whatsmeow.Client, jid types.JID) {
	const liveFor = 15 * time.Minute
	gen := b.liveGen.Add(1)
	for end := time.Now().Add(liveFor); time.Now().Before(end) && b.liveGen.Load() == gen && cli.IsConnected(); {
		d, err := cli.NewsletterSubscribeLiveUpdates(b.ctx, jid)
		if err != nil {
			b.log.Debugf("channel live updates %s: %v", jid, err)
			return
		}
		select {
		case <-b.ctx.Done():
			return
		case <-time.After(max(d, 30*time.Second) - 5*time.Second):
		}
	}
}

// onChannelMessage handles what a channel's message stanza does to a post
// stored before: an edit (the new content under the post's ID) or a
// delete. It reports whether it was one.
func (b *Backend) onChannelMessage(e *events.Message) bool {
	ctx := b.ctx
	chat := e.Info.Chat.String()
	id := e.Info.ID
	if _, ok := b.store.message(ctx, chat, id); !ok && e.Info.ServerID != 0 {
		if byServer, ok := b.store.postBySID(ctx, chat, e.Info.ServerID); ok {
			id = byServer
		}
	}
	switch {
	case e.Info.Edit == types.EditAttributeAdminRevoke || e.Info.Edit == types.EditAttributeSenderRevoke:
		if _, ok := b.store.message(ctx, chat, id); ok {
			_ = b.store.markDeleted(ctx, chat, id, "")
			b.emitMessage(chat, id)
			b.emitChat(chat)
		}
		return true
	case e.NewsletterMeta != nil && !e.NewsletterMeta.EditTS.IsZero():
		var p parsed
		if p.setEdit(id, e.Message, e.NewsletterMeta.EditTS) {
			p.msg.Message = &model.Message{ChatID: chat}
			b.apply(&e.Info, p)
		}
		return true
	}
	return false
}

// onChannelUpdate applies a channel's live reaction counts.
func (b *Backend) onChannelUpdate(e *events.NewsletterLiveUpdate) {
	ctx := b.ctx
	chat := e.JID.String()
	for _, nm := range e.Messages {
		id, ok := b.store.postBySID(ctx, chat, nm.MessageServerID)
		if !ok || nm.ReactionCounts == nil {
			continue
		}
		_ = b.store.putPost(ctx, chat, id, 0, nm.ReactionCounts)
		b.emitMessage(chat, id)
	}
}

// reactChannel reacts to a channel's post, which goes by its server ID.
func (b *Backend) reactChannel(cli *whatsmeow.Client, jid types.JID, m *model.Message, emoji string) {
	ctx := b.ctx
	counts, sid := b.store.postCounts(ctx, m.ChatID, m.ID)
	if sid == 0 {
		b.emit(model.NoticeEvent{Text: "Couldn't react to this update."})
		return
	}
	// Count it at once; the server's counts replace these later.
	if old := b.store.myReaction(ctx, m.ChatID, m.ID); old != "" && counts[old] > 0 {
		counts[old]--
	}
	if emoji != "" {
		counts[emoji]++
	}
	_, _ = b.db.ExecContext(ctx, `INSERT INTO wz_reactions (chat, id, who, ts, emoji) VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT (chat, id, who) DO UPDATE SET ts = excluded.ts, emoji = excluded.emoji`,
		m.ChatID, m.ID, meVoter, b.sendTime().UnixMilli(), emoji)
	_ = b.store.putPost(ctx, m.ChatID, m.ID, 0, counts)
	b.emitMessage(m.ChatID, m.ID)
	go func() {
		if err := cli.NewsletterSendReaction(ctx, jid, sid, emoji, ""); err != nil {
			b.log.Warnf("react in channel %s: %v", m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't send the reaction."})
		}
	}()
}

// channelJob runs f with the client on a goroutine, when connected, and
// says what failed.
func (b *Backend) channelJob(id, what string, f func(context.Context, *whatsmeow.Client, types.JID) error) {
	jid, err := types.ParseJID(id)
	cli := b.client()
	if err != nil || !isChannel(jid) {
		return
	}
	if cli == nil || !cli.IsConnected() {
		b.emit(model.NoticeEvent{Text: "You're offline. Try again when you're connected."})
		return
	}
	go func() {
		if err := f(b.ctx, cli, jid); err != nil {
			b.log.Warnf("%s %s: %v", what, id, err)
			b.emit(model.NoticeEvent{Text: "Couldn't " + what + "."})
		}
		b.emit(model.ChannelsEvent{})
	}()
}

// FollowChannel implements model.Backend.
func (b *Backend) FollowChannel(id string) {
	b.channelJob(id, "follow the channel", func(ctx context.Context, cli *whatsmeow.Client, jid types.JID) error {
		if err := cli.FollowNewsletter(ctx, jid); err != nil {
			return err
		}
		_, _ = b.db.ExecContext(ctx, `UPDATE wz_channels SET following = 1, rank = 0 WHERE jid = ?`, id)
		b.refreshChannel(ctx, cli, jid)
		b.fetchChannelPosts(ctx, cli, jid, 0)
		return nil
	})
}

// UnfollowChannel implements model.Backend.
func (b *Backend) UnfollowChannel(id string) {
	b.channelJob(id, "unfollow the channel", func(ctx context.Context, cli *whatsmeow.Client, jid types.JID) error {
		if err := cli.UnfollowNewsletter(ctx, jid); err != nil {
			return err
		}
		_, err := b.db.ExecContext(ctx, `UPDATE wz_channels SET following = 0 WHERE jid = ?`, id)
		return err
	})
}

// MuteChannel implements model.Backend.
func (b *Backend) MuteChannel(id string, muted bool) {
	// It shows at once; a failure puts it back.
	_, _ = b.db.ExecContext(b.ctx, `UPDATE wz_channels SET muted = ? WHERE jid = ?`, boolInt(muted), id)
	b.emit(model.ChannelsEvent{})
	b.channelJob(id, "change the channel's notifications", func(ctx context.Context, cli *whatsmeow.Client, jid types.JID) error {
		err := cli.NewsletterToggleMute(ctx, jid, muted)
		if err != nil {
			_, _ = b.db.ExecContext(ctx, `UPDATE wz_channels SET muted = ? WHERE jid = ?`, boolInt(!muted), id)
		}
		return err
	})
}

// RefreshChannel implements model.Backend.
func (b *Backend) RefreshChannel(id string) {
	jid, err := types.ParseJID(id)
	cli := b.client()
	if err != nil || !isChannel(jid) || cli == nil || !cli.IsConnected() {
		return
	}
	go func() {
		b.refreshChannel(b.ctx, cli, jid)
		b.emit(model.ChannelsEvent{})
	}()
}

// refreshChannel stores a channel's details as the server has them now.
func (b *Backend) refreshChannel(ctx context.Context, cli *whatsmeow.Client, jid types.JID) {
	n, err := cli.GetNewsletterInfo(ctx, jid)
	if err != nil || n == nil {
		b.log.Debugf("channel info %s: %v", jid, err)
		return
	}
	m := metaFromNewsletter(n)
	if n.ViewerMeta == nil {
		// Without it the server didn't say whether you follow it.
		if old, ok := b.store.channelMeta(ctx, m.jid); ok {
			m.following, m.owner, m.muted = old.following, old.owner, old.muted
		}
	}
	m.rank = -1
	b.storeChannel(ctx, m)
}

// DiscoverChannels implements model.Backend.
func (b *Backend) DiscoverChannels() {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return
	}
	go func() {
		followed := map[string]bool{}
		for id := range b.store.channelMetas(b.ctx, true) {
			followed[id] = true
		}
		b.fetchSuggestedChannels(b.ctx, cli, followed, 50, 50)
	}()
}

// CreateChannel implements model.Backend.
func (b *Backend) CreateChannel(name, description string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.ChannelCreatedEvent{Err: "You're offline. Try again when you're connected."})
		return
	}
	go func() {
		ctx := b.ctx
		// Creating a channel takes accepting WhatsApp's channel terms,
		// which the phone may have done already.
		if err := cli.AcceptTOSNotice(ctx, "20601218", "5"); err != nil {
			b.log.Debugf("accept channel terms: %v", err)
		}
		n, err := cli.CreateNewsletter(ctx, whatsmeow.CreateNewsletterParams{Name: name, Description: description})
		if err == nil && n == nil {
			err = errors.New("no channel in the reply")
		}
		if err != nil {
			b.log.Warnf("create channel: %v", err)
			b.emit(model.ChannelCreatedEvent{Err: "Couldn't create the channel."})
			return
		}
		m := metaFromNewsletter(n)
		m.following, m.owner = true, true
		if m.name == "" {
			m.name = name
		}
		if m.description == "" {
			m.description = description
		}
		if m.created.IsZero() {
			m.created = b.now()
		}
		b.storeChannel(ctx, m)
		b.emit(model.ChannelsEvent{})
		b.emit(model.ChannelCreatedEvent{ID: m.jid})
		// The server fills in its link and the rest a moment later.
		time.Sleep(2 * time.Second)
		b.refreshChannel(ctx, cli, n.ID)
		b.emit(model.ChannelsEvent{})
	}()
}

// DeleteChannel implements model.Backend.
func (b *Backend) DeleteChannel(id string) {
	b.channelJob(id, "delete the channel", func(ctx context.Context, cli *whatsmeow.Client, jid types.JID) error {
		if err := cli.DeleteNewsletter(ctx, jid); err != nil {
			return err
		}
		_, err := b.db.ExecContext(ctx, `DELETE FROM wz_channels WHERE jid = ?1;
			DELETE FROM wz_channel_posts WHERE chat = ?1;
			DELETE FROM wz_messages WHERE chat = ?1;
			DELETE FROM wz_chats WHERE jid = ?1`, id)
		return err
	})
}

// onChannelEvent handles the server's notes that you followed, left or
// muted a channel (on another device, say).
func (b *Backend) onChannelEvent(evt any) {
	ctx := b.ctx
	switch e := evt.(type) {
	case *events.NewsletterJoin:
		m := metaFromNewsletter(&e.NewsletterMetadata)
		m.following, m.rank = true, 0
		b.storeChannel(ctx, m)
		if cli := b.client(); cli != nil {
			b.fetchChannelPosts(ctx, cli, e.ID, 0)
		}
	case *events.NewsletterLeave:
		_, _ = b.db.ExecContext(ctx, `UPDATE wz_channels SET following = 0 WHERE jid = ?`, e.ID.String())
	case *events.NewsletterMuteChange:
		_, _ = b.db.ExecContext(ctx, `UPDATE wz_channels SET muted = ? WHERE jid = ?`,
			boolInt(e.Mute == types.NewsletterMuteOn), e.ID.String())
	case *events.NewsletterLiveUpdate:
		b.onChannelUpdate(e)
		return
	default:
		return
	}
	b.emit(model.ChannelsEvent{})
}
