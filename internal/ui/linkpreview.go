package ui

import (
	"context"
	"image"
	"image/color"
	"net/url"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/linkpreview"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// hasLinkCard reports whether a message's bubble shows a link preview.
func hasLinkCard(m *model.Message) bool {
	return m.Kind == model.KindText && m.Media == model.MediaNone && m.Link.Shown(m.Text)
}

// linkDomain returns the host a link goes to, without "www.".
func linkDomain(link string) string {
	if !strings.Contains(link, "://") {
		link = "http://" + link
	}
	h := link
	if p, err := url.Parse(link); err == nil && p.Host != "" {
		h = p.Hostname()
	}
	return strings.TrimPrefix(strings.ToLower(h), "www.")
}

// layoutLinkCard draws a message's link preview, width wide: its picture
// on the left, then the page's title, description and domain. Clicking it
// opens the link.
func (u *UI) layoutLinkCard(gtx C, m *model.Message, width int, radius unit.Dp, bg, textCol, secondary color.NRGBA) D {
	l := m.Link
	hasPic := len(m.Thumb) > 0 || m.ImageA != 0 || m.ImageB != 0
	side := 0
	if hasPic {
		side = gtx.Dp(88)
	}
	padX, padY := gtx.Dp(10), gtx.Dp(8)
	tgtx := gtx
	tgtx.Constraints = layout.Constraints{Max: image.Pt(max(0, width-side-2*padX), 1<<20)}
	var rows []layout.FlexChild
	gap := func() {
		if len(rows) > 0 {
			rows = append(rows, layout.Rigid(layout.Spacer{Height: 2}.Layout))
		}
	}
	if l.Title != "" {
		rows = append(rows, layout.Rigid(u.label(14, l.Title, textCol, labelOpts{weight: font.SemiBold, maxLines: 2}).Layout))
	}
	if l.Description != "" {
		gap()
		rows = append(rows, layout.Rigid(u.label(13, l.Description, secondary, labelOpts{maxLines: 2}).Layout))
	}
	if d := linkDomain(l.URL); l.URL != "" && d != "" {
		gap()
		rows = append(rows, layout.Rigid(u.label(13, d, secondary, labelOpts{maxLines: 1}).Layout))
	}
	txt := record(tgtx, func(gtx C) D { return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...) })
	h := max(side, txt.size.Y+2*padY)

	r := gtx.Dp(radius)
	fillRRect(gtx, image.Rect(0, 0, width, h), r, bg)
	if hasPic {
		pr := image.Rect(0, 0, side, h)
		func() {
			defer clip.RRect{Rect: pr, NW: r, SW: r}.Push(gtx.Ops).Pop()
			if img := u.messageImage(m, side*2); img != nil && img.state == imgReady {
				paintCover(gtx, img.op, img.size, pr)
			} else if m.ImageA != 0 || m.ImageB != 0 {
				u.gradientImage(gtx, pr, m.ImageA, m.ImageB)
			} else {
				fillRect(gtx, pr, u.pal.Hover)
			}
		}()
	}
	txt.at(gtx, side+padX, (h-txt.size.Y)/2)

	sz := image.Pt(width, h)
	if !u.conv.selecting {
		btn := u.btn("link:" + m.ID)
		if btn.Clicked(gtx) {
			u.openLink(l.URL)
		}
		cg := gtx
		cg.Constraints = layout.Exact(sz)
		clickable(cg, btn, func(gtx C) D { return D{Size: sz} })
	}
	return D{Size: sz}
}

// linkFetchDelay is how long the first link in the composer has to stay
// the same before its page is fetched, so typing one doesn't fetch every
// prefix of it.
const linkFetchDelay = 600 * time.Millisecond

// composerCardRadius rounds a link preview over the composer: 8dp in from
// the box, it follows the box's corner.
const composerCardRadius = 12

// composerLink is the preview of the first link in the composer, sent
// with the message.
type composerLink struct {
	url       string    // the first link in the composer, or ""
	since     time.Time // when url became the first link
	fetching  string    // the link being fetched
	cancel    context.CancelFunc
	failed    string // a link whose page has no preview
	dismissed string // a link whose preview was closed
	got       *linkResult
	results   chan linkResult
	anim      tween
	ghost     *linkResult // drawn while the card shrinks away
}

type linkResult struct {
	url  string
	prev *linkpreview.Preview
	err  error
}

// reset forgets the composer's link, for a chat switch or a sent message.
func (l *composerLink) reset() {
	if l.cancel != nil {
		l.cancel()
	}
	l.url, l.fetching, l.cancel, l.failed, l.dismissed, l.got = "", "", nil, "", "", nil
	l.anim.snap(false)
	l.ghost = nil
}

// updateComposerLink follows the first link in the composer: it fetches
// the link's preview once the link stops changing, and takes the result.
func (u *UI) updateComposerLink(gtx C) {
	l := &u.conv.link
	for drained := false; !drained; {
		select {
		case r := <-l.results:
			if r.url != l.fetching {
				continue // the composer moved on
			}
			l.fetching, l.cancel = "", nil
			if r.err != nil {
				l.failed = r.url
			} else if r.url == l.url {
				l.got = &r
			}
		default:
			drained = true
		}
	}
	url := ""
	if u.selected != nil && u.conv.edit.msg == nil && !u.conv.editorElsewhere && !u.postingStatus() &&
		prefOn(u.backend, prefLinkPreviews) {
		url = firstLink(u.conv.composer.Text())
	}
	if url != l.url {
		l.url, l.since, l.got = url, gtx.Now, nil
		if l.cancel != nil {
			l.cancel()
			l.fetching, l.cancel = "", nil
		}
	}
	if url == "" || l.got != nil || l.fetching != "" || url == l.failed || url == l.dismissed {
		return
	}
	if wait := l.since.Add(linkFetchDelay); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	if l.results == nil {
		l.results = make(chan linkResult, 8)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	l.fetching, l.cancel = url, cancel
	results, notify, fetch := l.results, u.images.invalidate, u.fetchLink
	go func() {
		defer cancel()
		p, err := fetch(ctx, url)
		select {
		case results <- linkResult{url: url, prev: p, err: err}:
		default:
		}
		if notify != nil {
			notify()
		}
	}()
}

// composerPreview returns the link preview to send with text, or nil.
func (u *UI) composerPreview(text string) *linkResult {
	l := &u.conv.link
	if g := l.got; g != nil && g.url == l.url && g.url != l.dismissed && strings.Contains(text, g.url) {
		return g
	}
	return nil
}

// layoutComposerLink is a link's preview above the composer's input, with
// a button that closes it.
func (u *UI) layoutComposerLink(gtx C, r *linkResult) D {
	p := u.pal
	if u.btn("composerlink:close").Clicked(gtx) {
		u.conv.link.dismissed = r.url
	}
	m := &model.Message{ID: "composer:" + r.url, Link: &model.LinkPreview{URL: r.url, Title: r.prev.Title,
		Description: r.prev.Description}, Thumb: r.prev.Thumb}
	return layout.Inset{Left: 8, Right: 8, Top: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return u.layoutLinkCard(gtx, m, gtx.Constraints.Max.X, composerCardRadius, p.QuoteIn, p.Text, p.TextSecondary)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				return u.iconButton(gtx, u.btn("composerlink:close"), icClose, 40, 24, p.Icon)
			}),
		)
	})
}
