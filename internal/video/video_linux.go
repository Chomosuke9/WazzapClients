//go:build linux && cgo

package video

// The Linux backend drives GStreamer's playbin, the player behind GNOME's
// and most other desktops' video apps: it picks the system's decoders,
// plays the sound itself and keeps it in sync. It decodes in software
// (see wz_no_hardware); WAZZAP_HWDEC=1 lets it use the GPU's decoders. The picture goes through videoflip (phone videos turned upright),
// is converted and scaled to the size the app asked for, and an appsink
// hands each RGBA frame over.
//
// GStreamer is opened with dlopen, never linked, so the app starts on a
// system without it; Open then returns ErrUnsupported and the app hands
// videos to the system's player app, as before. Nor does the build need
// its headers: the preamble declares the few types and functions it uses,
// all part of GStreamer 1.x's stable ABI.
//
// Like the Windows backend, the player's goroutine polls: the bus for
// messages and the appsink for frames, so no C callback calls into Go.

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <malloc.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef void *ptr;

// GstMiniObject and GstMessage, as far as the message type.
typedef struct {
	size_t type;
	int refcount, lockstate;
	unsigned flags;
	ptr copy, dispose, free_fn;
	unsigned priv_uint;
	ptr priv_pointer;
} wz_mini_object;

typedef struct {
	wz_mini_object mini;
	int type;
} wz_message;

// GstMapInfo.
typedef struct {
	ptr memory;
	int flags;
	uint8_t *data;
	size_t size, maxsize;
	ptr user_data[4];
	ptr reserved[4];
} wz_map_info;

// GList.
typedef struct wz_list {
	ptr data;
	struct wz_list *next, *prev;
} wz_list;

// GError.
typedef struct {
	uint32_t domain;
	int code;
	char *message;
} wz_error;

static int (*wz_gst_init_check)(int *, char ***, wz_error **);
static ptr (*wz_gst_element_factory_make)(const char *, const char *);
static ptr (*wz_gst_element_factory_find)(const char *);
static ptr (*wz_gst_parse_bin_from_description)(const char *, int, wz_error **);
static ptr (*wz_gst_bin_get_by_name)(ptr, const char *);
static ptr (*wz_gst_element_get_static_pad)(ptr, const char *);
static ptr (*wz_gst_pad_get_current_caps)(ptr);
static int (*wz_gst_element_set_state)(ptr, int);
static ptr (*wz_gst_element_get_bus)(ptr);
static ptr (*wz_gst_bus_pop_filtered)(ptr, int);
static int (*wz_gst_element_seek)(ptr, double, int, int, int, int64_t, int, int64_t);
static int (*wz_gst_element_query_position)(ptr, int, int64_t *);
static int (*wz_gst_element_query_duration)(ptr, int, int64_t *);
static ptr (*wz_gst_sample_get_buffer)(ptr);
static ptr (*wz_gst_sample_get_caps)(ptr);
static ptr (*wz_gst_caps_get_structure)(ptr, unsigned);
static ptr (*wz_gst_caps_from_string)(const char *);
static int (*wz_gst_structure_get_int)(ptr, const char *, int *);
static int (*wz_gst_structure_get_fraction)(ptr, const char *, int *, int *);
static int (*wz_gst_buffer_map)(ptr, wz_map_info *, int);
static void (*wz_gst_buffer_unmap)(ptr, wz_map_info *);
static void (*wz_gst_mini_object_unref)(ptr);
static void (*wz_gst_object_unref)(ptr);
static ptr (*wz_gst_app_sink_try_pull_sample)(ptr, uint64_t);
static ptr (*wz_gst_app_sink_try_pull_preroll)(ptr, uint64_t);
static void (*wz_g_object_set)(ptr, const char *, ...);
static void (*wz_g_error_free)(wz_error *);
static unsigned long (*wz_g_signal_connect_data)(ptr, const char *, ptr, ptr, ptr, int);
static char *(*wz_gst_object_get_name)(ptr);
static void (*wz_g_free)(ptr);
static wz_list *(*wz_gst_element_factory_list_get_elements)(uint64_t, int);
static int (*wz_gst_element_factory_list_is_type)(ptr, uint64_t);
static void (*wz_gst_plugin_feature_set_rank)(ptr, unsigned);
static void (*wz_gst_plugin_feature_list_free)(wz_list *);

// wz_element_added caps the threads of libav's decoders, which by default
// start one per core, each holding frames of its own: a 1080p video took
// about 90 MB more with 20 threads than with 2, which keep up with it.
static void wz_element_added(ptr bin, ptr sub, ptr element, ptr data) {
	char *name = wz_gst_object_get_name(element);
	if (name && strncmp(name, "avdec_", 6) == 0) wz_g_object_set(element, "max-threads", 2, NULL);
	wz_g_free(name);
}

// wz_no_hardware keeps playbin from picking hardware decoders (VA-API,
// NVDEC, V4L2...), from the registry, without loading their plugins.
// Loading the VA plugin opens every GPU: on a laptop with a sleeping
// second GPU that woke it, the first video took 2.6 s to start, and the
// drivers it loads stayed in memory. A broken driver would crash the app.
static void wz_no_hardware(void) {
	wz_list *all = wz_gst_element_factory_list_get_elements(1, 0); // DECODER, any rank
	for (wz_list *l = all; l; l = l->next) {
		if (wz_gst_element_factory_list_is_type(l->data, (uint64_t)1 << 12)) { // HARDWARE
			wz_gst_plugin_feature_set_rank(l->data, 0);
		}
	}
	wz_gst_plugin_feature_list_free(all);
}

// wz_trim gives the C heap's free memory back to the system.
static void wz_trim(void) { malloc_trim(0); }

#define WZ_SYM(lib, name) \
	if (!(*(void **)&wz_##name = dlsym(lib, #name))) return "missing " #name;

// wz_load opens GStreamer and starts it, with hardware decoders unless
// hw is 0. It returns NULL, or what failed.
static const char *wz_load(int hw) {
	void *gst = dlopen("libgstreamer-1.0.so.0", RTLD_NOW | RTLD_LOCAL);
	void *app = dlopen("libgstapp-1.0.so.0", RTLD_NOW | RTLD_LOCAL);
	void *gobject = dlopen("libgobject-2.0.so.0", RTLD_NOW | RTLD_LOCAL);
	void *glib = dlopen("libglib-2.0.so.0", RTLD_NOW | RTLD_LOCAL);
	if (!gst || !app || !gobject || !glib) return "no GStreamer libraries";
	WZ_SYM(gst, gst_init_check)
	WZ_SYM(gst, gst_element_factory_make)
	WZ_SYM(gst, gst_element_factory_find)
	WZ_SYM(gst, gst_parse_bin_from_description)
	WZ_SYM(gst, gst_bin_get_by_name)
	WZ_SYM(gst, gst_element_get_static_pad)
	WZ_SYM(gst, gst_pad_get_current_caps)
	WZ_SYM(gst, gst_element_set_state)
	WZ_SYM(gst, gst_element_get_bus)
	WZ_SYM(gst, gst_bus_pop_filtered)
	WZ_SYM(gst, gst_element_seek)
	WZ_SYM(gst, gst_element_query_position)
	WZ_SYM(gst, gst_element_query_duration)
	WZ_SYM(gst, gst_sample_get_buffer)
	WZ_SYM(gst, gst_sample_get_caps)
	WZ_SYM(gst, gst_caps_get_structure)
	WZ_SYM(gst, gst_caps_from_string)
	WZ_SYM(gst, gst_structure_get_int)
	WZ_SYM(gst, gst_structure_get_fraction)
	WZ_SYM(gst, gst_buffer_map)
	WZ_SYM(gst, gst_buffer_unmap)
	WZ_SYM(gst, gst_mini_object_unref)
	WZ_SYM(gst, gst_object_unref)
	WZ_SYM(app, gst_app_sink_try_pull_sample)
	WZ_SYM(app, gst_app_sink_try_pull_preroll)
	WZ_SYM(gobject, g_object_set)
	WZ_SYM(glib, g_error_free)
	WZ_SYM(gobject, g_signal_connect_data)
	WZ_SYM(gst, gst_object_get_name)
	WZ_SYM(glib, g_free)
	WZ_SYM(gst, gst_element_factory_list_get_elements)
	WZ_SYM(gst, gst_element_factory_list_is_type)
	WZ_SYM(gst, gst_plugin_feature_set_rank)
	WZ_SYM(gst, gst_plugin_feature_list_free)
	wz_error *err = NULL;
	if (!wz_gst_init_check(NULL, NULL, &err)) {
		if (err) wz_g_error_free(err);
		return "gst_init failed";
	}
	if (!hw) wz_no_hardware();
	return NULL;
}

// GstPlayFlags: video, audio, soft-volume. No subtitles, deinterlacing or
// color balance, which would add elements to every pipeline.
enum { wz_flags_video = 0x13, wz_flags_audio = 0x12 };

typedef struct {
	ptr pipe, bus, sink, caps, flip_src;
} wz_pipeline;

static void wz_free(wz_pipeline *p) {
	if (p->pipe) wz_gst_element_set_state(p->pipe, 1); // GST_STATE_NULL
	if (p->flip_src) wz_gst_object_unref(p->flip_src);
	if (p->caps) wz_gst_object_unref(p->caps);
	if (p->sink) wz_gst_object_unref(p->sink);
	if (p->bus) wz_gst_object_unref(p->bus);
	if (p->pipe) wz_gst_object_unref(p->pipe);
	memset(p, 0, sizeof *p);
}

// wz_open builds the pipeline for uri, and reports whether it could.
static int wz_open(wz_pipeline *p, const char *uri, int audio) {
	memset(p, 0, sizeof *p);
	p->pipe = wz_gst_element_factory_make("playbin", NULL);
	if (!p->pipe) return 0; // no gst-plugins-base
	wz_g_object_set(p->pipe, "uri", uri, "flags", audio ? wz_flags_audio : wz_flags_video, NULL);
	// scaletempo keeps the pitch at other speeds.
	ptr tempo = wz_gst_element_factory_make("scaletempo", NULL);
	if (tempo) wz_g_object_set(p->pipe, "audio-filter", tempo, NULL);
	wz_g_signal_connect_data(p->pipe, "deep-element-added", (ptr)wz_element_added, NULL, NULL, 0);
	if (!audio) {
		// videoconvertscale (GStreamer 1.22) converts and scales in one pass.
		ptr f = wz_gst_element_factory_find("videoconvertscale");
		const char *convert = f ? "videoconvertscale" : "videoconvert ! videoscale";
		if (f) wz_gst_object_unref(f);
		char desc[512];
		snprintf(desc, sizeof desc,
			"videoflip name=flip video-direction=auto ! %s ! "
			"capsfilter name=caps caps=video/x-raw,format=RGBA,pixel-aspect-ratio=1/1 ! "
			"appsink name=sink max-buffers=1 drop=true enable-last-sample=false", convert);
		wz_error *err = NULL;
		ptr bin = wz_gst_parse_bin_from_description(desc, 1, &err);
		if (err) wz_g_error_free(err);
		if (!bin) {
			wz_free(p);
			return 0;
		}
		wz_g_object_set(p->pipe, "video-sink", bin, NULL);
		p->sink = wz_gst_bin_get_by_name(bin, "sink");
		p->caps = wz_gst_bin_get_by_name(bin, "caps");
		ptr flip = wz_gst_bin_get_by_name(bin, "flip");
		if (flip) {
			p->flip_src = wz_gst_element_get_static_pad(flip, "src");
			wz_gst_object_unref(flip);
		}
		if (!p->sink || !p->caps || !p->flip_src) {
			wz_free(p);
			return 0;
		}
	}
	p->bus = wz_gst_element_get_bus(p->pipe);
	if (!p->bus) {
		wz_free(p);
		return 0;
	}
	return 1;
}

static int wz_set_state(wz_pipeline *p, int state) {
	return wz_gst_element_set_state(p->pipe, state);
}

static int wz_seek(wz_pipeline *p, double rate, int flags, int64_t pos) {
	// GST_FORMAT_TIME, GST_SEEK_TYPE_SET, GST_SEEK_TYPE_NONE
	return wz_gst_element_seek(p->pipe, rate, 3, flags, 1, pos, 0, -1);
}

static int64_t wz_position(wz_pipeline *p) {
	int64_t v = -1;
	if (!wz_gst_element_query_position(p->pipe, 3, &v)) return -1;
	return v;
}

static int64_t wz_duration(wz_pipeline *p) {
	int64_t v = -1;
	if (!wz_gst_element_query_duration(p->pipe, 3, &v)) return -1;
	return v;
}

// wz_pop returns the type of the next of the messages in types and takes
// it off the bus, or returns 0.
static int wz_pop(wz_pipeline *p, int types) {
	wz_message *m = wz_gst_bus_pop_filtered(p->bus, types);
	if (!m) return 0;
	int t = m->type;
	wz_gst_mini_object_unref(m);
	return t;
}

static void wz_set_bool(wz_pipeline *p, const char *name, int v) {
	wz_g_object_set(p->pipe, name, v, NULL);
}

static void wz_set_double(wz_pipeline *p, const char *name, double v) {
	wz_g_object_set(p->pipe, name, v, NULL);
}

// wz_native reports the picture's size as shown: turned upright, with
// square pixels.
static int wz_native(wz_pipeline *p, int *w, int *h) {
	ptr caps = wz_gst_pad_get_current_caps(p->flip_src);
	if (!caps) return 0;
	ptr s = wz_gst_caps_get_structure(caps, 0);
	int ok = s && wz_gst_structure_get_int(s, "width", w) && wz_gst_structure_get_int(s, "height", h);
	int n = 1, d = 1;
	if (ok && wz_gst_structure_get_fraction(s, "pixel-aspect-ratio", &n, &d) && n > 0 && d > 0 && n != d) {
		*w = (int)((int64_t)*w * n / d);
	}
	wz_gst_mini_object_unref(caps);
	return ok;
}

// wz_set_size makes the frames w by h pixels.
static void wz_set_size(wz_pipeline *p, int w, int h) {
	char s[128];
	snprintf(s, sizeof s, "video/x-raw,format=RGBA,width=%d,height=%d,pixel-aspect-ratio=1/1", w, h);
	ptr caps = wz_gst_caps_from_string(s);
	if (!caps) return;
	wz_g_object_set(p->caps, "caps", caps, NULL);
	wz_gst_mini_object_unref(caps);
}

// wz_pull takes the newest frame off the sink: a played one, or with
// preroll set, the one a paused pipeline shows. It returns NULL if there
// is none, and the frame's size otherwise.
static ptr wz_pull(wz_pipeline *p, int preroll, int *w, int *h) {
	ptr sample = preroll ? wz_gst_app_sink_try_pull_preroll(p->sink, 0)
	                     : wz_gst_app_sink_try_pull_sample(p->sink, 0);
	if (!sample) return NULL;
	ptr caps = wz_gst_sample_get_caps(sample);
	ptr s = caps ? wz_gst_caps_get_structure(caps, 0) : NULL;
	if (!s || !wz_gst_structure_get_int(s, "width", w) || !wz_gst_structure_get_int(s, "height", h) ||
	    *w <= 0 || *h <= 0) {
		wz_gst_mini_object_unref(sample);
		return NULL;
	}
	return sample;
}

// wz_copy copies sample's w by h RGBA picture into dst, and frees sample.
static int wz_copy(ptr sample, uint8_t *dst, int stride, int w, int h) {
	ptr buf = wz_gst_sample_get_buffer(sample);
	wz_map_info m;
	int ok = 0;
	if (buf && wz_gst_buffer_map(buf, &m, 1)) { // GST_MAP_READ
		size_t row = (size_t)w * 4, pitch = (row + 3) & ~(size_t)3;
		if (m.size >= pitch * (size_t)(h - 1) + row) {
			for (int y = 0; y < h; y++) memcpy(dst + (size_t)y * stride, m.data + (size_t)y * pitch, row);
			ok = 1;
		}
		wz_gst_buffer_unmap(buf, &m);
	}
	wz_gst_mini_object_unref(sample);
	return ok;
}
*/
import "C"

import (
	"errors"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"
)

var errPlay = errors.New("video: this video can't play")

const (
	pollPlaying = 8 * time.Millisecond
	pollIdle    = 40 * time.Millisecond
)

// GStreamer's constants, from gstelement.h, gstmessage.h and gstsegment.h.
const (
	statePaused  = 3
	statePlaying = 4

	stateChangeFailure = 0

	msgEOS         = 1 << 0
	msgError       = 1 << 1
	msgSegmentDone = 1 << 17
	msgAsyncDone   = 1 << 21

	seekFlush       = 1 << 0
	seekAccurate    = 1 << 1
	seekKeyUnit     = 1 << 2
	seekSegment     = 1 << 3
	seekSnapBefore  = 1 << 5
	seekSnapAfter   = 1 << 6
	seekSnapNearest = seekSnapBefore | seekSnapAfter
)

var (
	loadOnce sync.Once
	loadErr  error
)

// load opens GStreamer once.
func load() error {
	loadOnce.Do(func() {
		hw := C.int(0)
		if os.Getenv("WAZZAP_HWDEC") == "1" {
			hw = 1
		}
		if msg := C.wz_load(hw); msg != nil {
			loadErr = errors.New("video: " + C.GoString(msg))
		}
	})
	return loadErr
}

// gstPlayer is the state of the player's goroutine.
type gstPlayer struct {
	p    *Player
	pipe C.wz_pipeline

	ready   bool // prerolled once: the length and size are known
	paused  bool
	ended   bool
	failed  bool
	loop    bool
	rate    float64
	preroll bool // a paused pipeline has a new frame to show

	pendingSeek  bool // a seek asked for before ready
	seekPos      time.Duration
	seekExact    bool
	pendingRate  bool
	native, size image.Point // the picture's size as shown, and the frames'
}

func start(p *Player, path string) error {
	if load() != nil {
		return ErrUnsupported
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uri := C.CString((&url.URL{Scheme: "file", Path: abs}).String())
	defer C.free(unsafe.Pointer(uri))
	g := &gstPlayer{p: p, rate: 1}
	audio := C.int(0)
	if p.audio {
		audio = 1
	}
	if C.wz_open(&g.pipe, uri, audio) == 0 {
		return ErrUnsupported
	}
	if C.wz_set_state(&g.pipe, statePlaying) == stateChangeFailure {
		C.wz_free(&g.pipe)
		return errPlay
	}
	go g.run()
	return nil
}

func (g *gstPlayer) run() {
	defer C.wz_trim()
	defer C.wz_free(&g.pipe)
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-g.p.done:
			return
		case c := <-g.p.cmds:
			g.do(c)
		case <-t.C:
		}
		if g.poll() {
			t.Reset(pollPlaying)
		} else {
			t.Reset(pollIdle)
		}
	}
}

func (g *gstPlayer) do(c command) {
	switch c.kind {
	case cmdPlay:
		if g.ended {
			g.seek(0, true) // like <video>: play at the end starts over
		}
		g.paused = false
		C.wz_set_state(&g.pipe, statePlaying)
	case cmdPause:
		if !g.paused {
			g.paused = true
			C.wz_set_state(&g.pipe, statePaused)
		}
	case cmdSeek:
		if !g.ready {
			g.pendingSeek, g.seekPos, g.seekExact = true, c.pos, c.exact
			return
		}
		g.seek(c.pos, c.exact)
	case cmdMute:
		name := C.CString("mute")
		C.wz_set_bool(&g.pipe, name, boolInt(c.on))
		C.free(unsafe.Pointer(name))
	case cmdVolume:
		name := C.CString("volume")
		C.wz_set_double(&g.pipe, name, C.double(c.vol))
		C.free(unsafe.Pointer(name))
	case cmdRate:
		if c.rate <= 0 || c.rate == g.rate {
			return
		}
		g.rate = c.rate
		if !g.ready {
			g.pendingRate = true
			return
		}
		// A new rate takes a seek, from where it plays now.
		g.seek(g.position(), true)
	case cmdLoop:
		// Looping starts at the next end, which seeks back with a segment
		// seek; from then on each lap ends with SEGMENT_DONE instead of
		// EOS, and starts over without a gap.
		g.loop = c.on
	}
}

// seek jumps to pos; an inexact seek goes to the nearest keyframe.
func (g *gstPlayer) seek(pos time.Duration, exact bool) {
	// Drop an end the old position reached: it's no longer true.
	for C.wz_pop(&g.pipe, msgEOS|msgSegmentDone) != 0 {
	}
	flags := seekFlush | seekKeyUnit | seekSnapNearest
	if exact {
		flags = seekFlush | seekAccurate
	}
	if g.loop {
		flags |= seekSegment
	}
	C.wz_seek(&g.pipe, C.double(g.rate), C.int(flags), C.int64_t(max(0, pos)))
	g.ended = false
	g.preroll = g.paused
}

// position returns where the pipeline plays, or 0.
func (g *gstPlayer) position() time.Duration {
	return time.Duration(max(0, int64(C.wz_position(&g.pipe))))
}

// end stops at the end, paused, like <video>.
func (g *gstPlayer) end() {
	g.ended, g.paused = true, true
	C.wz_set_state(&g.pipe, statePaused)
}

// poll handles the bus's messages, copies out a new frame if there is
// one, and updates the status. It reports whether the video is playing.
func (g *gstPlayer) poll() bool {
	for !g.failed {
		t := C.wz_pop(&g.pipe, msgEOS|msgError|msgSegmentDone|msgAsyncDone)
		if t == 0 {
			break
		}
		switch t {
		case msgError:
			// A missing decoder too: the app then offers the system's player.
			g.failed = true
		case msgAsyncDone:
			if !g.ready {
				g.prerolled()
			}
			g.preroll = g.paused
		case msgEOS:
			if g.loop {
				g.seek(0, true)
			} else {
				g.end()
			}
		case msgSegmentDone:
			if g.loop {
				// Without a flush, the next lap queues right behind this one.
				C.wz_seek(&g.pipe, C.double(g.rate), seekAccurate|seekSegment, 0)
			} else {
				g.end()
			}
		}
	}

	var st Status
	if g.failed {
		st.Err = errPlay
		g.p.setStatus(st)
		return false
	}
	st.Ready = g.ready
	st.Paused, st.Ended = g.paused, g.ended
	if g.ready {
		if d := int64(C.wz_duration(&g.pipe)); d > 0 {
			st.Dur = time.Duration(d)
		}
		st.Pos = g.position()
		if g.ended && st.Dur > 0 {
			st.Pos = st.Dur
		}
		if !g.p.audio {
			st.Size = g.native
			g.frame()
		}
	}
	g.p.setStatus(st)
	return !st.Paused && !st.Ended
}

// prerolled runs once the first frame is ready: the size and length are
// known, and what was asked for before can be done.
func (g *gstPlayer) prerolled() {
	g.ready = true
	if !g.p.audio {
		var w, h C.int
		if C.wz_native(&g.pipe, &w, &h) != 0 {
			g.native = image.Pt(int(w), int(h))
		}
	}
	switch {
	case g.pendingSeek:
		g.seek(g.seekPos, g.seekExact)
	case g.pendingRate:
		g.seek(g.position(), true)
	}
	g.pendingSeek, g.pendingRate = false, false
}

// frame scales the frames to the size the app asked for, and copies out a
// new one.
func (g *gstPlayer) frame() {
	if sz := g.p.frameSize(g.native); sz != (image.Point{}) && sz != g.size {
		C.wz_set_size(&g.pipe, C.int(sz.X), C.int(sz.Y))
		g.size = sz
	}
	var w, h C.int
	sample := C.wz_pull(&g.pipe, boolInt(g.preroll), &w, &h)
	if sample == nil {
		return
	}
	g.preroll = false
	buf := g.p.buffer(image.Pt(int(w), int(h)))
	if C.wz_copy(sample, (*C.uint8_t)(unsafe.Pointer(&buf.Pix[0])), C.int(buf.Stride), w, h) != 0 {
		g.p.publish(buf)
	}
}

func boolInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
