package mock

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Schedule implements model.Backend: the message joins its chat on the
// first Poll after its time.
func (b *Backend) Schedule(chatID string, d model.Draft, shown string, at time.Time) error {
	_, err := b.schedule(chatID, d, shown, at)
	return err
}

// ScheduleFile implements model.Backend: the file is sent at its time.
func (b *Backend) ScheduleFile(chatID string, a model.Attachment, d model.Draft, shown string, at time.Time) error {
	s, err := b.schedule(chatID, d, shown, at)
	if err != nil {
		return err
	}
	s.Media, s.Draft.ViewOnce = a.Media, a.ViewOnce
	if a.Media == model.MediaDocument {
		s.FileName = filepath.Base(a.Path)
	}
	if b.schedFiles == nil {
		b.schedFiles = map[string]model.Attachment{}
	}
	a.Album = ""
	b.schedFiles[s.ID] = a
	return nil
}

func (b *Backend) schedule(chatID string, d model.Draft, shown string, at time.Time) (*model.Scheduled, error) {
	now := b.now()
	at = at.Truncate(time.Minute)
	if at.Sub(now.Truncate(time.Minute)) < model.ScheduleMin || at.Sub(now) > model.ScheduleMax {
		return nil, errors.New("pick a time from 10 minutes to 14 days from now")
	}
	if strings.HasSuffix(chatID, "@newsletter") {
		return nil, errors.New("channels and broadcasts can't take scheduled messages")
	}
	if len(b.Scheduled(chatID)) >= model.ScheduleMaxPerChat {
		return nil, fmt.Errorf("WhatsApp holds up to %d scheduled messages per chat", model.ScheduleMaxPerChat)
	}
	b.schedSeq++
	if d.Reply != nil {
		d.Reply = &model.Message{ID: d.Reply.ID, ChatID: d.Reply.ChatID}
	}
	s := &model.Scheduled{ID: fmt.Sprintf("sched-%d", b.schedSeq), ChatID: chatID, At: at, Draft: d, Shown: shown}
	i, _ := slices.BinarySearchFunc(b.scheduled, at, func(s *model.Scheduled, at time.Time) int {
		if s.At.After(at) {
			return 1
		}
		return -1
	})
	b.scheduled = slices.Insert(b.scheduled, i, s)
	b.emit(model.ScheduledEvent{ChatID: chatID})
	return s, nil
}

// Scheduled implements model.Backend.
func (b *Backend) Scheduled(chatID string) []*model.Scheduled {
	var out []*model.Scheduled
	for _, s := range b.scheduled {
		if chatID == "" || s.ChatID == chatID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out
}

// Unschedule implements model.Backend.
func (b *Backend) Unschedule(chatID, id string) bool {
	i := slices.IndexFunc(b.scheduled, func(s *model.Scheduled) bool { return s.ChatID == chatID && s.ID == id })
	if i < 0 {
		return false
	}
	b.scheduled = slices.Delete(b.scheduled, i, i+1)
	delete(b.schedFiles, id)
	b.emit(model.ScheduledEvent{ChatID: chatID})
	b.emit(model.NoticeEvent{Text: "Scheduled message cancelled"})
	return true
}

// revealDue sends the scheduled messages whose time came.
func (b *Backend) revealDue() {
	now := b.now()
	for len(b.scheduled) > 0 && !b.scheduled[0].At.After(now) {
		s := b.scheduled[0]
		b.scheduled = b.scheduled[1:]
		d := s.Draft
		if d.Reply != nil {
			for _, m := range b.msgs[s.ChatID] {
				if m.ID == d.Reply.ID {
					d.Reply = m
				}
			}
		}
		var m *model.Message
		if a, ok := b.schedFiles[s.ID]; ok {
			delete(b.schedFiles, s.ID)
			m = b.SendFile(s.ChatID, a, d)
		} else {
			m = b.Send(s.ChatID, d)
		}
		if m == nil {
			continue
		}
		m.Time = s.At
		b.emit(model.MessageEvent{Msg: m})
		b.emit(model.ScheduledEvent{ChatID: s.ChatID})
	}
}
