// Package photo scales and compresses a picked photo for sending.
package photo

import (
	"bytes"
	"image"
	"image/jpeg"

	_ "image/gif" // formats a picked photo may come in
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Prepared is a photo ready to upload.
type Prepared struct {
	Data  []byte
	PNG   bool // Data is a PNG (only for QualityRaw); otherwise a JPEG
	W, H  int
	Thumb []byte // JPEG, about 100 px on the long side
}

// Each quality's longest side and JPEG quality. Raw has no limit.
var settings = map[model.Quality]struct{ side, jpeg int }{
	model.QualityStandard: {1600, 80},
	model.QualityHD:       {4096, 90},
	model.QualityRaw:      {0, 95},
}

// Size returns the size a w x h photo is sent at with quality q.
func Size(w, h int, q model.Quality) (int, int) {
	side := settings[q].side
	if s := max(w, h); side > 0 && s > side {
		return max(1, side*w/s), max(1, side*h/s)
	}
	return w, h
}

// Prepare scales and compresses the photo in data for quality q. A JPEG
// that already fits is kept as it is when re-encoding wouldn't make it
// smaller, and QualityRaw keeps any JPEG or PNG.
func Prepare(data []byte, q model.Quality) (Prepared, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Prepared{}, err
	}
	p, err := encode(img, format, data, q)
	if err != nil {
		return Prepared{}, err
	}
	// WhatsApp's own thumbnails are about 100 px on the long side.
	s := max(p.W, p.H, 1)
	t := flatten(Shrink(img, max(1, 100*p.W/s), max(1, 100*p.H/s)))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, t, &jpeg.Options{Quality: 70}); err == nil {
		p.Thumb = buf.Bytes()
	}
	return p, nil
}

// Estimate is what each quality makes of a photo.
type Estimate struct {
	W, H  [3]int // indexed by model.Quality
	Bytes [3]int
}

// Estimates prepares the photo in data at every quality, decoding it once.
func Estimates(data []byte) (Estimate, error) {
	var e Estimate
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return e, err
	}
	for _, q := range []model.Quality{model.QualityStandard, model.QualityHD, model.QualityRaw} {
		p, err := encode(img, format, data, q)
		if err != nil {
			return e, err
		}
		e.W[q], e.H[q], e.Bytes[q] = p.W, p.H, len(p.Data)
	}
	return e, nil
}

func encode(img image.Image, format string, data []byte, q model.Quality) (Prepared, error) {
	b := img.Bounds()
	p := Prepared{W: b.Dx(), H: b.Dy()}
	if q == model.QualityRaw && (format == "jpeg" || format == "png") {
		p.Data, p.PNG = data, format == "png"
		return p, nil
	}
	w, h := Size(p.W, p.H, q)
	out := img
	if w != p.W || h != p.H || !opaque(img) {
		out = flatten(Shrink(img, w, h))
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: settings[q].jpeg}); err != nil {
		return Prepared{}, err
	}
	p.Data, p.W, p.H = buf.Bytes(), w, h
	if format == "jpeg" && out == img && len(data) <= len(p.Data) {
		p.Data = data
	}
	return p, nil
}

func opaque(img image.Image) bool {
	o, ok := img.(interface{ Opaque() bool })
	return ok && o.Opaque()
}

// flatten puts img's transparent parts on white, as JPEG has no alpha.
func flatten(img *image.RGBA) *image.RGBA {
	px := img.Pix
	for i := 0; i+3 < len(px); i += 4 {
		if a := px[i+3]; a < 0xff {
			// Premultiplied: add the white showing through.
			px[i] += 0xff - a
			px[i+1] += 0xff - a
			px[i+2] += 0xff - a
			px[i+3] = 0xff
		}
	}
	return img
}

// Square crops the middle square out of the photo in data and scales it
// down to at most side px, as a JPEG: a profile or group picture.
func Square(data []byte, side int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return SquareOf(img, side)
}

// SquareOf is Square of a decoded picture.
func SquareOf(img image.Image, side int) ([]byte, error) {
	b := img.Bounds()
	s := min(b.Dx(), b.Dy())
	crop := image.Rect(0, 0, s, s).Add(b.Min).Add(image.Pt((b.Dx()-s)/2, (b.Dy()-s)/2))
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		img = sub.SubImage(crop)
	}
	out := flatten(Shrink(img, min(s, side), min(s, side)))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Fit scales img down to at most side px on the long side and returns it
// as a JPEG, with its size.
func Fit(img image.Image, side int) ([]byte, int, int, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if s := max(w, h); s > side {
		w, h = max(1, side*w/s), max(1, side*h/s)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flatten(Shrink(img, w, h)), &jpeg.Options{Quality: 80}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), w, h, nil
}
