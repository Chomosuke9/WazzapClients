package mock

import "github.com/chomosuke9/wazzapclients/internal/model"

// SetSharedTheme sets chatID's theme for everyone, as someone in it would.
func (b *Backend) SetSharedTheme(chatID string, t model.SharedTheme) {
	if b.sharedThemes == nil {
		b.sharedThemes = map[string]*model.SharedTheme{}
	}
	b.sharedThemes[chatID] = &t
	b.emit(model.ChatThemeEvent{ChatID: chatID})
}

// SharedTheme implements model.Backend.
func (b *Backend) SharedTheme(chatID string) *model.SharedTheme { return b.sharedThemes[chatID] }

// DropSharedTheme implements model.Backend.
func (b *Backend) DropSharedTheme(chatID string) { delete(b.sharedThemes, chatID) }

// ShareTheme implements model.Backend: nobody else is there to see it.
func (b *Backend) ShareTheme(chatID string, t model.SharedTheme) {
	text := "Theme set for everyone"
	if t.Reset {
		text = "Theme reset for everyone"
	}
	b.SharedThemesSent = append(b.SharedThemesSent, t)
	b.emit(model.NoticeEvent{Text: text})
}
