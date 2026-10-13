package klipy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const page = `{"result":true,"data":{"data":[
 {"id":1,"slug":"hello-1","title":"Hello","type":"gif","file":{
  "hd":{"mp4":{"url":"https://static.klipy.com/hd.mp4","width":498,"height":280,"size":119294},
        "jpg":{"url":"https://static.klipy.com/hd.jpg","width":498,"height":280,"size":19255}},
  "sm":{"mp4":{"url":"https://static.klipy.com/sm.mp4","width":220,"height":124,"size":9000},
        "jpg":{"url":"https://static.klipy.com/sm.jpg","width":220,"height":124,"size":7000},
        "webp":{"url":"https://static.klipy.com/sm.webp","width":220,"height":124,"size":90000}}}},
 {"id":2,"slug":"ad-1","type":"ad","file":{}},
 {"id":3,"slug":"no-video","type":"gif","file":{"sm":{"jpg":{"url":"https://static.klipy.com/x.jpg","width":1,"height":1}}}}
],"current_page":1,"per_page":24,"has_next":true}}`

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL + "/api/v1/"
	t.Cleanup(func() { baseURL = old })
	return &Client{Key: "k/ey", CustomerID: "c1", UserAgent: "WazzapClients/test"}
}

func TestSearch(t *testing.T) {
	var got *http.Request
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		io.WriteString(w, page)
	})
	p, err := c.Search(context.Background(), " cats ", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.EscapedPath() != "/api/v1/k%2Fey/gifs/search" {
		t.Errorf("path %q", got.URL.EscapedPath())
	}
	q := got.URL.Query()
	if q.Get("q") != "cats" || q.Get("page") != "2" || q.Get("customer_id") != "c1" || q.Get("format_filter") != "jpg,webp,mp4" {
		t.Errorf("query %v", q)
	}
	if ua := got.Header.Get("User-Agent"); ua != "WazzapClients/test" {
		t.Errorf("user agent %q", ua)
	}
	if !p.HasNext || len(p.GIFs) != 1 {
		t.Fatalf("page %+v", p)
	}
	g := p.GIFs[0]
	if g.Slug != "hello-1" || g.Preview.URL != "https://static.klipy.com/sm.jpg" || g.Anim.URL != "https://static.klipy.com/sm.webp" || g.Video.URL != "https://static.klipy.com/hd.mp4" ||
		g.Video.W != 498 || g.Video.H != 280 {
		t.Errorf("gif %+v", g)
	}
}

func TestTrending(t *testing.T) {
	var path string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, page)
	})
	if _, err := c.Search(context.Background(), "", 1); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/gifs/trending") {
		t.Errorf("path %q", path)
	}
}

func TestShare(t *testing.T) {
	var body map[string]string
	var path, method string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"result":true}`)
	})
	if err := c.Share(context.Background(), "hello-1", "cats"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || !strings.HasSuffix(path, "/gifs/share/hello-1") || body["q"] != "cats" || body["customer_id"] != "c1" {
		t.Errorf("%s %s %v", method, path, body)
	}
}

func TestBadKey(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if _, err := c.Search(context.Background(), "x", 1); !errors.Is(err, ErrKey) {
		t.Errorf("err %v", err)
	}
	c.Key = ""
	if _, err := c.Search(context.Background(), "x", 1); !errors.Is(err, ErrKey) {
		t.Errorf("no key: err %v", err)
	}
}

func TestRecent(t *testing.T) {
	var path string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, page)
	})
	p, err := c.Recent(context.Background(), 1)
	if err != nil || len(p.GIFs) != 1 {
		t.Fatalf("page %+v, err %v", p, err)
	}
	if !strings.HasSuffix(path, "/gifs/recent/c1") {
		t.Errorf("path %q", path)
	}
	c.CustomerID = ""
	if p, err := c.Recent(context.Background(), 1); err != nil || len(p.GIFs) != 0 {
		t.Errorf("no customer: %+v %v", p, err)
	}
}

func TestHideRecent(t *testing.T) {
	var r0 *http.Request
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		r0 = r
		io.WriteString(w, `{"result":true,"data":[]}`)
	})
	if err := c.HideRecent(context.Background(), "hello-1--x"); err != nil {
		t.Fatal(err)
	}
	if r0.Method != http.MethodDelete || !strings.HasSuffix(r0.URL.Path, "/gifs/recent/c1") || r0.URL.Query().Get("slug") != "hello-1--x" {
		t.Errorf("%s %s", r0.Method, r0.URL)
	}
}

// Items keeps the order asked for, whichever way the slugs are written,
// and leaves out what KLIPY no longer has.
func TestItems(t *testing.T) {
	const items = `{"result":true,"data":{"data":[
 {"slug":"b-2","type":"gif","file":{"sm":{"jpg":{"url":"https://s/b.jpg","width":2,"height":2},"mp4":{"url":"https://s/b.mp4","width":2,"height":2}}}},
 {"slug":"a-1","type":"gif","file":{"sm":{"jpg":{"url":"https://s/a.jpg","width":2,"height":2},"mp4":{"url":"https://s/a.mp4","width":2,"height":2}}}}]}}`
	var slugs string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		slugs = r.URL.Query().Get("slugs")
		io.WriteString(w, items)
	})
	gifs, err := c.Items(context.Background(), []string{"a-1--xyz", "gone-3", "b-2"})
	if err != nil {
		t.Fatal(err)
	}
	if slugs != "a-1--xyz,gone-3,b-2" {
		t.Errorf("asked for %q", slugs)
	}
	if len(gifs) != 2 || gifs[0].Slug != "a-1" || gifs[1].Slug != "b-2" {
		t.Errorf("got %+v", gifs)
	}
	if BaseSlug("hello-8126--kBycButtD") != "hello-8126" || BaseSlug("hello-8126") != "hello-8126" {
		t.Error("BaseSlug")
	}
}
