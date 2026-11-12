package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestFollowers(t *testing.T) {
	tests := map[int]string{
		0:         "Channel",
		1:         "1 follower",
		950:       "950 followers",
		1500:      "1.5k followers",
		17400:     "17k followers",
		736100:    "736k followers",
		1_450_000: "1.4m followers",
		7_000_000: "7m followers",
	}
	for n, want := range tests {
		if got := followers(n); got != want {
			t.Errorf("followers(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestChannelActions follows a suggested channel from its preview, mutes,
// unfollows and creates one, and draws their pages and info panels.
func TestChannelActions(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.setPage(pageChannels)
	st.frame()
	labels := func(ch *model.Channel, open bool) []string {
		var out []string
		for _, it := range u.channelMenuItems(ch, open) {
			if !it.divider && it.run == nil {
				t.Errorf("%s: %q does nothing", ch.ID, it.label)
			}
			out = append(out, it.label)
		}
		return out
	}

	// A suggestion opens to show its posts, with Follow under them.
	bola := u.channelByID("bola@newsletter")
	if bola == nil || bola.Following {
		t.Fatalf("bola = %+v, want a suggestion", bola)
	}
	u.open(channelChat(bola))
	st.frame()
	if len(u.msgs) == 0 {
		t.Errorf("a suggested channel's preview shows no posts")
	}
	if u.channelFooter(u.selected) == nil {
		t.Errorf("a suggested channel shows the composer")
	}
	if got := labels(bola, true); !slices.Contains(got, "Follow channel") {
		t.Errorf("suggestion's menu %q", got)
	}
	u.backend.FollowChannel(bola.ID)
	u.applyEvents()
	st.frame()
	if ch := u.followedChannel(bola.ID); ch == nil || !ch.Following {
		t.Fatalf("bola isn't followed: %+v", ch)
	}

	// A followed channel: no composer, the menu's actions.
	tech := u.followedChannel("techdaily@newsletter")
	u.openChannel(tech.ID)
	st.frame()
	if f := u.channelFooter(u.selected); f == nil {
		t.Errorf("a followed channel shows the composer")
	}
	got := labels(tech, true)
	for _, want := range []string{"Channel info", "Mute notifications", "Copy link", "Unfollow channel", "Close channel"} {
		if !slices.Contains(got, want) {
			t.Errorf("channel menu %q lacks %q", got, want)
		}
	}
	if slices.Contains(got, "Delete channel") {
		t.Errorf("a channel you don't own offers Delete: %q", got)
	}
	u.backend.MuteChannel(tech.ID, true)
	u.applyEvents()
	if ch := u.followedChannel(tech.ID); ch == nil || !ch.Muted {
		t.Errorf("mute didn't take: %+v", ch)
	}
	if !canReact(tech.ID, u.msgs[len(u.msgs)-1]) {
		t.Errorf("a channel's update can't be reacted to")
	}
	u.openInfo(tech.ID)
	st.frame()
	st.frame()
	if !u.info.open || u.info.chatID != tech.ID {
		t.Errorf("channel info didn't open")
	}

	// Your own: the composer, and Delete.
	mine := u.followedChannel("wazzapnews@newsletter")
	u.openChannel(mine.ID)
	st.frame()
	if u.channelFooter(u.selected) != nil {
		t.Errorf("your own channel has no composer")
	}
	if got := labels(mine, true); !slices.Contains(got, "Delete channel") || slices.Contains(got, "Unfollow channel") {
		t.Errorf("own channel's menu %q", got)
	}

	// Creating one opens it.
	u.openCreateChannel()
	st.frame()
	u.backend.CreateChannel("Morning notes", "Short daily notes")
	u.applyEvents()
	st.frame()
	if u.selected == nil || !u.ownsChannel(u.selected.ID) || u.selected.Name != "Morning notes" {
		t.Errorf("the new channel didn't open: %+v", u.selected)
	}

	// Unfollowing takes it off the list.
	u.backend.UnfollowChannel(tech.ID)
	u.applyEvents()
	st.frame()
	if u.followedChannel(tech.ID) != nil {
		t.Errorf("tech is still followed")
	}

	// Discover more lists more suggestions.
	before := len(u.suggested)
	u.backend.DiscoverChannels()
	u.applyEvents()
	if len(u.suggested) <= before {
		t.Errorf("Discover more listed %d suggestions, had %d", len(u.suggested), before)
	}
}

// TestChannelsOffChatList checks that posting in your own channel doesn't
// put it in the chat list, and that a contact's security code changing
// neither moves their chat up nor becomes its preview.
func TestChannelsOffChatList(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.openChannel("wazzapnews@newsletter")
	st.frame()
	m := &model.Message{ID: "post", ChatID: "wazzapnews@newsletter", FromMe: true, Text: "hello", Time: testNow()}
	u.listSent(m)
	u.upsertMessage(m)
	u.upsertChat(&model.Chat{ID: "wazzapnews@newsletter", Name: "Wazzap Updates"})
	if u.chatByID("wazzapnews@newsletter") != nil {
		t.Errorf("your channel went into the chat list")
	}

	rina := u.chatByID("rina")
	last, at := rina.Last, rina.Time
	u.upsertMessage(&model.Message{ID: "sec", ChatID: "rina", Kind: model.KindSystem, Notice: model.NoticeSecurity,
		Text: "Your security code with Rina changed", Time: testNow().Add(time.Hour)})
	if rina.Last != last || !rina.Time.Equal(at) {
		t.Errorf("a security code change became Rina's latest activity: last %+v at %v", rina.Last, rina.Time)
	}
}
