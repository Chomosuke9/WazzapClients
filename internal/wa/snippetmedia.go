package wa

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// prepareSnippetMessage changes a private copy only. A saved message is
// content to send anew, never a protocol command targeting an old message.
func prepareSnippetMessage(source *waE2E.Message) (*waE2E.Message, error) {
	if isViewOnce(source) {
		return nil, errors.New("View once messages can't be saved as snippets.")
	}
	msg := proto.Clone(unwrap(source)).(*waE2E.Message)
	allowed := map[protoreflect.Name]bool{
		"conversation": true, "extendedTextMessage": true, "imageMessage": true, "videoMessage": true,
		"audioMessage": true, "documentMessage": true, "stickerMessage": true, "ptvMessage": true,
		"contactMessage": true, "contactsArrayMessage": true, "locationMessage": true, "liveLocationMessage": true,
		"pollCreationMessage": true, "pollCreationMessageV2": true, "pollCreationMessageV3": true,
		"eventMessage": true, "buttonsMessage": true, "templateMessage": true, "interactiveMessage": true, "listMessage": true,
	}
	count, bad := 0, false
	msg.ProtoReflect().Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Name() == "messageContextInfo" {
			return true
		}
		if allowed[f.Name()] {
			count++
		} else {
			bad = true
		}
		return true
	})
	if bad || count != 1 {
		return nil, errors.New("Use a payload with one message: text, media, a card, poll, or interactive content. Protocol actions aren't snippets.")
	}
	msg.MessageContextInfo = nil
	if msg.PollCreationMessage != nil || msg.PollCreationMessageV2 != nil || msg.PollCreationMessageV3 != nil || msg.EventMessage != nil {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		msg.MessageContextInfo = &waE2E.MessageContextInfo{MessageSecret: secret}
	}
	walkSnippet(msg.ProtoReflect(), func(m protoreflect.Message) {
		if ci, ok := m.Interface().(*waE2E.ContextInfo); ok {
			ci.StanzaID, ci.Participant, ci.RemoteJID = nil, nil, nil
			ci.QuotedMessage = nil
			ci.Expiration, ci.EphemeralSettingTimestamp, ci.EphemeralSharedSecret = nil, nil, nil
			ci.DisappearingMode = nil
		}
	})
	return msg, nil
}

// walkSnippet avoids quoted content and keeps traversal bounded by the
// protobuf JSON parser's recursion limit. Message values aren't cached.
func walkSnippet(m protoreflect.Message, visit func(protoreflect.Message)) {
	visit(m)
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Name() == "quotedMessage" || f.IsMap() || f.Kind() != protoreflect.MessageKind {
			return true
		}
		if f.IsList() {
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				walkSnippet(l.Get(i).Message(), visit)
			}
		} else {
			walkSnippet(v.Message(), visit)
		}
		return true
	})
}

// expandSnippetPayload fills the {variables} in a payload's text and
// captions (ExpandSnippet). Other strings, like URLs and IDs, stay. It
// reports whether they use {mention}.
func expandSnippetPayload(msg *waE2E.Message, vars map[string]string) (mention bool) {
	if len(vars) == 0 {
		return false
	}
	walkSnippet(msg.ProtoReflect(), func(m protoreflect.Message) {
		for _, name := range []protoreflect.Name{"conversation", "text", "caption"} {
			f := m.Descriptor().Fields().ByName(name)
			if f != nil && f.Kind() == protoreflect.StringKind && !f.IsList() && m.Has(f) {
				text := m.Get(f).String()
				mention = mention || model.SnippetUses(text, "mention")
				m.Set(f, protoreflect.ValueOfString(model.ExpandSnippet(text, vars)))
			}
		}
	})
	return mention
}

// snippetContext returns msg with a context (for a quote or mentions)
// and that context: a conversation becomes an extended text to carry it.
func snippetContext(msg *waE2E.Message) (*waE2E.Message, *waE2E.ContextInfo) {
	if ci := describe(msg).ctx; ci != nil {
		return msg, ci
	}
	ci := &waE2E.ContextInfo{}
	if msg.Conversation != nil {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: msg.Conversation, ContextInfo: ci}}, ci
	}
	attachSnippetContext(msg, ci)
	return msg, ci
}

func attachSnippetContext(msg *waE2E.Message, ci *waE2E.ContextInfo) {
	// The content's contextInfo field is on its first-level message.
	msg.ProtoReflect().Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Kind() != protoreflect.MessageKind || f.IsList() || f.IsMap() {
			return true
		}
		inner := v.Message()
		if cf := inner.Descriptor().Fields().ByName("contextInfo"); cf != nil {
			inner.Set(cf, protoreflect.ValueOfMessage(ci.ProtoReflect()))
			return false
		}
		return true
	})
}

func snippetMedia(msg *waE2E.Message) []whatsmeow.DownloadableMessage {
	var out []whatsmeow.DownloadableMessage
	walkSnippet(msg.ProtoReflect(), func(m protoreflect.Message) {
		if d, ok := m.Interface().(whatsmeow.DownloadableMessage); ok && whatsmeow.GetMediaType(d) != "" {
			out = append(out, d)
		}
	})
	return out
}

func (b *Backend) snippetMediaPath(d whatsmeow.DownloadableMessage) (string, error) {
	if len(d.GetFileSHA256()) != 32 {
		return "", errors.New("The media payload needs a valid file SHA-256.")
	}
	return filepath.Join(b.dataDir, "snippet-media", hex.EncodeToString(d.GetFileSHA256())), nil
}

// keepSnippetMedia streams media to disk before saving the template. Shared
// files are addressed by their hash; deleting a source chat won't remove them.
func (b *Backend) keepSnippetMedia(msg *waE2E.Message) (string, error) {
	var hashes []string
	for _, d := range snippetMedia(msg) {
		path, err := b.snippetMediaPath(d)
		if err != nil {
			return "", err
		}
		hashes = append(hashes, filepath.Base(path))
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			continue
		}
		cli := b.client()
		if cli == nil || !cli.IsConnected() {
			return "", errors.New("Connect to WhatsApp to save this snippet's media.")
		}
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		f, err := os.CreateTemp(filepath.Dir(path), "download-")
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(b.ctx, 5*time.Minute)
		err = cli.DownloadToFile(ctx, d, f)
		cancel()
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		if err != nil {
			_ = os.Remove(f.Name())
			return "", fmt.Errorf("Couldn't save snippet media: %w", err)
		}
	}
	return strings.Join(hashes, ","), nil
}

func (b *Backend) uploadSnippetMedia(msg *waE2E.Message) error {
	for _, d := range snippetMedia(msg) {
		path, err := b.snippetMediaPath(d)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(b.ctx, 5*time.Minute)
		res, err := uploadFile(ctx, b.client(), path, whatsmeow.GetMediaType(d))
		cancel()
		if err != nil {
			return err
		}
		m := d.(proto.Message).ProtoReflect()
		fields := map[protoreflect.Name]protoreflect.Value{
			"URL": protoreflect.ValueOfString(res.URL), "directPath": protoreflect.ValueOfString(res.DirectPath),
			"mediaKey": protoreflect.ValueOfBytes(res.MediaKey), "fileEncSHA256": protoreflect.ValueOfBytes(res.FileEncSHA256),
			"fileSHA256": protoreflect.ValueOfBytes(res.FileSHA256), "fileLength": protoreflect.ValueOfUint64(res.FileLength),
			"mediaKeyTimestamp": protoreflect.ValueOfInt64(b.now().Unix()),
		}
		for name, value := range fields {
			if f := m.Descriptor().Fields().ByName(name); f != nil {
				m.Set(f, value)
			}
		}
	}
	return nil
}

// Called with snippetMu held. Queued sends reserve their files even if a
// snippet is deleted before its upload starts.
func (b *Backend) cleanSnippetMedia() {
	b.snippetUseMu.Lock()
	defer b.snippetUseMu.Unlock()
	rows, err := b.db.QueryContext(b.ctx, `SELECT assets FROM wz_snippets`)
	if err != nil {
		return
	}
	used := map[string]bool{}
	for hash, n := range b.snippetRefs {
		if n > 0 {
			used[hash] = true
		}
	}
	for rows.Next() {
		var assets string
		if rows.Scan(&assets) != nil {
			rows.Close()
			return
		}
		for _, hash := range strings.Split(assets, ",") {
			used[hash] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	dir := filepath.Join(b.dataDir, "snippet-media")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || used[e.Name()] {
			continue
		}
		if raw, err := hex.DecodeString(e.Name()); err == nil && len(raw) == 32 {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func (b *Backend) snippetToSend(id int64) (model.Snippet, []byte, func(), error) {
	b.snippetUseMu.Lock()
	defer b.snippetUseMu.Unlock()
	var s model.Snippet
	var raw []byte
	var assets string
	err := b.db.QueryRowContext(b.ctx, `SELECT id,name,payload,body,raw_payload,assets FROM wz_snippets WHERE id=?`, id).
		Scan(&s.ID, &s.Name, &s.Payload, &s.Body, &raw, &assets)
	if err != nil {
		return s, nil, nil, err
	}
	if assets == "" {
		return s, raw, func() {}, nil
	}
	hashes := strings.Split(assets, ",")
	if b.snippetRefs == nil {
		b.snippetRefs = map[string]int{}
	}
	for _, hash := range hashes {
		b.snippetRefs[hash]++
	}
	return s, raw, func() {
		b.snippetUseMu.Lock()
		for _, hash := range hashes {
			b.snippetRefs[hash]--
			if b.snippetRefs[hash] == 0 {
				delete(b.snippetRefs, hash)
			}
		}
		b.snippetUseMu.Unlock()
		go func() { b.snippetMu.Lock(); defer b.snippetMu.Unlock(); b.cleanSnippetMedia() }()
	}, nil
}
