package ui

import (
	"encoding/json"
	"strings"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// memberColors is the color of each member of a chat, 1 to 17 (an index in
// Palette.Members, plus one), like WhatsApp Web's WAWebChatParticipantColor:
// a chat gives each new author the first color nobody in it has, and once
// all 17 are taken the one fewest have (the lowest of those). So the same
// person has a color of their own in each group. Like WhatsApp's, the colors
// are this device's: they're given as names are drawn and kept in the
// account's prefs (prefMemberColors), so they don't change once given.
//
// WhatsApp Web uses these unless its wa_web_default_profile_pics A/B flag is
// on; then a name takes the last digit of the person's LID (personTint).
type memberColors struct {
	chats map[string]map[string]int // chat ID, member's key: color
	dirty map[string]bool           // chats to save at the end of the frame
}

// prefMemberColors is the pref key prefix of a chat's member colors (a JSON
// object of member keys and colors).
const prefMemberColors = "member_colors:"

// memberPalette is how many colors memberColors gives.
const memberPalette = 17

// get returns the color of the member key in chat, giving them one if they
// have none yet. b, which may be nil, loads the chat's colors.
func (mc *memberColors) get(b model.Backend, chat, key string) int {
	if mc.chats == nil {
		mc.chats = map[string]map[string]int{}
	}
	colors, ok := mc.chats[chat]
	if !ok {
		colors = map[string]int{}
		if b != nil && chat != "" {
			if s := b.Pref(prefMemberColors + chat); s != "" {
				_ = json.Unmarshal([]byte(s), &colors)
			}
		}
		mc.chats[chat] = colors
	}
	if c := colors[key]; c >= 1 && c <= memberPalette {
		return c
	}
	c := nextMemberColor(colors)
	colors[key] = c
	if chat != "" {
		if mc.dirty == nil {
			mc.dirty = map[string]bool{}
		}
		mc.dirty[chat] = true
	}
	return c
}

// nextMemberColor is the color a chat gives its next new member: the first
// that nobody has or, with all taken, the one fewest have.
func nextMemberColor(colors map[string]int) int {
	var count [memberPalette + 1]int
	for _, c := range colors {
		if c >= 1 && c <= memberPalette {
			count[c]++
		}
	}
	best := 1
	for c := 1; c <= memberPalette; c++ {
		if count[c] == 0 {
			return c
		}
		if count[c] < count[best] {
			best = c
		}
	}
	return best
}

// endFrame saves the colors given in this frame, once a chat.
func (mc *memberColors) endFrame(b model.Backend) {
	if len(mc.dirty) == 0 || b == nil {
		return
	}
	for chat := range mc.dirty {
		if s, err := json.Marshal(mc.chats[chat]); err == nil {
			b.SetPref(prefMemberColors+chat, string(s))
		}
	}
	clear(mc.dirty)
}

// memberKey is who a color belongs to: the user of the ID, without its
// device (WhatsApp Web keys them by the LID, which ids here are when one is
// known), or the name for someone without an ID (demo data).
func memberKey(id, name string) string {
	if id == "" {
		return "~" + name
	}
	if at := strings.IndexByte(id, '@'); at >= 0 {
		if colon := strings.IndexByte(id[:at], ':'); colon >= 0 {
			id = id[:colon] + id[at:]
		}
	}
	return id
}
