// Package klipy searches KLIPY's GIFs (https://docs.klipy.com), for the
// picker's GIF tab.
//
// KLIPY's integration requirements shape it: requests go from the user's
// computer straight to KLIPY (no proxy of ours), media loads from the URLs
// a response gives, unchanged, and results show in the order they come.
// The app key isn't a secret: every client gets a copy.
package klipy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	PerPage = 24
	// maxJSON and maxMedia bound what a response can make us read.
	maxJSON  = 2 << 20
	maxMedia = 16 << 20
)

// formats are the renditions asked for: see GIF.
const formats = "jpg,webp,mp4"

var (
	baseURL = "https://api.klipy.com/api/v1/"
	client  = &http.Client{Timeout: 20 * time.Second}
)

// ErrKey means KLIPY refused the app key.
var ErrKey = errors.New("klipy: the API key was refused")

// Client asks KLIPY for GIFs with one app key.
type Client struct {
	Key string
	// CustomerID is a random ID that stays the same for this user, which
	// KLIPY's personalization and share counts ask for.
	CustomerID string
	// UserAgent names the app ("WazzapClients/v1.2.3").
	UserAgent string
}

// Media is one rendition of a GIF.
type Media struct {
	URL  string
	W, H int
	Size int64
}

// GIF is a result.
type GIF struct {
	Slug, Title string
	// Preview is a still picture for the grid and the message's
	// thumbnail (a small JPEG), Anim the same small size animated (a
	// WebP, or none), and Video the MP4 that is sent.
	Preview, Anim, Video Media
}

// Page is a page of results.
type Page struct {
	GIFs    []GIF
	HasNext bool
}

// Search returns a page (from 1) of the GIFs for q, or of the trending
// ones when q is empty.
func (c *Client) Search(ctx context.Context, q string, page int) (Page, error) {
	v := pageQuery(page)
	if c.CustomerID != "" {
		v.Set("customer_id", c.CustomerID)
	}
	what := "trending"
	if q = strings.TrimSpace(q); q != "" {
		what = "search"
		v.Set("q", q)
	}
	return c.list(ctx, "gifs/"+what+"?"+v.Encode())
}

// Recent returns a page (from 1) of the GIFs this user shared, newest
// first: KLIPY keeps them by CustomerID (Share adds one).
func (c *Client) Recent(ctx context.Context, page int) (Page, error) {
	if c.CustomerID == "" {
		return Page{}, nil
	}
	return c.list(ctx, "gifs/recent/"+url.PathEscape(c.CustomerID)+"?"+pageQuery(page).Encode())
}

// HideRecent takes a GIF off this user's recent ones.
func (c *Client) HideRecent(ctx context.Context, slug string) error {
	v := url.Values{}
	v.Set("slug", slug)
	return c.do(ctx, http.MethodDelete, "gifs/recent/"+url.PathEscape(c.CustomerID)+"?"+v.Encode(), nil, nil)
}

// MaxItems is how many GIFs Items asks for at once.
const MaxItems = PerPage

// Items returns the GIFs with these slugs, the ones KLIPY still has, in
// the order asked for. At most MaxItems are asked for.
func (c *Client) Items(ctx context.Context, slugs []string) ([]GIF, error) {
	slugs = slugs[:min(len(slugs), MaxItems)]
	if len(slugs) == 0 {
		return nil, nil
	}
	v := url.Values{}
	v.Set("slugs", strings.Join(slugs, ","))
	v.Set("format_filter", formats)
	p, err := c.list(ctx, "gifs/items?"+v.Encode())
	if err != nil {
		return nil, err
	}
	by := make(map[string]GIF, len(p.GIFs))
	for _, g := range p.GIFs {
		by[BaseSlug(g.Slug)] = g
	}
	var out []GIF
	for _, s := range slugs {
		if g, ok := by[BaseSlug(s)]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

// BaseSlug is a slug without the "--…" a search adds to it ("hello-8126"
// for "hello-8126--kBycButtD"), as Recent and Items give it. Compare
// slugs by it; send KLIPY the slug as it came.
func BaseSlug(slug string) string {
	base, _, _ := strings.Cut(slug, "--")
	return base
}

func pageQuery(page int) url.Values {
	v := url.Values{}
	v.Set("page", fmt.Sprint(max(page, 1)))
	v.Set("per_page", fmt.Sprint(PerPage))
	v.Set("format_filter", formats)
	return v
}

// list reads a list of GIFs from an endpoint.
func (c *Client) list(ctx context.Context, path string) (Page, error) {
	var resp struct {
		Result bool `json:"result"`
		Data   struct {
			Data    []item `json:"data"`
			HasNext bool   `json:"has_next"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return Page{}, err
	}
	p := Page{HasNext: resp.Data.HasNext}
	for _, it := range resp.Data.Data {
		if g, ok := it.gif(); ok {
			p.GIFs = append(p.GIFs, g)
		}
	}
	return p, nil
}

// Share tells KLIPY a GIF was sent, found by searching q ("" for
// trending), as its Share Trigger API asks.
func (c *Client) Share(ctx context.Context, slug, q string) error {
	body := map[string]string{"customer_id": c.CustomerID}
	if q = strings.TrimSpace(q); q != "" {
		body["q"] = q
	}
	data, _ := json.Marshal(body)
	return c.do(ctx, http.MethodPost, "gifs/share/"+url.PathEscape(slug), data, nil)
}

// Fetch downloads a media URL a result gave.
func (c *Client) Fetch(ctx context.Context, link string) ([]byte, error) {
	if !strings.HasPrefix(link, "https://") {
		return nil, errors.New("klipy: not an https link")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	c.header(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("klipy: " + resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMedia+1))
	if err == nil && len(data) > maxMedia {
		err = errors.New("klipy: the file is too big")
	}
	return data, err
}

func (c *Client) header(req *http.Request) {
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
}

// do calls an endpoint and decodes its JSON answer into out (unless nil).
func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	if c.Key == "" {
		return ErrKey
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+url.PathEscape(c.Key)+"/"+path, rd)
	if err != nil {
		return err
	}
	c.header(req)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrKey
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("klipy: too many requests, try again later")
	case resp.StatusCode/100 != 2:
		return errors.New("klipy: " + resp.Status)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxJSON))
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxJSON)).Decode(out)
}

// item is a result as KLIPY sends it: its renditions by size ("hd", "md",
// "sm", "xs") and format ("gif", "webp", "jpg", "mp4", "webm").
type item struct {
	Slug  string                       `json:"slug"`
	Title string                       `json:"title"`
	Type  string                       `json:"type"`
	File  map[string]map[string]rendit `json:"file"`
}

type rendit struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int64  `json:"size"`
}

// gif picks a result's renditions: a small still for the grid and the
// biggest MP4 (they are a few hundred KB). Anything but a GIF (an ad,
// which this app can't show) is left out: keys used here have ads off.
func (it item) gif() (GIF, bool) {
	if it.Type != "" && it.Type != "gif" || it.Slug == "" {
		return GIF{}, false
	}
	pick := func(format string, sizes ...string) Media {
		for _, s := range sizes {
			if r, ok := it.File[s][format]; ok && r.URL != "" && r.Width > 0 && r.Height > 0 {
				return Media{URL: r.URL, W: r.Width, H: r.Height, Size: r.Size}
			}
		}
		return Media{}
	}
	g := GIF{Slug: it.Slug, Title: it.Title,
		Preview: pick("jpg", "sm", "xs", "md", "hd"),
		Anim:    pick("webp", "sm", "xs"),
		Video:   pick("mp4", "hd", "md", "sm", "xs")}
	return g, g.Preview.URL != "" && g.Video.URL != ""
}
