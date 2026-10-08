package mock

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Demo snippets saved from a message keep that message (snippetSrc), as
// the wa backend keeps its protobuf, and send a copy of it. Their Body is
// a payload in the protocol's JSON shape (messageJSON), for show.

func (b *Backend) Snippets() ([]model.Snippet, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.Snippet
	for _, s := range b.snippets {
		s.Body = ""
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (b *Backend) Snippet(id int64) (model.Snippet, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.snippets[id]; ok {
		return s, nil
	}
	return model.Snippet{}, errors.New("This snippet no longer exists.")
}

func (b *Backend) SaveSnippet(s model.Snippet) (model.Snippet, error) {
	return b.saveSnippet(s, nil)
}

func (b *Backend) saveSnippet(s model.Snippet, src *model.Message) (model.Snippet, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(s.Body) > model.MaxSnippetBytes || strings.TrimSpace(s.Body) == "" {
		return s, errors.New("Enter a message of at most 1 MB.")
	}
	if s.Payload && !json.Valid([]byte(s.Body)) {
		return s, errors.New("Invalid message JSON.")
	}
	if b.snippets == nil {
		b.snippets = map[int64]model.Snippet{}
		b.snippetSrc = map[int64]*model.Message{}
	}
	if s.ID == 0 {
		b.snippetSeq++
		s.ID = b.snippetSeq
	} else if old, ok := b.snippets[s.ID]; !ok {
		return s, errors.New("This snippet no longer exists.")
	} else if old.Body != s.Body || old.Payload != s.Payload {
		delete(b.snippetSrc, s.ID) // edited: send what it says now
	}
	if s.Name == "" {
		s.Name = fmt.Sprintf("Snippet %d", s.ID)
	}
	for _, other := range b.snippets {
		if other.ID != s.ID && strings.EqualFold(other.Name, s.Name) {
			return s, errors.New("A snippet with that name already exists.")
		}
	}
	text := s.Body
	if src != nil {
		cp := *src
		b.snippetSrc[s.ID] = &cp
		text = src.Text
		if text == "" {
			text = mediaNames[src.Media]
		}
	}
	s.Preview = strings.Join(strings.Fields(text), " ")
	if r := []rune(s.Preview); len(r) > 100 {
		s.Preview = string(r[:100]) + "…"
	}
	b.snippets[s.ID] = s
	return s, nil
}

// mediaNames preview a snippet of a message without text.
var mediaNames = map[model.Media]string{model.MediaImage: "Photo", model.MediaVideo: "Video", model.MediaGIF: "GIF",
	model.MediaVoice: "Voice message", model.MediaAudio: "Audio", model.MediaDocument: "Document",
	model.MediaSticker: "Sticker", model.MediaLocation: "Location", model.MediaContact: "Contact",
	model.MediaPoll: "Poll", model.MediaEventInvite: "Event"}

// plainText reports whether m is only text, which saves as a text
// snippet.
func plainText(m *model.Message) bool {
	return m.Media == model.MediaNone && m.Poll == nil && m.Location == nil && len(m.Contacts) == 0 &&
		m.Event == nil && m.Link == nil && len(m.Buttons) == 0 && m.FileName == ""
}

func (b *Backend) message(chat, id string) *model.Message {
	for _, m := range b.Messages(chat, 10000) {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (b *Backend) SaveMessageSnippet(chat, id, name string) (model.Snippet, error) {
	m := b.message(chat, id)
	if m == nil {
		return model.Snippet{}, errors.New("This message is no longer stored.")
	}
	if m.Kind == model.KindDeleted {
		return model.Snippet{}, errors.New("This message was deleted.")
	}
	if m.Kind == model.KindViewOnce {
		return model.Snippet{}, errors.New("View once messages can't be saved as snippets.")
	}
	if plainText(m) {
		return b.SaveSnippet(model.Snippet{Name: name, Body: m.Text})
	}
	body, err := messageJSON(m)
	if err != nil {
		return model.Snippet{}, err
	}
	return b.saveSnippet(model.Snippet{Name: name, Payload: true, Body: body}, m)
}

func (b *Backend) DeleteSnippet(id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.snippets, id)
	delete(b.snippetSrc, id)
	return nil
}

func (b *Backend) SendSnippet(chat string, id int64, reply *model.Message, vars map[string]string) (*model.Message, error) {
	s, err := b.Snippet(id)
	if err != nil {
		return nil, err
	}
	// {mention} is plain text here: "@" and the replied author's name, or
	// the chat's.
	vars = maps.Clone(vars)
	if vars == nil {
		vars = map[string]string{}
	}
	who := vars["chat"]
	if reply != nil && reply.Sender != "" {
		who = reply.Sender
	} else if reply != nil && reply.FromMe {
		who = b.meName
	}
	vars["mention"] = "@" + who
	b.mu.Lock()
	src := b.snippetSrc[id]
	b.mu.Unlock()
	if src != nil {
		// A copy of the whole message: its poll, file or card too.
		m := *src
		m.Text = model.ExpandSnippet(m.Text, vars)
		m.ChatID, m.FromMe, m.Time, m.Receipt = chat, true, b.now(), model.Sent
		m.Sender, m.SenderID, m.Reactions, m.MyReaction, m.Starred, m.Pinned, m.Forwarded = "", "", nil, "", false, false, false
		m.Quote, m.Edited, m.Revoked = quoteOf(reply), time.Time{}, time.Time{}
		if src.Poll != nil {
			p := *src.Poll
			p.Voters, p.Options = 0, make([]model.PollOption, len(src.Poll.Options))
			for i, o := range src.Poll.Options {
				p.Options[i] = model.PollOption{Name: o.Name}
			}
			m.Poll = &p
		}
		b.add(&m)
		cp := m
		b.emit(model.MessageEvent{Msg: &cp})
		return &cp, nil
	}
	body := s.Body
	if s.Payload {
		var m struct {
			Conversation        string `json:"conversation"`
			ExtendedTextMessage struct {
				Text string `json:"text"`
			} `json:"extendedTextMessage"`
		}
		if err = json.Unmarshal([]byte(body), &m); err != nil {
			return nil, err
		}
		body = m.Conversation
		if body == "" {
			body = m.ExtendedTextMessage.Text
		}
		if body == "" {
			body = "Message payload"
		}
	}
	return b.Send(chat, model.Draft{Text: model.ExpandSnippet(body, vars), Reply: reply}), nil
}

// messageJSON writes a demo message the way the protocol's JSON would
// carry it; demo messages have no real payload.
func messageJSON(m *model.Message) (string, error) {
	var p map[string]any
	media := func(kind string) map[string]any {
		c := map[string]any{}
		if m.Text != "" {
			c["caption"] = m.Text
		}
		if m.FileType != "" {
			c["mimetype"] = m.FileType
		}
		if m.Duration > 0 {
			c["seconds"] = m.Duration
		}
		p = map[string]any{kind: c}
		return c
	}
	switch {
	case m.Poll != nil:
		opts := make([]map[string]string, len(m.Poll.Options))
		for i, o := range m.Poll.Options {
			opts[i] = map[string]string{"optionName": o.Name}
		}
		p = map[string]any{"pollCreationMessageV3": map[string]any{"name": m.Text, "options": opts,
			"selectableOptionsCount": m.Poll.Max}}
	case m.Event != nil:
		p = map[string]any{"eventMessage": map[string]any{"name": m.Event.Name,
			"description": m.Event.Description, "startTime": m.Event.Start.Unix()}}
	case m.Location != nil:
		p = map[string]any{"locationMessage": map[string]any{"degreesLatitude": m.Location.Lat,
			"degreesLongitude": m.Location.Lng, "name": m.Location.Name, "address": m.Location.Address}}
	case len(m.Contacts) > 0:
		cards := make([]map[string]string, len(m.Contacts))
		for i, c := range m.Contacts {
			vcard := "BEGIN:VCARD\nVERSION:3.0\nFN:" + c.Name + "\n"
			for _, ph := range c.Phones {
				vcard += "TEL:" + ph.Number + "\n"
			}
			cards[i] = map[string]string{"displayName": c.Name, "vcard": vcard + "END:VCARD"}
		}
		p = map[string]any{"contactsArrayMessage": map[string]any{"contacts": cards}}
	case m.Media == model.MediaDocument || m.FileName != "":
		c := media("documentMessage")
		c["fileName"], c["fileLength"] = m.FileName, m.FileSize
		if m.Pages > 0 {
			c["pageCount"] = m.Pages
		}
		if m.Text == m.FileName {
			delete(c, "caption")
		}
	case m.Media == model.MediaImage:
		media("imageMessage")
	case m.Media == model.MediaVideo, m.Media == model.MediaGIF:
		c := media("videoMessage")
		if m.Media == model.MediaGIF {
			c["gifPlayback"] = true
		}
	case m.Media == model.MediaVoice, m.Media == model.MediaAudio:
		c := media("audioMessage")
		c["ptt"] = m.Media == model.MediaVoice
	case m.Media == model.MediaSticker:
		media("stickerMessage")
	case m.Link != nil:
		p = map[string]any{"extendedTextMessage": map[string]any{"text": m.Text, "title": m.Link.Title,
			"description": m.Link.Description}}
	default:
		p = map[string]any{"conversation": m.Text}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	return string(data), err
}

// Demo messages have synthetic payloads; real captures use the wa backend.
func (b *Backend) MessagePayload(chat, id string) (string, error) {
	if m := b.message(chat, id); m != nil {
		return messageJSON(m)
	}
	return "", errors.New("This message is no longer stored.")
}

func (b *Backend) ExportMessagePayload(chat, id string) (string, error) {
	return "", errors.New("File export is unavailable in demo mode. Use Copy JSON.")
}
