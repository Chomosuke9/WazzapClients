package ui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/klipy"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// The picker's GIF tab searches KLIPY (internal/klipy). Unlike the other
// extra features it is on until it's turned off. KLIPY asks apps to call it from the
// user's computer with an app key that isn't a secret: release builds have
// one (-X main.klipyKey), and anyone can enter their own.

// Preferences of GIF search (Backend.Pref keys). They belong to the app
// (appPrefs).
const (
	prefKlipy    = "klipy"          // GIF search, on unless "off"
	prefKlipyKey = "klipy_key"      // the user's own app key, or "" for the build's
	prefKlipyID  = "klipy_customer" // the random customer_id KLIPY counts shares by
	// prefKlipyFavs are the favourite GIFs' slugs, newest first. KLIPY
	// lets apps keep slugs, not media: the tab loads them (Items).
	prefKlipyFavs = "klipy_favs"
)

// maxGIFFavs is how many favourite GIFs are kept.
const maxGIFFavs = 200

// gifSet is one of the GIF tab's own tabs.
type gifSet int

const (
	gifTrending gifSet = iota
	gifRecent          // what you sent, which KLIPY keeps by customer ID
	gifFavs
)

var gifSets = []struct {
	ic    *icon.Icon
	empty string
}{
	{icTrending, "No GIFs found"},
	{icClock, "GIFs you send show up here."},
	{icStar, "Star a GIF to keep it here."},
}

// gifSource is what a list of results is: a set's, or the results of a
// search (q), whichever set is chosen.
type gifSource struct {
	set gifSet
	q   string
}

// klipyAppKey is the build's KLIPY key, and klipyAgent the User-Agent its
// requests name the app by. Run sets them.
var klipyAppKey, klipyAgent = "", "WazzapClients"

const (
	gifSearchDelay = 400 * time.Millisecond // typing pauses this long before a search
	gifRowH        = 116                    // dp, the height rows of results aim for
	gifGap         = 4                      // dp between results
	gifTimeout     = 30 * time.Second
)

// gifSearch is the GIF tab: its search and results, and the GIFs on
// their way to a chat.
type gifSearch struct {
	search widget.Editor
	list   widget.List

	set     gifSet
	setSel  switcher[int]
	setLine follower

	typed   string    // the search box's text
	since   time.Time // when it last changed
	src     gifSource // what the results are
	results []klipy.GIF
	favList []string // the favourites when their tab opened, which its pages load
	page    int      // pages loaded
	hasNext bool
	loading bool
	err     error
	gen     int // counts searches, so an old one's answer is dropped
	cancel  context.CancelFunc
	pages   chan gifPage
	sent    chan gifSent

	// The preferences, read with the other extras (loadExtras): Pref
	// reads the database.
	on       bool
	userKey  string // the user's own key, or ""
	customer string
	favs     []string      // slugs, newest first
	keyEd    widget.Editor // userKey, on the settings page
}

type gifPage struct {
	gen, page int
	src       gifSource
	res       klipy.Page
	err       error
}

// gifSent is a GIF downloaded to send.
type gifSent struct {
	chatID string
	reply  *model.Message
	gif    klipy.GIF
	path   string
	thumb  []byte
	err    error
}

// loadGIFPrefs reads GIF search's preferences.
func (u *UI) loadGIFPrefs() {
	g, b := &u.gifs, u.backend
	if b == nil {
		return
	}
	g.on = b.Pref(prefKlipy) != "off"
	g.userKey = trimSpace(b.Pref(prefKlipyKey))
	g.customer = b.Pref(prefKlipyID)
	g.favs = strings.Fields(b.Pref(prefKlipyFavs))
	g.keyEd.SingleLine = true
	g.keyEd.SetText(g.userKey)
}

// key is the key GIF search uses: the user's own, or the build's.
func (g *gifSearch) key() string {
	if g.userKey != "" {
		return g.userKey
	}
	return klipyAppKey
}

// klipyClient returns a client for the key in use, giving this user a
// customer ID the first time.
func (u *UI) klipyClient() *klipy.Client {
	g := &u.gifs
	if g.customer == "" {
		g.customer = newUUID()
		u.backend.SetPref(prefKlipyID, g.customer)
	}
	return &klipy.Client{Key: g.key(), CustomerID: g.customer, UserAgent: klipyAgent}
}

// newUUID returns a random (version 4) UUID, the anonymous customer ID
// KLIPY asks for.
func newUUID() string {
	var r [16]byte
	_, _ = rand.Read(r[:])
	r[6] = r[6]&0x0f | 0x40
	r[8] = r[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", r[0:4], r[4:6], r[6:8], r[8:10], r[10:])
}

// reset drops the results, so the tab loads them again (with another key,
// say).
func (g *gifSearch) reset() {
	if g.cancel != nil {
		g.cancel()
	}
	g.gen++
	g.src, g.results, g.page, g.hasNext, g.loading, g.err, g.cancel = gifSource{}, nil, 0, false, false, nil, nil
}

// want is what the tab should show: a search's results while there is
// text in the box, or else the chosen set.
func (g *gifSearch) want() gifSource {
	if g.typed != "" {
		return gifSource{set: gifTrending, q: g.typed}
	}
	return gifSource{set: g.set}
}

// isFav reports whether a GIF is a favourite.
func (g *gifSearch) isFav(slug string) bool {
	base := klipy.BaseSlug(slug)
	for _, f := range g.favs {
		if klipy.BaseSlug(f) == base {
			return true
		}
	}
	return false
}

// toggleFav makes a GIF a favourite, or no longer one.
func (u *UI) toggleFav(gif klipy.GIF) {
	g := &u.gifs
	base := klipy.BaseSlug(gif.Slug)
	keep := []string{}
	was := false
	for _, f := range g.favs {
		if klipy.BaseSlug(f) == base {
			was = true
		} else {
			keep = append(keep, f)
		}
	}
	if !was {
		keep = append([]string{gif.Slug}, keep[:min(len(keep), maxGIFFavs-1)]...)
	} else if g.src == (gifSource{set: gifFavs}) {
		u.dropResult(gif.Slug) // it leaves the Favourites tab at once
	}
	g.favs = keep
	u.backend.SetPref(prefKlipyFavs, strings.Join(keep, " "))
}

// hideRecent takes a GIF off the Recent tab, here and at KLIPY.
func (u *UI) hideRecent(gif klipy.GIF) {
	u.dropResult(gif.Slug)
	c := u.klipyClient()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gifTimeout)
		defer cancel()
		_ = c.HideRecent(ctx, gif.Slug)
	}()
}

// dropResult takes a GIF out of the results on screen.
func (u *UI) dropResult(slug string) {
	g := &u.gifs
	base := klipy.BaseSlug(slug)
	var keep []klipy.GIF
	for _, r := range g.results {
		if klipy.BaseSlug(r.Slug) != base {
			keep = append(keep, r)
		}
	}
	g.results = keep
}

// updateGIFs takes the answers of searches and sends the GIFs that
// downloaded.
func (u *UI) updateGIFs() {
	g := &u.gifs
	for drained := false; !drained; {
		select {
		case r := <-g.pages:
			if r.gen != g.gen || r.src != g.src {
				continue
			}
			g.loading, g.cancel, g.err = false, nil, r.err
			if r.err != nil {
				continue
			}
			if r.page == 1 {
				g.results = nil
				g.list.Position = layout.Position{}
			}
			g.results = append(g.results, r.res.GIFs...)
			g.page, g.hasNext = r.page, r.res.HasNext
		case s := <-g.sent:
			if s.err != nil {
				u.toast("Couldn't download the GIF.")
				continue
			}
			u.backend.SendFile(s.chatID, model.Attachment{Path: s.path, Media: model.MediaGIF, Thumb: s.thumb,
				W: s.gif.Video.W, H: s.gif.Video.H, Klipy: true}, model.Draft{Reply: s.reply})
			if g.src == (gifSource{set: gifRecent}) {
				g.reset() // KLIPY has it among the recent ones now
			}
		default:
			drained = true
		}
	}
}

// loadGIFs asks for a page of src's results.
func (u *UI) loadGIFs(src gifSource, page int) {
	g := &u.gifs
	if g.cancel != nil {
		g.cancel()
	}
	if g.pages == nil {
		g.pages = make(chan gifPage, 4)
	}
	if page == 1 {
		g.gen++
	}
	if page == 1 && src.set != g.src.set {
		g.results = nil // another tab's GIFs don't stay while it loads
	}
	g.src, g.loading = src, true
	var favs []string // the page's, for the Favourites tab
	if src == (gifSource{set: gifFavs}) {
		if page == 1 {
			g.favList = append([]string(nil), g.favs...)
		}
		from := min((page-1)*klipy.MaxItems, len(g.favList))
		favs = g.favList[from:min(from+klipy.MaxItems, len(g.favList))]
	}
	more := src.set == gifFavs && page*klipy.MaxItems < len(g.favList)
	ctx, cancel := context.WithTimeout(context.Background(), gifTimeout)
	g.cancel = cancel
	c, gen, pages, notify := u.klipyClient(), g.gen, g.pages, u.images.invalidate
	go func() {
		defer cancel()
		var res klipy.Page
		var err error
		switch {
		case src.q != "" || src.set == gifTrending:
			res, err = c.Search(ctx, src.q, page)
		case src.set == gifRecent:
			res, err = c.Recent(ctx, page)
		default:
			res.GIFs, err = c.Items(ctx, favs)
			res.HasNext = more
		}
		if ctx.Err() == context.Canceled {
			return // another search took its place
		}
		select {
		case pages <- gifPage{gen: gen, page: page, src: src, res: res, err: err}:
		default:
		}
		if notify != nil {
			notify()
		}
	}()
}

// sendGIF downloads a GIF and sends it to the open chat, as an answer to
// the message being replied to.
func (u *UI) sendGIF(gif klipy.GIF) {
	g := &u.gifs
	if u.selected == nil {
		return
	}
	if g.sent == nil {
		g.sent = make(chan gifSent, 8)
	}
	s := gifSent{chatID: u.selected.ID, reply: u.conv.reply, gif: gif}
	u.conv.reply = nil
	c, q, sent, notify := u.klipyClient(), g.src.q, g.sent, u.images.invalidate
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gifTimeout)
		defer cancel()
		s.path, s.err = downloadGIF(ctx, c, gif)
		if s.err == nil {
			s.thumb, _ = c.Fetch(ctx, gif.Preview.URL) // the bubble does without
			_ = c.Share(ctx, gif.Slug, q)
		}
		sent <- s
		if notify != nil {
			notify()
		}
	}()
}

// gifDir keeps GIFs between their download and their upload. The backend
// keeps its own copy of what you send, so they go after an hour.
func gifDir() string { return filepath.Join(os.TempDir(), "wazzap-gifs") }

// downloadGIF saves a GIF's MP4 in gifDir and returns its path.
func downloadGIF(ctx context.Context, c *klipy.Client, gif klipy.GIF) (string, error) {
	data, err := c.Fetch(ctx, gif.Video.URL)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("empty GIF")
	}
	dir := gifDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if old, err := os.ReadDir(dir); err == nil {
		for _, e := range old {
			if i, err := e.Info(); err == nil && time.Since(i.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	f, err := os.CreateTemp(dir, "gif-*.mp4")
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

// gifCell is a result in its row, in px from the row's left.
type gifCell struct {
	i    int // into gifSearch.results
	x, w int
}

// gifRows lays the results out in rows of about gifRowH that fill the
// width, each GIF as wide as its shape makes it.
func gifRows(gifs []klipy.GIF, width, rowH, gap int) (rows [][]gifCell, heights []int) {
	var cur []gifCell
	sum := 0 // the row's widths at rowH
	flush := func(full bool) {
		if len(cur) == 0 {
			return
		}
		h := rowH
		if avail := width - gap*(len(cur)-1); full && sum > 0 {
			h = rowH * avail / sum
		}
		x := 0
		for k := range cur {
			w := max(1, cur[k].w*h/rowH)
			if k == len(cur)-1 && full {
				w = width - x // no gap at the end
			}
			cur[k].x, cur[k].w = x, w
			x += w + gap
		}
		rows, heights = append(rows, cur), append(heights, h)
		cur, sum = nil, 0
	}
	for i, g := range gifs {
		m := g.Preview
		w := rowH
		if m.W > 0 && m.H > 0 {
			w = min(max(rowH*m.W/m.H, rowH/2), 3*rowH)
		}
		cur = append(cur, gifCell{i: i, w: w})
		sum += w
		if sum+gap*(len(cur)-1) >= width {
			flush(true)
		}
	}
	flush(false)
	return rows, heights
}

func (u *UI) layoutGIFTab(gtx C) D {
	g := &u.gifs
	p := u.pal
	if !g.on || g.key() == "" {
		gtx.Constraints.Max.Y -= gtx.Dp(56) // above the tabs
		msg := "Turn on GIF search in Settings > Extra features to find GIFs on KLIPY."
		if g.on {
			msg = "Enter a KLIPY API key in Settings > Extra features to search GIFs."
		}
		return u.pickerMessage(gtx, msg)
	}
	g.list.Axis = layout.Vertical
	for i := range gifSets {
		if u.btn("gset:" + itoa(i+1)).Clicked(gtx) {
			g.set = gifSet(i)
			g.search.SetText("") // a set's tab shows the set
		}
	}
	if t := trimSpace(g.search.Text()); t != g.typed {
		g.typed, g.since = t, gtx.Now
	}
	if want := g.want(); want != g.src || g.page == 0 && !g.loading && g.err == nil {
		// Typing waits for a pause; a tab, or an emptied box, loads at once.
		if wait := g.since.Add(gifSearchDelay); want.q != "" && gtx.Now.Before(wait) {
			gtx.Execute(op.InvalidateCmd{At: wait})
		} else if !g.loading || want != g.src {
			u.loadGIFs(want, 1)
		}
	}
	for _, gif := range g.results {
		switch {
		case u.btn("gifstar:" + gif.Slug).Clicked(gtx):
			u.toggleFav(gif)
		case u.btn("gifhide:" + gif.Slug).Clicked(gtx):
			u.hideRecent(gif)
		case u.btn("gif:" + gif.Slug).Clicked(gtx):
			u.sendGIF(gif)
			u.closePicker()
			return D{}
		}
	}
	w := gtx.Constraints.Max.X - gtx.Dp(24)
	rows, heights := gifRows(g.results, w, gtx.Dp(gifRowH), gtx.Dp(gifGap))
	// The next page loads as the list nears its end.
	if pos := g.list.Position; g.hasNext && !g.loading && g.err == nil && len(rows) > 0 &&
		pos.First+pos.Count >= len(rows)-2 {
		u.loadGIFs(g.src, g.page+1)
	}
	g.setSel.step(gtx, int(g.set), durSwitch)
	recent := g.src == (gifSource{set: gifRecent})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			ics := make([]*icon.Icon, len(gifSets))
			for i, s := range gifSets {
				ics[i] = s.ic
			}
			return u.pickerHeader(gtx, "gset:", ics, int(g.set), &g.setSel, &g.setLine)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 15, Right: 15, Top: 6, Bottom: 2}.Layout(gtx, func(gtx C) D {
				return u.pickerSearch(gtx, &g.search, "Search KLIPY")
			})
		}),
		layout.Rigid(func(gtx C) D {
			// KLIPY's attribution, which it asks for wherever its GIFs show.
			return layout.Inset{Right: 20, Top: 2, Bottom: 6}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return u.label(12, "Powered by KLIPY", p.TextSecondary, labelOpts{maxLines: 1, align: text.End}).Layout(gtx)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			if len(g.results) == 0 {
				gtx.Constraints.Max.Y -= gtx.Dp(56)
				gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
				var msg string
				switch {
				case errors.Is(g.err, klipy.ErrKey):
					msg = "KLIPY refused the API key. Check it in Settings > Extra features."
				case g.err != nil:
					msg = "Couldn't load GIFs. Check your connection and try again."
				case g.loading || g.want() != g.src || g.page == 0:
					msg = "Loading GIFs…"
				case g.src.q != "":
					msg = "No GIFs found"
				default:
					msg = gifSets[g.src.set].empty
				}
				return u.pickerMessage(gtx, msg)
			}
			return u.scrollList(gtx, &g.list, len(rows)+1, func(gtx C, row int) D {
				if row == len(rows) {
					return D{Size: image.Pt(0, gtx.Dp(56))} // room for the tabs
				}
				h := heights[row]
				for _, c := range rows[row] {
					t := op.Offset(image.Pt(gtx.Dp(12)+c.x, 0)).Push(gtx.Ops)
					u.layoutGIFCell(gtx, g.results[c.i], c.w, h, recent)
					t.Pop()
				}
				return D{Size: image.Pt(gtx.Constraints.Max.X, h+gtx.Dp(gifGap))}
			})
		}),
	)
}

// layoutGIFCell draws a result, w×h px: its still picture, which loads
// from KLIPY as it comes on screen, and under the pointer a star that
// makes it a favourite (always there on a favourite) and, on the Recent
// tab, a button that takes it off.
func (u *UI) layoutGIFCell(gtx C, gif klipy.GIF, w, h int, recent bool) {
	p := u.pal
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	cl := u.btn("gif:" + gif.Slug)
	clickable(gtx, cl, func(gtx C) D {
		r := image.Rect(0, 0, w, h)
		load := func() []byte {
			ctx, cancel := context.WithTimeout(context.Background(), gifTimeout)
			defer cancel()
			data, _ := (&klipy.Client{UserAgent: klipyAgent}).Fetch(ctx, gif.Preview.URL)
			return data
		}
		var img *imgEntry
		if u.blurred() {
			img = u.images.getBlurred("klipy:"+gif.Preview.URL, load)
		} else {
			img = u.images.get("klipy:"+gif.Preview.URL, max(w, h), load)
		}
		if img != nil && img.state == imgReady {
			paintCover(gtx, img.op, img.size, r)
		} else {
			fillRect(gtx, r, p.Hover)
		}
		// It plays over its still, through the animated stickers' players
		// (previewFrame): in their memory budget, as the Autoplay setting
		// says. The ones past the cap stay still.
		if gif.Anim.URL != "" && !u.blurred() && (perf.autoplay == "" || perf.autoplay == "hover" && cl.Hovered()) {
			loadAnim := func() []byte {
				ctx, cancel := context.WithTimeout(context.Background(), gifTimeout)
				defer cancel()
				data, _ := (&klipy.Client{UserAgent: klipyAgent}).Fetch(ctx, gif.Anim.URL)
				return data
			}
			if f, ok := u.previewFrame("klipy:"+gif.Anim.URL, max(w, h), loadAnim); ok {
				paintCover(gtx, f, f.Size(), r)
			}
		}
		if a := u.hover(gtx, cl); a > 0 {
			fillRect(gtx, r, faded(p.Shadow, 0.5*a))
		}
		return D{Size: r.Max}
	})
	// The buttons go on top, after the cell's own (see layoutChatRow).
	star, hide := u.btn("gifstar:"+gif.Slug), u.btn("gifhide:"+gif.Slug)
	on := max(u.hover(gtx, cl), u.hover(gtx, star), u.hover(gtx, hide))
	fav := u.gifs.isFav(gif.Slug)
	ic := icStar
	if fav {
		ic = icStarFill
	}
	pad, d := gtx.Dp(6), gtx.Dp(28)
	if w < 2*d+3*pad || h < d+2*pad {
		return // too small for them
	}
	if a := max(on, b2f(fav)); a > 0 {
		withOpacity(gtx, a, func() { gifButton(gtx, star, ic, image.Pt(w-d-pad, pad), d) })
	}
	if recent && on > 0 {
		withOpacity(gtx, on, func() { gifButton(gtx, hide, icClose, image.Pt(pad, pad), d) })
	}
}

// gifButton is a round button over a GIF, d px wide at at. Like the play
// button on videos, it looks the same over any picture in either theme.
func gifButton(gtx C, cl *widget.Clickable, ic *icon.Icon, at image.Point, d int) {
	defer op.Offset(at).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(d, d))
	clickable(gtx, cl, func(gtx C) D {
		fillCircle(gtx, image.Pt(d/2, d/2), d/2, argb(0x000000, 0x80))
		return centerIn2(gtx, d, d, iconW(ic, 18, rgb(0xffffff)))
	})
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// pickerSearch is a picker's round search box.
func (u *UI) pickerSearch(gtx C, ed *widget.Editor, hint string) D {
	p := u.pal
	ed.SingleLine = true
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	hgt := gtx.Dp(46)
	border := p.PopupBorder
	if gtx.Focused(ed) {
		border = p.Green
	}
	rr := image.Rect(0, 0, gtx.Constraints.Max.X, hgt)
	fillRRect(gtx, rr, hgt/2, border)
	fillRRect(gtx, rr.Inset(gtx.Dp(2)), hgt/2-gtx.Dp(2), p.Popup)
	return vcenter(gtx, hgt, func(gtx C) D {
		return layout.Inset{Left: 18, Right: 16}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icSearch, 22, p.TextSecondary)),
				layout.Rigid(layout.Spacer{Width: 16}.Layout),
				layout.Flexed(1, func(gtx C) D {
					e := material.Editor(u.th, ed, hint)
					e.TextSize = 15.5
					e.Color = p.Text
					e.HintColor = p.TextSecondary
					return e.Layout(gtx)
				}),
			)
		})
	})
}

// gifSettings is GIF search's section of the Extra features page.
func (u *UI) gifSettings() settingsSection {
	g := &u.gifs
	b := u.backend
	on := g.on
	sec := settingsSection{title: "GIFs", rows: []settingRow{
		{key: prefKlipy, kind: setToggle, on: on, title: "GIF search", // on by default
			sub: "Search GIFs from KLIPY on the GIF tab of the emoji panel. What you search for goes to KLIPY",
			run: func() {
				g.on = !on
				setExtra(b, prefKlipy, !on)
				g.reset()
			}},
	}}
	if !on {
		return sec
	}
	sec.rows = append(sec.rows, settingRow{key: "klipy:key", kind: setCustom, w: func(gtx C) D {
		return layout.Inset{Left: 24, Right: 24, Bottom: 4}.Layout(gtx, func(gtx C) D {
			hint := "Your KLIPY API key"
			if klipyAppKey != "" {
				hint = "Your own KLIPY API key (optional)"
			}
			d := u.pollLine(gtx, &g.keyEd, hint, false)
			if k := trimSpace(g.keyEd.Text()); k != g.userKey {
				g.userKey = k
				b.SetPref(prefKlipyKey, k)
				g.reset()
			}
			return d
		})
	}})
	sec.note = "Get a free key on partner.klipy.com, under API Keys, and keep its ads off: this app doesn't show them."
	if klipyAppKey != "" {
		sec.note = "This app has a KLIPY key of its own. Enter yours to use it instead. " +
			"Get one on partner.klipy.com, under API Keys, and keep its ads off: this app doesn't show them."
	}
	return sec
}

// showDemoGIFs fills the GIF tab with results of a few shapes, which load
// no pictures, for screenshots.
func (u *UI) showDemoGIFs() {
	g := &u.gifs
	g.on, g.userKey = true, "demo"
	g.reset()
	sizes := [][2]int{{220, 124}, {220, 220}, {220, 165}, {124, 220}, {220, 98}, {220, 147}, {220, 220}, {165, 220}, {220, 124}, {220, 180}}
	for i, s := range sizes {
		g.results = append(g.results, klipy.GIF{Slug: "demo-" + itoa(i), Preview: klipy.Media{W: s[0], H: s[1]}})
	}
	g.page = 1
	g.favs = []string{"demo-1"} // its star shows
}
