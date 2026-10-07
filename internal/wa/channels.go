package wa

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// channelMeta is a channel's row in wz_channels.
type channelMeta struct {
	jid, name, picture string
	verified           bool
	followers          int
	following, owner   bool
	muted              bool
	created            time.Time
	rank               int
}

func metaFromNewsletter(n *types.NewsletterMetadata) channelMeta {
	tm := n.ThreadMeta
	m := channelMeta{
		jid:       n.ID.String(),
		name:      tm.Name.Text,
		verified:  tm.VerificationState == types.NewsletterVerificationStateVerified,
		followers: tm.SubscriberCount,
		created:   tm.CreationTime.Time,
		picture:   pictureURL(&tm.Preview),
	}
	if m.picture == "" {
		m.picture = pictureURL(tm.Picture)
	}
	if vm := n.ViewerMeta; vm != nil {
		m.following = vm.Role != "" && vm.Role != types.NewsletterRoleGuest
		m.owner = vm.Role == types.NewsletterRoleOwner || vm.Role == types.NewsletterRoleAdmin
		m.muted = vm.Mute == types.NewsletterMuteOn
	}
	return m
}

func pictureURL(p *types.ProfilePictureInfo) string {
	switch {
	case p == nil:
		return ""
	case p.URL != "":
		return p.URL
	case p.DirectPath != "":
		return "https://mmg.whatsapp.net" + p.DirectPath
	}
	return ""
}

func (s *msgStore) putChannel(ctx context.Context, m channelMeta) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO wz_channels (jid, name, verified, followers, following, owner, muted, created, picture, rank)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET name = excluded.name, verified = excluded.verified,
			followers = excluded.followers, following = excluded.following, owner = excluded.owner,
			muted = excluded.muted, created = excluded.created, rank = excluded.rank,
			picture = CASE WHEN excluded.picture <> '' THEN excluded.picture ELSE wz_channels.picture END`,
		m.jid, m.name, boolInt(m.verified), m.followers, boolInt(m.following), boolInt(m.owner), boolInt(m.muted),
		m.created.Unix(), m.picture, m.rank)
	return err
}

func (s *msgStore) channelPicture(ctx context.Context, jid string) string {
	var p string
	_ = s.db.QueryRowContext(ctx, `SELECT picture FROM wz_channels WHERE jid = ?`, jid).Scan(&p)
	return p
}

// Channels implements model.Backend.
func (b *Backend) Channels() []*model.Channel {
	ctx := b.ctx
	raw, err := b.store.queryChats(ctx, chatQuery+` JOIN wz_channels ch ON ch.jid = c.jid
		WHERE ch.following = 1`)
	if err != nil {
		b.log.Errorf("load channels: %v", err)
		return nil
	}
	metas := b.store.channelMetas(ctx, true)
	var out []*model.Channel
	for _, rc := range raw {
		c := rc.Chat
		m := metas[c.ID]
		ch := &model.Channel{
			ID: c.ID, Name: first(m.name, c.Name), Verified: m.verified, Followers: m.followers,
			Following: true, Muted: m.muted, Unread: c.Unread, Time: c.Time, Last: c.Last,
		}
		if ch.Last == nil && m.owner {
			// WhatsApp shows this system message until the first post.
			ch.Last = &model.Message{ChatID: c.ID, FromMe: true, Kind: model.KindText,
				Text: `You created this channel, "` + ch.Name + `"`, Time: m.created}
			ch.Time = m.created
		}
		if ch.Last != nil && ch.Last.Time.After(ch.Time) {
			ch.Time = ch.Last.Time
		}
		out = append(out, ch)
	}
	sortChannels(out)
	return out
}

func sortChannels(cs []*model.Channel) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Time.After(cs[j].Time) })
}

func (s *msgStore) channelMetas(ctx context.Context, following bool) map[string]channelMeta {
	rows, err := s.db.QueryContext(ctx, `SELECT jid, name, verified, followers, owner, muted, created, rank
		FROM wz_channels WHERE following = ?`, boolInt(following))
	out := map[string]channelMeta{}
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			m                      channelMeta
			verified, owner, muted int
			created                int64
		)
		if rows.Scan(&m.jid, &m.name, &verified, &m.followers, &owner, &muted, &created, &m.rank) != nil {
			continue
		}
		m.verified, m.owner, m.muted, m.following = verified != 0, owner != 0, muted != 0, following
		m.created = time.Unix(created, 0)
		out[m.jid] = m
	}
	return out
}

// SuggestedChannels implements model.Backend.
func (b *Backend) SuggestedChannels() []*model.Channel {
	metas := b.store.channelMetas(b.ctx, false)
	out := make([]*model.Channel, 0, len(metas))
	for _, m := range metas {
		if m.rank <= 0 {
			continue
		}
		out = append(out, &model.Channel{ID: m.jid, Name: m.name, Verified: m.verified, Followers: m.followers})
	}
	sort.Slice(out, func(i, j int) bool { return metas[out[i].ID].rank < metas[out[j].ID].rank })
	return out
}

// FollowChannel implements model.Backend.
func (b *Backend) FollowChannel(id string) {
	jid, err := types.ParseJID(id)
	cli := b.client()
	if err != nil || cli == nil || !cli.IsConnected() {
		return
	}
	go func() {
		if err := cli.FollowNewsletter(b.ctx, jid); err != nil {
			b.log.Warnf("follow channel %s: %v", id, err)
			return
		}
		b.refreshChannels()
	}()
}

// refreshChannels loads followed channels, their latest posts and
// suggestions. It runs once per connection.
func (b *Backend) refreshChannels() {
	ctx := b.ctx
	cli := b.client()
	list, err := cli.GetSubscribedNewsletters(ctx)
	if err != nil {
		b.log.Warnf("get followed channels: %v", err)
		return
	}
	followed := map[string]bool{}
	for _, n := range list {
		m := metaFromNewsletter(n)
		m.following = true
		followed[m.jid] = true
		if err := b.store.putChannel(ctx, m); err != nil {
			b.log.Warnf("store channel %s: %v", m.jid, err)
			continue
		}
		_ = b.store.ensureChat(ctx, b.db, m.jid, false, m.name)
		_ = b.store.setName(ctx, m.jid, m.name)
	}
	_, _ = b.db.ExecContext(ctx, `UPDATE wz_channels SET following = 0 WHERE following = 1 AND jid NOT IN (`+
		quoteList(keys(followed))+`)`)
	b.emit(model.ChannelsEvent{})

	for _, n := range list {
		b.fetchChannelPosts(ctx, cli, n.ID)
	}
	b.emit(model.ChannelsEvent{})
	b.fetchSuggestedChannels(ctx, cli, followed)
}

// fetchChannelPosts stores a channel's latest posts.
func (b *Backend) fetchChannelPosts(ctx context.Context, cli *whatsmeow.Client, jid types.JID) {
	msgs, err := cli.GetNewsletterMessages(ctx, jid, &whatsmeow.GetNewsletterMessagesParams{Count: 30})
	if err != nil {
		b.log.Debugf("channel posts %s: %v", jid, err)
		return
	}
	chat := jid.String()
	for _, nm := range msgs {
		if nm.Message == nil {
			continue
		}
		c := describe(nm.Message)
		if c.text == "" && c.media == model.MediaNone {
			continue
		}
		id := string(nm.MessageID)
		if id == "" {
			id = strconv.Itoa(int(nm.MessageServerID))
		}
		m := storedMsg{Message: &model.Message{
			ID: id, ChatID: chat, Kind: c.kind, Media: c.media, Text: c.text, Time: nm.Timestamp, Receipt: model.Read,
		}, senderJID: chat, rawPayload: marshal(nm.Message)}
		if err := b.store.putMessage(ctx, b.db, m); err != nil {
			b.log.Debugf("store channel post: %v", err)
			continue
		}
		b.emitMessage(chat, id)
	}
}

// Recommended-channels query of the WhatsApp Web client. hypermeow knows
// the ID but has no wrapper, so the reply is parsed loosely.
const queryRecommendedChannels = "7263823273662354"

func (b *Backend) fetchSuggestedChannels(ctx context.Context, cli *whatsmeow.Client, followed map[string]bool) {
	input := map[string]any{"limit": 20}
	if cc := countryCode(cli); cc != "" {
		input["country_codes"] = []string{cc}
	}
	data, err := cli.DangerousInternals().SendMexIQ(ctx, queryRecommendedChannels, map[string]any{"input": input})
	if err != nil {
		b.log.Infof("suggested channels: %v", err)
		return
	}
	var found []*types.NewsletterMetadata
	collectNewsletters(data, &found)
	b.log.Infof("suggested channels: %d found", len(found))
	if len(found) == 0 {
		return
	}
	_, _ = b.db.ExecContext(ctx, `UPDATE wz_channels SET rank = 0 WHERE following = 0`)
	rank := 0
	for _, n := range found {
		m := metaFromNewsletter(n)
		if followed[m.jid] || m.name == "" {
			continue
		}
		rank++
		m.following, m.rank = false, rank
		_ = b.store.putChannel(ctx, m)
		if rank == 5 {
			break
		}
	}
	b.emit(model.ChannelsEvent{})
}

// collectNewsletters finds channel objects anywhere in a GraphQL reply.
func collectNewsletters(data json.RawMessage, out *[]*types.NewsletterMetadata) {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if id, ok := x["id"].(string); ok && strings.HasSuffix(id, "@"+types.NewsletterServer) {
				if _, ok := x["thread_metadata"]; ok {
					raw, _ := json.Marshal(x)
					var n types.NewsletterMetadata
					if json.Unmarshal(raw, &n) == nil {
						*out = append(*out, &n)
					}
					return
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
}

// callingCodes maps phone number prefixes to ISO country codes, for
// channel suggestions. Longest prefixes are tried first.
var callingCodes = map[string]string{
	"1": "US", "7": "RU", "20": "EG", "27": "ZA", "30": "GR", "31": "NL", "32": "BE", "33": "FR", "34": "ES",
	"36": "HU", "39": "IT", "40": "RO", "41": "CH", "43": "AT", "44": "GB", "45": "DK", "46": "SE", "47": "NO",
	"48": "PL", "49": "DE", "51": "PE", "52": "MX", "54": "AR", "55": "BR", "56": "CL", "57": "CO", "58": "VE",
	"60": "MY", "61": "AU", "62": "ID", "63": "PH", "64": "NZ", "65": "SG", "66": "TH", "81": "JP", "82": "KR",
	"84": "VN", "86": "CN", "90": "TR", "91": "IN", "92": "PK", "93": "AF", "94": "LK", "95": "MM", "98": "IR",
	"212": "MA", "213": "DZ", "234": "NG", "254": "KE", "351": "PT", "353": "IE", "380": "UA", "852": "HK",
	"880": "BD", "886": "TW", "966": "SA", "971": "AE", "972": "IL", "974": "QA",
}

func countryCode(cli *whatsmeow.Client) string {
	if cli.Store.ID == nil {
		return ""
	}
	num := cli.Store.ID.User
	for n := 3; n >= 1; n-- {
		if len(num) > n {
			if cc, ok := callingCodes[num[:n]]; ok {
				return cc
			}
		}
	}
	return ""
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// quoteList renders strings as an SQL list literal. Only used with JIDs,
// which never contain quotes, but escape anyway.
func quoteList(ss []string) string {
	if len(ss) == 0 {
		return "''"
	}
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return strings.Join(q, ",")
}
