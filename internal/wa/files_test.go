package wa

import (
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func box(typ string, body []byte) []byte {
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], typ)
	return append(b, body...)
}

func TestMP4Seconds(t *testing.T) {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)  // time scale
	binary.BigEndian.PutUint32(mvhd[16:], 61500) // duration
	data := append(box("ftyp", []byte("isom0000")), box("moov", append(box("udta", nil), box("mvhd", mvhd)...))...)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mp4Seconds(path); got != 62 {
		t.Errorf("mp4Seconds = %d, want 62", got)
	}
}

func TestOggSeconds(t *testing.T) {
	page := func(granule uint64) []byte {
		p := make([]byte, 27)
		copy(p, "OggS")
		binary.LittleEndian.PutUint64(p[6:], granule)
		return p
	}
	data := append(append(page(0), make([]byte, 500)...), page(48000*42+100)...)
	path := filepath.Join(t.TempDir(), "a.ogg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := oggSeconds(path, int64(len(data))); got != 42 {
		t.Errorf("oggSeconds = %d, want 42", got)
	}
}

func TestMP3Seconds(t *testing.T) {
	// A 128 kbit/s frame header after an empty ID3 tag, padded to 160 kB:
	// 10 seconds.
	data := make([]byte, 160000+10)
	copy(data, "ID3\x03\x00\x00\x00\x00\x00\x00")
	copy(data[10:], []byte{0xff, 0xfb, 0x90, 0x00})
	path := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mp3Seconds(path, int64(len(data))); got != 10 {
		t.Errorf("mp3Seconds = %d, want 10", got)
	}
}

func TestFileInfo(t *testing.T) {
	m := &model.Message{FileName: "a.pdf", FileSize: 1234, FileType: "application/pdf", Pages: 3, Waveform: []byte{1, 2}}
	s := fileOf(m).marshal()
	var back model.Message
	parseFile(s).apply(&back)
	if back.FileName != "a.pdf" || back.FileSize != 1234 || back.Pages != 3 || len(back.Waveform) != 2 {
		t.Errorf("round trip lost data: %q -> %+v", s, back)
	}
	if fileOf(&model.Message{}).marshal() != "" {
		t.Error("empty file info should marshal to an empty string")
	}
}

func TestMediaExt(t *testing.T) {
	for _, c := range []struct {
		m    model.Message
		want string
	}{
		{model.Message{Media: model.MediaVoice}, ".ogg"},
		{model.Message{Media: model.MediaDocument, FileName: "Report.PDF"}, ".pdf"},
		{model.Message{Media: model.MediaDocument, FileName: "evil.p/df"}, ".bin"},
		{model.Message{Media: model.MediaAudio, FileType: "audio/mpeg"}, ".mp3"},
		{model.Message{Media: model.MediaVideo}, ".mp4"},
	} {
		if got := mediaExt(&c.m); got != c.want {
			t.Errorf("mediaExt(%+v) = %q, want %q", c.m, got, c.want)
		}
	}
}

func TestMigrateFileInfo(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &msgStore{db: db}
	if err := s.init(ctx); err != nil {
		t.Fatal(err)
	}
	// A document stored before the file column existed: its name as text.
	doc := &waE2E.DocumentMessage{FileName: proto.String("a.pdf"), Caption: proto.String("see this"),
		FileLength: proto.Uint64(2048), PageCount: proto.Uint32(4)}
	if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts, media, text, media_blob) VALUES (?, ?, 1, ?, ?, ?)`,
		"c", "m1", int(model.MediaDocument), "a.pdf", marshal(doc)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM wz_meta`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateFileInfo(ctx); err != nil {
		t.Fatal(err)
	}
	r, ok := s.message(ctx, "c", "m1")
	if !ok {
		t.Fatal("message gone")
	}
	if r.Text != "see this" || r.FileName != "a.pdf" || r.FileSize != 2048 || r.Pages != 4 {
		t.Errorf("after migration: text %q, file %q, size %d, pages %d", r.Text, r.FileName, r.FileSize, r.Pages)
	}
}
