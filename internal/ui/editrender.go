package ui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/osclip"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// Edited photos are rendered into new pictures to send, copy or save:
// a goroutine decodes the photo and crops, scales and filters it, the UI
// goroutine draws it with the marks (text needs the UI's shaper) on
// Gio's headless GPU renderer, and another goroutine encodes the result.

// maxEdited is the long side edited photos are rendered at, at most:
// WhatsApp's HD size.
const maxEdited = 4096

// colorMatrix maps a pixel's red, green and blue (0-255) to new ones:
// three rows of r, g, b factors and an offset.
type colorMatrix [12]float32

// filterMatrix saturates (0 is grey), adds contrast around mid-grey,
// brightens and tints.
func filterMatrix(sat, contrast, bright float32, tint [3]float32) colorMatrix {
	lum := [3]float32{0.2126, 0.7152, 0.0722}
	var m colorMatrix
	for i := range 3 {
		for j := range 3 {
			v := (1 - sat) * lum[j]
			if i == j {
				v += sat
			}
			m[4*i+j] = contrast * v
		}
		m[4*i+3] = 128*(1-contrast) + bright + tint[i]
	}
	return m
}

// photoFilters are WhatsApp's photo filters.
var photoFilters = []struct {
	name string
	m    colorMatrix
}{
	{"None", filterMatrix(1, 1, 0, [3]float32{})},
	{"Pop", filterMatrix(1.4, 1.12, 0, [3]float32{})},
	{"B&W", filterMatrix(0, 1.15, 0, [3]float32{})},
	{"Cool", filterMatrix(1.05, 1, 0, [3]float32{-12, 0, 14})},
	{"Chrome", filterMatrix(1.2, 1.22, 0, [3]float32{6, 0, -6})},
	{"Film", filterMatrix(0.7, 0.88, 12, [3]float32{8, 2, -6})},
}

// filtered returns a copy of img with filter k.
func filtered(img *image.RGBA, k int) *image.RGBA {
	out := &image.RGBA{Pix: bytes.Clone(img.Pix), Stride: img.Stride, Rect: img.Rect}
	applyFilter(out, k)
	return out
}

// applyFilter applies filter k to img in place.
func applyFilter(img *image.RGBA, k int) {
	if k <= 0 || k >= len(photoFilters) {
		return
	}
	var m [12]int32 // in 1/1024ths
	for i, v := range photoFilters[k].m {
		m[i] = int32(math.Round(float64(v) * 1024))
	}
	px := img.Pix
	for i := 0; i+3 < len(px); i += 4 {
		r, g, b, a := int32(px[i]), int32(px[i+1]), int32(px[i+2]), int32(px[i+3])
		for c := range 3 {
			// Premultiplied: the offset counts as much as the pixel shows.
			v := (m[4*c]*r + m[4*c+1]*g + m[4*c+2]*b + m[4*c+3]*a/255) >> 10
			px[i+c] = uint8(min(max(v, 0), a))
		}
	}
}

type renderSink int

const (
	sinkSend renderSink = iota // send it (outItem)
	sinkCopy                   // put it on the clipboard
	sinkSave                   // save it to Downloads
)

// renderJob is an edited photo on its way to a picture.
type renderJob struct {
	sink renderSink
	out  *outItem
	path string
	edit photoEdit

	// From the decoding goroutine.
	pics   photoPics
	region image.Rectangle
	scale  float32

	img    *image.RGBA // rendered
	result string      // the file written (sinkSend, sinkSave)
	err    error
	done   bool
}

// outItem is a file waiting its turn to be sent: files go out in the order
// they were picked, and edited photos wait for their rendering.
type outItem struct {
	chatID string
	att    model.Attachment
	draft  model.Draft
	temp   bool // att.Path is the app's own copy, removed once sent
	busy   bool // still rendering
}

// queueSend queues a picked file to send, rendering it first if edited.
func (u *UI) queueSend(chatID string, f *attachFile, d model.Draft) {
	a := &u.attach
	it := &outItem{chatID: chatID, att: f.Attachment, draft: d, temp: f.temp}
	it.att.Quality = a.quality
	a.outbox = append(a.outbox, it)
	if f.Media == model.MediaImage && f.edit.edited() {
		it.busy = true
		u.startRender(&renderJob{sink: sinkSend, out: it, path: f.Path, edit: f.edit.copyEdit()})
	}
}

// startRender decodes, crops, scales and filters the photo in the
// background; updateRenders takes it from there.
func (u *UI) startRender(j *renderJob) {
	a := &u.attach
	if a.renders == nil {
		a.renders = make(chan *renderJob, 16)
	}
	out, notify := a.renders, u.images.invalidate
	go func() {
		defer func() {
			out <- j
			if notify != nil {
				notify()
			}
		}()
		data, err := os.ReadFile(j.path)
		if err != nil {
			j.err = err
			return
		}
		release := acquireDecode(data)
		defer release()
		src, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			j.err = err
			return
		}
		b := src.Bounds()
		orig := b.Size()
		r := j.edit.region(orig, false)
		if r.Empty() {
			j.err = errors.New("the crop is empty")
			return
		}
		s := min(1, float64(maxEdited)/float64(max(r.Dx(), r.Dy())))
		w, h := max(1, int(math.Round(float64(r.Dx())*s))), max(1, int(math.Round(float64(r.Dy())*s)))
		sub := src
		if si, ok := src.(interface {
			SubImage(image.Rectangle) image.Image
		}); ok {
			sub = si.SubImage(r.Add(b.Min))
		}
		base := photo.Shrink(sub, w, h)
		applyFilter(base, j.edit.filter)
		block := float64(max(orig.X, orig.Y)) / blurBlocks
		mw := max(1, int(math.Ceil(float64(r.Dx())/block)))
		mh := max(1, int(math.Ceil(float64(r.Dy())/block)))
		j.pics = photoPics{
			img: paint.NewImageOp(base), size: image.Pt(w, h), cover: r,
			mosaic: paint.NewImageOp(photo.Shrink(base, mw, mh)), mosaicSize: image.Pt(mw, mh), mosaicCover: r,
		}
		j.region, j.scale = r, float32(s)
	}()
}

// updateRenders moves render jobs along, and sends what is ready.
func (u *UI) updateRenders() {
	a := &u.attach
	for {
		var j *renderJob
		select {
		case j = <-a.renders:
		default:
		}
		if j == nil {
			break
		}
		switch {
		case j.done || j.err != nil:
			u.renderDone(j)
		default:
			j.img, j.err = u.renderEdit(j)
			j.pics = photoPics{} // let the photo go
			if j.err != nil {
				u.renderDone(j)
				continue
			}
			u.finishRender(j)
		}
	}
	u.flushOutbox()
}

// renderEdit draws a decoded job's photo with its marks, offscreen.
func (u *UI) renderEdit(j *renderJob) (*image.RGBA, error) {
	w, h := j.pics.size.X, j.pics.size.Y
	if j.edit.rot%2 != 0 {
		w, h = h, w
	}
	win, err := headless.NewWindow(w, h)
	if err != nil {
		return nil, err
	}
	defer win.Release()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Now:         time.Now(),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(w, h)),
	}
	paint.Fill(&ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	tr, _ := fitPhoto(j.region, j.edit.rot, image.Rect(0, 0, w, h), false)
	u.drawPhoto(gtx, j.pics, &j.edit, j.region, tr, -1)
	if err := win.Frame(&ops); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		return nil, err
	}
	return img, nil
}

// finishRender encodes or copies a rendered picture in the background.
func (u *UI) finishRender(j *renderJob) {
	out, notify := u.attach.renders, u.images.invalidate
	var owner uintptr
	if u.host != nil {
		owner = u.host.hwnd
	}
	go func() {
		defer func() {
			j.img, j.done = nil, true
			out <- j
			if notify != nil {
				notify()
			}
		}()
		if j.sink == sinkCopy {
			j.err = osclip.SetImage(owner, j.img)
			return
		}
		var buf bytes.Buffer
		if j.err = jpeg.Encode(&buf, j.img, &jpeg.Options{Quality: 95}); j.err != nil {
			return
		}
		if j.sink == sinkSave {
			j.result, j.err = saveToDownloads(strings.TrimSuffix(filepath.Base(j.path), filepath.Ext(j.path))+" (edited).jpg", bytes.NewReader(buf.Bytes()))
			return
		}
		j.result, j.err = writeTemp("edited.jpg", buf.Bytes())
	}()
}

// renderDone reports a finished job.
func (u *UI) renderDone(j *renderJob) {
	switch j.sink {
	case sinkSend:
		if j.err == nil {
			j.out.att.Path, j.out.temp = j.result, true
		} else {
			u.toast("Couldn't apply the edits; the photo was sent as it was.")
		}
		j.out.busy = false
	case sinkCopy:
		if j.err != nil {
			u.toast("Couldn't copy the photo.")
		} else {
			u.toast("Photo copied")
		}
	case sinkSave:
		if j.err != nil {
			u.toast("Couldn't save the photo: " + j.err.Error())
		} else {
			u.toast("Saved to " + j.result)
		}
	}
}

// flushOutbox sends the queued files that are ready, in order.
func (u *UI) flushOutbox() {
	a := &u.attach
	for len(a.outbox) > 0 && !a.outbox[0].busy {
		it := a.outbox[0]
		a.outbox[0] = nil
		a.outbox = a.outbox[1:]
		if isStatusDestination(it.chatID) {
			att := it.att
			u.backend.PostStatus(model.StatusPost{GroupID: statusGroup(it.chatID), Text: it.draft.Text, File: &att})
		} else if m := u.backend.SendFile(it.chatID, it.att, it.draft); m != nil {
			if u.chatByID(m.ChatID) == nil && u.selected != nil && u.selected.ID == m.ChatID {
				u.chats = append(u.chats, u.selected)
			}
			u.upsertMessage(m)
		}
		if it.temp {
			// Photos are read as they're sent; a copy stays in the media cache.
			_ = os.Remove(it.att.Path)
		}
	}
	if len(a.outbox) == 0 {
		a.outbox = nil
	}
}

// exportPhoto copies the photo on show, with its edits, to the clipboard
// or saves it to Downloads.
func (u *UI) exportPhoto(f *attachFile, sink renderSink) {
	if f.edit.edited() {
		u.startRender(&renderJob{sink: sink, path: f.Path, edit: f.edit.copyEdit()})
		return
	}
	// Unedited, the photo itself will do.
	out, notify := u.attach.renders, u.images.invalidate
	if out == nil {
		u.attach.renders = make(chan *renderJob, 16)
		out = u.attach.renders
	}
	var owner uintptr
	if u.host != nil {
		owner = u.host.hwnd
	}
	j := &renderJob{sink: sink, path: f.Path}
	go func() {
		defer func() {
			j.done = true
			out <- j
			if notify != nil {
				notify()
			}
		}()
		src, err := os.Open(j.path)
		if err != nil {
			j.err = err
			return
		}
		defer src.Close()
		if sink == sinkSave {
			j.result, j.err = saveToDownloads(filepath.Base(j.path), src)
			return
		}
		img, _, err := image.Decode(src)
		if err != nil {
			j.err = err
			return
		}
		j.err = osclip.SetImage(owner, img)
	}()
}

// saveToDownloads writes r to a new file named name (or "name (1)", ...)
// in the Downloads folder, and returns its path.
func saveToDownloads(name string, r io.Reader) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	path := filepath.Join(dir, name)
	for i := 1; ; i++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) && i < 1000 {
			path = filepath.Join(dir, base+" ("+strconv.Itoa(i)+")"+ext)
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = io.Copy(f, r)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
}

// writeTemp writes data to a new file in the app's temporary folder; the
// name ends in suffix.
func writeTemp(suffix string, data []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "WazzapClients")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "*-"+suffix)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
