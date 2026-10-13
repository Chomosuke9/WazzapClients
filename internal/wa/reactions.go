package wa

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Reactions: each person's latest reaction to a message, kept in
// wz_reactions. The message's reaction column holds a summary of them
// (reactionSummary), so reading a page of messages needs no second query.

const reactionsSchema = `
CREATE TABLE IF NOT EXISTS wz_reactions (
	chat  TEXT NOT NULL,
	id    TEXT NOT NULL,            -- the message reacted to
	who   TEXT NOT NULL,            -- usually a LID; meVoter for you, oldReactor for one from before
	ts    INTEGER NOT NULL,         -- unix milliseconds
	emoji TEXT NOT NULL DEFAULT '', -- '' once taken back
	PRIMARY KEY (chat, id, who)
) WITHOUT ROWID;
`

// oldReactor is who the one reaction a message kept before wz_reactions
// existed is stored under: nobody knows who gave it. It counts on the
// pill, but Reactors leaves it out.
const oldReactor = "old"

// keepOldReactions copies the reactions kept before wz_reactions existed,
// one per message in its reaction column, into it once, so the next
// reaction to the message doesn't drop them from its summary.
func (s *msgStore) keepOldReactions(ctx context.Context) error {
	const done = "old_reactions_kept"
	if s.meta(ctx, done) != "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO wz_reactions (chat, id, who, ts, emoji)
		SELECT chat, id, ?, ts * 1000, reaction FROM wz_messages
		WHERE reaction != '' AND instr(reaction, char(10)) = 0`, oldReactor)
	if err != nil {
		return err
	}
	return s.setMetaValue(ctx, done, "1")
}

// reaction is one person's reaction, to p.target or a history message.
type reaction struct {
	who   string
	ts    int64 // unix milliseconds
	emoji string
}

// historyReaction is a reaction that history sync listed on message id.
type historyReaction struct {
	id string
	reaction
}

// querier is a *sql.DB or a *sql.Tx.
type querier interface {
	execer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// putReaction records r unless a newer one from the same person is
// stored, and updates the message's summary. A removed reaction stays as
// a row with no emoji, so an older one coming late doesn't bring it back.
//
// On the database itself it runs in a transaction of its own: your own
// reactions are stored from the UI goroutine and others' from the event
// goroutine, and one summary must not miss the other's row.
func (s *msgStore) putReaction(ctx context.Context, x querier, chat, id string, r reaction) error {
	if db, ok := x.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := s.putReaction(ctx, tx, chat, id, r); err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err := x.ExecContext(ctx, `INSERT INTO wz_reactions (chat, id, who, ts, emoji) VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT (chat, id, who) DO UPDATE SET ts = excluded.ts, emoji = excluded.emoji
		WHERE excluded.ts >= wz_reactions.ts`, chat, id, r.who, r.ts, r.emoji)
	if err != nil {
		return err
	}
	rs, err := s.reactions(ctx, x, chat, id)
	if err != nil {
		return err
	}
	_, err = x.ExecContext(ctx, `UPDATE wz_messages SET reaction = ? WHERE chat = ? AND id = ?`,
		reactionSummary(rs), chat, id)
	return err
}

// reactions returns a message's reactions, oldest first.
func (s *msgStore) reactions(ctx context.Context, x querier, chat, id string) ([]reaction, error) {
	rows, err := x.QueryContext(ctx, `SELECT who, ts, emoji FROM wz_reactions
		WHERE chat = ? AND id = ? AND emoji != '' ORDER BY ts`, chat, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reaction
	for rows.Next() {
		var r reaction
		if err := rows.Scan(&r.who, &r.ts, &r.emoji); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// reactionSummary is what the reaction column keeps of rs (oldest
// first): your emoji on the first line, then one line per emoji with its
// count, tab separated, the most given first (ties: the one given first).
// It is empty when nobody reacted.
func reactionSummary(rs []reaction) string {
	if len(rs) == 0 {
		return ""
	}
	var mine string
	var counts []model.ReactionCount
	for _, r := range rs {
		if r.who == meVoter {
			mine = r.emoji
		}
		i := slices.IndexFunc(counts, func(c model.ReactionCount) bool { return c.Emoji == r.emoji })
		if i < 0 {
			counts = append(counts, model.ReactionCount{Emoji: r.emoji})
			i = len(counts) - 1
		}
		counts[i].Count++
	}
	slices.SortStableFunc(counts, func(a, b model.ReactionCount) int { return b.Count - a.Count })
	var sb strings.Builder
	sb.WriteString(mine)
	for _, c := range counts {
		sb.WriteString("\n" + c.Emoji + "\t" + strconv.Itoa(c.Count))
	}
	return sb.String()
}

// fillReactions puts what a reaction column holds on m. A column written
// before wz_reactions existed holds the one latest emoji, from anyone.
func fillReactions(m *model.Message, s string) {
	m.Reactions, m.MyReaction = nil, ""
	if s == "" {
		return
	}
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		m.Reactions = []model.ReactionCount{{Emoji: s, Count: 1}}
		return
	}
	m.MyReaction = lines[0]
	for _, l := range lines[1:] {
		e, n, ok := strings.Cut(l, "\t")
		if c, err := strconv.Atoi(n); ok && err == nil && e != "" && c > 0 {
			m.Reactions = append(m.Reactions, model.ReactionCount{Emoji: e, Count: c})
		}
	}
}

// reactorOf is who a reaction from j (fromMe if it's yours) is stored
// under.
func (b *Backend) reactorOf(ctx context.Context, j types.JID, fromMe bool) string {
	if fromMe {
		return meVoter
	}
	return b.voterOf(ctx, j)
}

// Reactors implements model.Backend.
func (b *Backend) Reactors(m *model.Message) []model.Reactor {
	ctx := b.ctx
	rs, err := b.store.reactions(ctx, b.db, m.ChatID, m.ID)
	if err != nil {
		return nil
	}
	out := make([]model.Reactor, 0, len(rs))
	for _, r := range rs {
		if r.who == oldReactor {
			continue
		}
		rr := model.Reactor{ID: r.who, Me: r.who == meVoter, Emoji: r.emoji, Time: time.UnixMilli(r.ts)}
		if rr.Me {
			rr.ID, rr.Name = b.ownJID(m.ChatID).String(), "You"
		} else {
			rr.Name = b.senderNameStr(ctx, r.who, "")
		}
		out = append(out, rr)
	}
	sortReactors(out)
	return out
}

// sortReactors puts you first, then the newest first.
func sortReactors(rs []model.Reactor) {
	slices.SortStableFunc(rs, func(a, b model.Reactor) int {
		if a.Me != b.Me {
			if a.Me {
				return -1
			}
			return 1
		}
		return b.Time.Compare(a.Time)
	})
}
