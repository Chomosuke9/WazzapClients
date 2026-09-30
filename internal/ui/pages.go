package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// page is what the navigation rail has selected.
type page int

const (
	pageChats page = iota
	pageCalls
	pageStatus
	pageChannels
	pageCommunities
	pageSettings
)

// btn returns the Clickable stored under key, creating it on first use.
// Lists of rows and per-item buttons use it instead of their own maps.
func (u *UI) btn(key string) *widget.Clickable {
	c, ok := u.clicks[key]
	if !ok {
		c = new(widget.Clickable)
		u.clicks[key] = c
	}
	return c
}

// pageHeader is the 68dp title row at the top of a sidebar page.
func (u *UI) pageHeader(gtx C, title string, buttons ...layout.FlexChild) D {
	p := u.pal
	children := append([]layout.FlexChild{
		layout.Flexed(1, u.label(23.5, title, p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
	}, buttons...)
	return vcenter(gtx, gtx.Dp(68), func(gtx C) D {
		return layout.Inset{Left: 21, Right: 21}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		})
	})
}

// headerButton is a 42dp round icon button for page headers.
func (u *UI) headerButton(c *widget.Clickable, ic *icon.Icon, size unit.Dp) layout.FlexChild {
	return layout.Rigid(func(gtx C) D { return u.iconButton(gtx, c, ic, 42, size, u.pal.IconStrong) })
}

// sectionLabel is a small gray heading inside a list ("Recent", "Viewed").
func (u *UI) sectionLabel(gtx C, txt string, in layout.Inset, o labelOpts) D {
	return in.Layout(gtx, u.label(15.2, txt, u.pal.TextSecondary, o).Layout)
}

// emptyPane is the right-hand placeholder of the Status, Channels,
// Communities and Settings pages: a large gray glyph, a title and a line
// of explanation, slightly above center, plus an optional footer.
func (u *UI) emptyPane(gtx C, glyph func(gtx C, col color.NRGBA) D, title, sub, footer string) D {
	p := u.pal
	dims := fill(gtx, p.Panel)
	gtx.Constraints.Min = gtx.Constraints.Max
	layout.Inset{Bottom: 43}.Layout(gtx, func(gtx C) D {
		return layout.Center.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(720))
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return glyph(gtx, p.EmptyIcon) }),
				layout.Rigid(layout.Spacer{Height: 32}.Layout),
				layout.Rigid(u.label(34.5, title, p.Text, labelOpts{weight: font.Medium, maxLines: 1, align: text.Middle}).Layout),
				layout.Rigid(layout.Spacer{Height: 14}.Layout),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(580))
					l := u.label(16.9, sub, p.TextSecondary, labelOpts{align: text.Middle})
					l.MaxLines = 0
					l.LineHeight, l.LineHeightScale = 26, 1
					return l.Layout(gtx)
				}),
			)
		})
	})
	if footer != "" {
		layout.S.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = 0
			return layout.Inset{Bottom: 45}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(icLockOutline, 20, p.TextSecondary)),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Rigid(u.label(14.7, footer, p.TextSecondary).Layout),
				)
			})
		})
	}
	return dims
}

// iconGlyph adapts an icon to the glyph signature emptyPane and the rail use.
func iconGlyph(ic *icon.Icon, size unit.Dp) func(gtx C, col color.NRGBA) D {
	return func(gtx C, col color.NRGBA) D { return drawIcon(gtx, ic, size, col) }
}

// roundedSquare paints a rounded-square tile with ic centered on it, as
// used for communities and the New community / announcements rows.
func roundedSquare(gtx C, size, radius unit.Dp, bg color.NRGBA, ic *icon.Icon, icSize unit.Dp, fg color.NRGBA) D {
	px := gtx.Dp(size)
	fillRRect(gtx, image.Rect(0, 0, px, px), gtx.Dp(radius), bg)
	return centerIn(gtx, px, iconW(ic, icSize, fg))
}

// listItem is one row of the Settings and info panel lists: an icon, a
// title and an optional gray subtitle, hover-highlighted.
type listItem struct {
	ic       *icon.Icon
	glyph    func(gtx C, col color.NRGBA) D // instead of ic
	title    string
	sub      string
	danger   bool          // red, for destructive actions
	trailing layout.Widget // after the text, e.g. a count or badge
	content  layout.Widget // replaces title and subtitle
}

func (u *UI) layoutListItem(gtx C, c *widget.Clickable, it listItem, g listGeom) D {
	p := u.pal
	fg, sub, icCol := p.Text, p.TextSecondary, p.TextSecondary
	if it.danger {
		fg, icCol = p.Danger, p.Danger
		if g.softDanger {
			fg, icCol = p.DangerSoft, p.DangerSoft
		}
	}
	return layout.Inset{Left: g.hoverLeft, Right: g.hoverRight}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, c, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := mix(p.Panel, p.RowHover, u.hover(gtx, c))
			return background(gtx, bg, 10, func(gtx C) D {
				minH := g.height
				if it.sub != "" || it.content != nil {
					minH = g.subHeight
				}
				return vcenter(gtx, gtx.Dp(minH), func(gtx C) D {
					pb := g.padBottom
					if pb == 0 {
						pb = g.padY
					}
					return layout.Inset{Top: g.padY, Bottom: pb}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								// The icon (or picture) is centered on iconCenter.
								pic := record(gtx, func(gtx C) D {
									gtx.Constraints.Min = image.Point{}
									if it.glyph != nil {
										return it.glyph(gtx, icCol)
									}
									return drawIcon(gtx, it.ic, 26, icCol)
								})
								pic.at(gtx, gtx.Dp(g.iconCenter)-pic.size.X/2, 0)
								return D{Size: image.Pt(gtx.Dp(g.textLeft), pic.size.Y)}
							}),
							layout.Flexed(1, func(gtx C) D {
								// padRight keeps text off the right edge; trailing
								// items may go closer.
								full := gtx.Constraints.Max.X
								gtx.Constraints.Max.X = max(0, full-gtx.Dp(g.padRight))
								gtx.Constraints.Min.X = min(gtx.Constraints.Min.X, gtx.Constraints.Max.X)
								d := u.listItemText(gtx, it, fg, sub)
								d.Size.X = full
								return d
							}),
							layout.Rigid(func(gtx C) D {
								if it.trailing == nil {
									return D{}
								}
								return it.trailing(gtx)
							}),
						)
					})
				})
			})
		})
	})
}

func (u *UI) listItemText(gtx C, it listItem, fg, sub color.NRGBA) D {
	if it.content != nil {
		return it.content(gtx)
	}
	if it.sub == "" {
		return u.label(17, it.title, fg).Layout(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.label(17, it.title, fg).Layout),
		layout.Rigid(layout.Spacer{Height: 2}.Layout),
		layout.Rigid(func(gtx C) D {
			l := u.label(15.2, it.sub, sub, labelOpts{})
			l.MaxLines = 0
			l.LineHeight, l.LineHeightScale = 20.5, 1
			return l.Layout(gtx)
		}),
	)
}

// listGeom positions listItem contents; all values are in dp, measured from
// the list's left edge.
type listGeom struct {
	hoverLeft, hoverRight unit.Dp // hover background insets
	iconCenter, textLeft  unit.Dp // relative to the hover background
	height, padY          unit.Dp // minimum height; top padding
	subHeight             unit.Dp // minimum height of rows with a subtitle
	padBottom             unit.Dp // defaults to padY
	padRight              unit.Dp // between the text and the right edge
	softDanger            bool    // paler red for destructive items, as in the info panel
}
