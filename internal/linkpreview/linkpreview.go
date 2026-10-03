// Package linkpreview reads what a web page says about itself (its Open
// Graph tags, or its title and description) to show a link as a card, as
// WhatsApp does for the links you send.
package linkpreview

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif" // og:image formats
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// Preview is what a page says about itself.
type Preview struct {
	Title       string
	Description string
	// Thumb is the page's picture as a small square JPEG, or nil.
	Thumb []byte
	// Image is the picture as a JPEG at most imageSide px on the long
	// side, W x H, for the wide card above the title, or nil when the
	// picture is too small for one.
	Image []byte
	W, H  int
}

const (
	maxPage   = 512 << 10 // the tags are in the head, near the start
	maxImage  = 4 << 20
	maxPixels = 6_000_000 // decoded, an RGBA picture takes 4 bytes a pixel
	thumbSide = 200
	imageSide = 1024 // what WhatsApp sends
	minWide   = 400  // narrower pictures only get the small square
	maxTitle  = 300
	maxDesc   = 600
	userAgent = "WhatsApp/2" // sites hand their preview tags to WhatsApp's fetcher
)

var client = &http.Client{Timeout: 10 * time.Second}

// ErrNoPreview means the page has nothing to show.
var ErrNoPreview = errors.New("linkpreview: the page has no title or description")

// Fetch reads the page at link ("https://…", "http://…" or "www.…") and
// returns its preview.
func Fetch(ctx context.Context, link string) (*Preview, error) {
	if !strings.Contains(link, "://") {
		link = "http://" + link
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("linkpreview: not a web link")
	}
	body, final, typ, err := get(ctx, u.String(), maxPage)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(typ, "image/") {
		// A link to a picture: show the picture, named by its file.
		p := &Preview{Title: lastSegment(final)}
		if pictures(p, body) != nil {
			return nil, ErrNoPreview
		}
		return p, nil
	}
	tags := parse(body)
	p := &Preview{
		Title:       clean(first(tags["og:title"], tags["twitter:title"], tags["title"]), maxTitle),
		Description: clean(first(tags["og:description"], tags["twitter:description"], tags["description"]), maxDesc),
	}
	if p.Title == "" && p.Description == "" {
		return nil, ErrNoPreview
	}
	if img := first(tags["og:image:secure_url"], tags["og:image"], tags["og:image:url"], tags["twitter:image"],
		tags["twitter:image:src"]); img != "" {
		if iu, err := final.Parse(img); err == nil && (iu.Scheme == "http" || iu.Scheme == "https") {
			// Some sites make the picture on the first request for it
			// (GitHub's), which can fail or time out: ask twice.
			for try := 0; try < 2 && p.Thumb == nil && ctx.Err() == nil; try++ {
				if data, _, _, err := get(ctx, iu.String(), maxImage); err == nil {
					pictures(p, data)
				}
			}
		}
	}
	return p, nil
}

// get downloads up to limit bytes of a URL, and tells where redirects
// ended and the content type.
func get(ctx context.Context, link string, limit int64) ([]byte, *url.URL, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.9,*/*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", errors.New("linkpreview: " + resp.Status)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, nil, "", err
	}
	return body, resp.Request.URL, typ, nil
}

// parse returns a page's meta tags by property or name ("og:title",
// "description"), and its <title> as "title". It stops at the body.
func parse(page []byte) map[string]string {
	tags := map[string]string{}
	z := html.NewTokenizer(bytes.NewReader(page))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return tags
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch atom.Lookup(name) {
			case atom.Body:
				return tags
			case atom.Title:
				inTitle = true
			case atom.Meta:
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "property", "name":
						if key == "" {
							key = strings.ToLower(string(v))
						}
					case "content":
						content = string(v)
					}
				}
				if key != "" && key != "title" && tags[key] == "" {
					tags[key] = content
				}
			}
		case html.EndTagToken:
			inTitle = false
		case html.TextToken:
			if inTitle && tags["title"] == "" {
				tags["title"] = string(z.Text())
			}
		}
	}
}

// pictures makes p's pictures of the picture in data: the small square
// JPEG a preview always carries and, for a picture big enough, the wide one.
func pictures(p *Preview, data []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width*cfg.Height > maxPixels {
		return errors.New("linkpreview: the picture is too big")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if p.Thumb, err = photo.SquareOf(img, thumbSide); err != nil {
		return err
	}
	if cfg.Width >= minWide && cfg.Width >= cfg.Height {
		p.Image, p.W, p.H, _ = photo.Fit(img, imageSide)
	}
	return nil
}

func first(s ...string) string {
	for _, v := range s {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// clean folds a tag's runs of white space and cuts it to at most n bytes.
func clean(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	s = strings.ToValidUTF8(s[:n], "")
	return strings.TrimSpace(s) + "…"
}

func lastSegment(u *url.URL) string {
	p := strings.TrimSuffix(u.Path, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i+1 < len(p) {
		if s, err := url.PathUnescape(p[i+1:]); err == nil {
			return s
		}
		return p[i+1:]
	}
	return u.Host
}
