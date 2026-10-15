package ui

import (
	"context"
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/linkpreview"
	"github.com/chomosuke9/wazzapclients/internal/mock"
)

func TestLinkDomain(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.YouTube.com/watch?v=1": "youtube.com",
		"go.dev/blog":                       "go.dev",
		"http://example.com:8080/x":         "example.com",
	} {
		if got := linkDomain(in); got != want {
			t.Errorf("linkDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestComposerLink: a link typed in the composer is fetched once it stops
// changing, its preview shows above the input and goes out with the
// message; a closed preview stays closed, and the privacy setting stops
// the fetching.
func TestComposerLink(t *testing.T) {
	b := mock.New()
	u := New(b)
	var mu sync.Mutex
	var fetched []string
	u.fetchLink = func(ctx context.Context, link string) (*linkpreview.Preview, error) {
		mu.Lock()
		fetched = append(fetched, link)
		mu.Unlock()
		if link == "https://nothing.example" {
			return nil, errors.New("no preview")
		}
		return &linkpreview.Preview{Title: "Example", Description: "A page", Thumb: []byte{1}}, nil
	}
	u.Start(func() {})
	u.SelectID("budi")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func(d time.Duration) {
		now = now.Add(d)
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
	}
	// waitFetch draws frames until the fetch started for the composer's
	// link has come back.
	waitFetch := func() {
		t.Helper()
		for range 200 {
			frame(10 * time.Millisecond)
			if u.conv.link.fetching == "" {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("the fetch didn't come back")
	}
	fetches := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), fetched...)
	}

	// Typing a link fetches only what it ends up as.
	for _, s := range []string{"look https://ex", "look https://example.com/pa", "look https://example.com/page"} {
		u.conv.composer.SetText(s)
		frame(100 * time.Millisecond)
	}
	if f := fetches(); len(f) != 0 {
		t.Fatalf("fetched while typing: %q", f)
	}
	frame(linkFetchDelay)
	waitFetch()
	if f := fetches(); len(f) != 1 || f[0] != "https://example.com/page" {
		t.Fatalf("fetched %q", f)
	}
	if u.composerPreview(u.conv.composer.Text()) == nil {
		t.Fatal("no preview for the link")
	}
	frame(time.Second)
	if u.conv.link.ghost == nil {
		t.Fatal("the preview isn't drawn")
	}

	u.sendComposer()
	m := u.msgs[len(u.msgs)-1]
	if m.Link == nil || m.Link.Title != "Example" || m.Link.URL != "https://example.com/page" || len(m.Thumb) != 1 {
		t.Fatalf("sent %+v with %+v", m, m.Link)
	}
	if !hasLinkCard(m) {
		t.Fatal("the sent message shows no card")
	}

	// A closed preview isn't sent; a page without one is fetched once.
	u.conv.composer.SetText("again https://example.com/page")
	frame(0)
	frame(linkFetchDelay)
	waitFetch()
	u.conv.link.dismissed = u.conv.link.url
	if u.composerPreview(u.conv.composer.Text()) != nil {
		t.Fatal("a closed preview is still offered")
	}
	u.conv.composer.SetText("https://nothing.example")
	frame(0)
	frame(linkFetchDelay)
	waitFetch()
	frame(time.Second)
	if n := len(fetches()); n != 3 {
		t.Fatalf("%d fetches, want 3", n)
	}

	// With previews turned off nothing is fetched.
	b.SetPref(prefLinkPreviews, "off")
	u.conv.composer.SetText("https://example.org")
	frame(0)
	frame(linkFetchDelay)
	frame(time.Second)
	if n := len(fetches()); n != 3 || u.conv.link.fetching != "" {
		t.Fatalf("fetched with previews off: %q", fetches())
	}
}

// TestComposerLinkLoading: while a link's page is fetched a loading card
// shows over the composer, the preview takes its place once it comes, and
// closing the loading card stops the fetch.
func TestComposerLinkLoading(t *testing.T) {
	u := New(mock.New())
	release := make(chan struct{})
	canceled := make(chan struct{}, 1)
	u.fetchLink = func(ctx context.Context, link string) (*linkpreview.Preview, error) {
		select {
		case <-release:
			return &linkpreview.Preview{Title: "Example"}, nil
		case <-ctx.Done():
			canceled <- struct{}{}
			return nil, ctx.Err()
		}
	}
	u.Start(func() {})
	u.SelectID("budi")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func(d time.Duration) {
		now = now.Add(d)
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
	}

	u.conv.composer.SetText("see https://example.com")
	frame(0)
	frame(linkFetchDelay)
	frame(time.Second)
	card := u.composerCard(u.conv.composer.Text())
	if card == nil || card.prev != nil || u.conv.link.ghost != card {
		t.Fatalf("loading: card %+v, drawn %+v", card, u.conv.link.ghost)
	}
	if u.composerPreview(u.conv.composer.Text()) != nil {
		t.Fatal("the loading card would be sent")
	}
	close(release)
	for range 200 {
		frame(10 * time.Millisecond)
		if u.conv.link.fetching == "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if card := u.composerCard(u.conv.composer.Text()); card == nil || card.prev == nil {
		t.Fatalf("after the fetch: card %+v", card)
	}

	// Closing the loading card cancels its fetch.
	release = make(chan struct{})
	u.conv.composer.SetText("and https://example.org")
	frame(0)
	frame(linkFetchDelay)
	if u.composerCard(u.conv.composer.Text()) == nil {
		t.Fatal("no loading card for the second link")
	}
	u.btn("composerlink:close").Click()
	frame(0)
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the loading card didn't stop the fetch")
	}
	if card := u.composerCard(u.conv.composer.Text()); card != nil {
		t.Fatalf("closed, but still shows %+v", card)
	}
}
