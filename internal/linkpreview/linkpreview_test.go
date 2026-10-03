package linkpreview

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetch(t *testing.T) {
	var pic bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 600, 300))
	for i := range img.Pix {
		img.Pix[i] = 0xc0
	}
	img.Set(0, 0, color.Black)
	if err := png.Encode(&pic, img); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><title>Plain title</title>
			<meta property="og:title" content="Villa &amp; Pool">
			<meta name="description" content="  A  quiet
			place ">
			<meta property="og:image" content="/pic.png">
			</head><body><meta property="og:title" content="not this"></body></html>`))
	})
	mux.HandleFunc("/titled", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title> Just a title </title></head></html>`))
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>hi</body></html>`))
	})
	mux.HandleFunc("/pic.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pic.Bytes())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()

	p, err := Fetch(ctx, srv.URL+"/page")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Villa & Pool" || p.Description != "A quiet place" {
		t.Fatalf("got %q, %q", p.Title, p.Description)
	}
	th, err := jpeg.DecodeConfig(bytes.NewReader(p.Thumb))
	if err != nil || th.Width != thumbSide || th.Height != thumbSide {
		t.Fatalf("thumb %dx%d, %v", th.Width, th.Height, err)
	}
	// A wide picture also comes big, for the card above the title.
	if im, err := jpeg.DecodeConfig(bytes.NewReader(p.Image)); err != nil || im.Width != 600 || im.Height != 300 ||
		p.W != 600 || p.H != 300 {
		t.Fatalf("image %dx%d (%dx%d), %v", im.Width, im.Height, p.W, p.H, err)
	}

	if p, err := Fetch(ctx, srv.URL+"/titled"); err != nil || p.Title != "Just a title" || p.Thumb != nil {
		t.Fatalf("titled page: %+v, %v", p, err)
	}
	if _, err := Fetch(ctx, srv.URL+"/empty"); err != ErrNoPreview {
		t.Fatalf("empty page: %v", err)
	}
	if p, err := Fetch(ctx, srv.URL+"/pic.png"); err != nil || p.Title != "pic.png" || p.Thumb == nil {
		t.Fatalf("picture: %+v, %v", p, err)
	}
	if _, err := Fetch(ctx, "ftp://example.com/x"); err == nil {
		t.Fatal("fetched an ftp link")
	}
}
