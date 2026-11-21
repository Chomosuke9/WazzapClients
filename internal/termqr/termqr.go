// Package termqr draws QR codes as text, for a terminal (the headless
// mode's login) and as SVG, for a web page.
package termqr

import (
	"strconv"
	"strings"

	"rsc.io/qr"
)

// quiet is the margin around a code, in modules: scanners want some.
const quiet = 2

// Text draws data's QR code with half blocks, two rows of modules a line.
// With ansi set it paints black on white itself, which every terminal
// theme scans; without, the dark modules are blocks, for a terminal (or
// a log) with a light background.
func Text(data string, ansi bool) (string, error) {
	c, err := qr.Encode(data, qr.L)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	n := c.Size + 2*quiet
	black := func(x, y int) bool { return c.Black(x-quiet, y-quiet) }
	for y := 0; y < n; y += 2 {
		if ansi {
			sb.WriteString("\x1b[30;107m") // black on bright white
		}
		for x := range n {
			top, bottom := black(x, y), y+1 < n && black(x, y+1)
			switch {
			case top && bottom:
				sb.WriteString("█")
			case top:
				sb.WriteString("▀")
			case bottom:
				sb.WriteString("▄")
			default:
				sb.WriteByte(' ')
			}
		}
		if ansi {
			sb.WriteString("\x1b[0m")
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// SVG draws data's QR code as an SVG picture, a unit a module.
func SVG(data string) ([]byte, error) {
	c, err := qr.Encode(data, qr.L)
	if err != nil {
		return nil, err
	}
	n := strconv.Itoa(c.Size + 2*quiet)
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + n + " " + n + `" shape-rendering="crispEdges">`)
	sb.WriteString(`<rect width="` + n + `" height="` + n + `" fill="#fff"/><path fill="#000" d="`)
	for y := range c.Size {
		for x := 0; x < c.Size; x++ {
			if !c.Black(x, y) {
				continue
			}
			run := 1 // a row's dark modules side by side, as one rectangle
			for c.Black(x+run, y) {
				run++
			}
			sb.WriteString("M" + strconv.Itoa(x+quiet) + " " + strconv.Itoa(y+quiet) + "h" + strconv.Itoa(run) + "v1h-" + strconv.Itoa(run) + "z")
			x += run - 1
		}
	}
	sb.WriteString(`"/></svg>`)
	return []byte(sb.String()), nil
}
