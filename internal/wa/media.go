package wa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// avatarTTL is how long a cached profile picture is trusted before refetching.
const avatarTTL = 24 * time.Hour

// fetcher runs background downloads one at a time, newest request first:
// the most recent requests are what's on screen right now.
type fetcher struct {
	mu      sync.Mutex
	stack   []func()
	pending map[string]bool
	wake    chan struct{}
}

func newFetcher() *fetcher {
	return &fetcher{pending: make(map[string]bool), wake: make(chan struct{}, 1)}
}

// add queues job under key unless the same key is already queued.
func (f *fetcher) add(key string, job func()) {
	f.mu.Lock()
	if f.pending[key] {
		f.mu.Unlock()
		return
	}
	f.pending[key] = true
	f.stack = append(f.stack, func() {
		job()
		f.mu.Lock()
		delete(f.pending, key)
		f.mu.Unlock()
	})
	f.mu.Unlock()
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (f *fetcher) run(ctx context.Context, gap time.Duration) {
	for {
		f.mu.Lock()
		var job func()
		if n := len(f.stack); n > 0 {
			job = f.stack[n-1]
			f.stack = f.stack[:n-1]
		}
		f.mu.Unlock()
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-f.wake:
			}
			continue
		}
		job()
		select {
		case <-ctx.Done():
			return
		case <-time.After(gap):
		}
	}
}

func fileKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		io.WriteString(h, p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func (b *Backend) avatarPath(id string) string {
	return filepath.Join(b.dataDir, "avatars", fileKey(id)+".jpg")
}

// Avatar implements model.Backend. An empty cache file means "no picture".
func (b *Backend) Avatar(id string) []byte {
	path := b.avatarPath(id)
	st, err := os.Stat(path)
	fresh := err == nil && time.Since(st.ModTime()) < avatarTTL
	if !fresh {
		b.avatars.add(id, func() { b.fetchAvatar(id) })
	}
	if err != nil || st.Size() == 0 {
		return nil
	}
	data, _ := os.ReadFile(path)
	return data
}

func (b *Backend) fetchAvatar(id string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return
	}
	jid, err := types.ParseJID(id)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	path := b.avatarPath(id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)

	info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized),
		err == nil && info == nil:
		if os.WriteFile(path, nil, 0o600) == nil {
			b.emit(model.AvatarEvent{ID: id})
		}
		return
	case err != nil:
		b.log.Debugf("profile picture of %s: %v", id, err)
		return
	}
	data, err := httpGet(ctx, info.URL)
	if err != nil {
		b.log.Debugf("download profile picture of %s: %v", id, err)
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.log.Warnf("save profile picture: %v", err)
		return
	}
	b.emit(model.AvatarEvent{ID: id})
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

func (b *Backend) mediaPath(chatID, msgID string) string {
	return filepath.Join(b.dataDir, "media", fileKey(chatID, msgID))
}

// MediaData implements model.Backend. A file with a ".failed" twin means the
// download failed permanently (e.g. the media expired on WhatsApp's servers).
func (b *Backend) MediaData(chatID, msgID string) []byte {
	path := b.mediaPath(chatID, msgID)
	if data, err := os.ReadFile(path); err == nil {
		return data
	}
	if _, err := os.Stat(path + ".failed"); err == nil {
		return nil
	}
	b.downloads.add(chatID+"/"+msgID, func() { b.download(chatID, msgID) })
	return nil
}

func (b *Backend) download(chatID, msgID string) {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Minute)
	defer cancel()
	media, blob, err := b.store.mediaBlob(ctx, chatID, msgID)
	if err != nil || len(blob) == 0 {
		return
	}
	var msg whatsmeow.DownloadableMessage
	switch media {
	case model.MediaImage:
		m := &waE2E.ImageMessage{}
		err = proto.Unmarshal(blob, m)
		msg = m
	case model.MediaSticker:
		m := &waE2E.StickerMessage{}
		err = proto.Unmarshal(blob, m)
		msg = m
	default:
		return
	}
	if err != nil {
		return
	}
	path := b.mediaPath(chatID, msgID)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, err := cli.Download(ctx, msg)
	if err != nil {
		b.log.Infof("download media %s: %v", msgID, err)
		if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
			_ = os.WriteFile(path+".failed", nil, 0o600)
			b.emit(model.MediaEvent{ChatID: chatID, MsgID: msgID})
		}
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.log.Warnf("save media: %v", err)
		return
	}
	b.emit(model.MediaEvent{ChatID: chatID, MsgID: msgID})
}
