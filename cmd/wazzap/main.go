// Command wazzap is a lightweight native WhatsApp desktop client.
package main

import (
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	rdebug "runtime/debug"
	"slices"

	"gioui.org/app"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui"
	"github.com/chomosuke9/wazzapclients/internal/wa"
)

func main() {
	demo := flag.Bool("demo", false, "show demo chats instead of connecting to WhatsApp")
	debug := flag.Bool("debug", false, "verbose protocol logging")
	dataDir := flag.String("data", defaultDataDir(), "directory for the session database and logs")
	pprofAddr := flag.String("pprof", "", "serve runtime profiles on this localhost address (debugging)")
	background := flag.Bool("background", false, "start in the notification area, without a window (used at login)")
	// COM adds -Embedding (or /Embedding) when a notification click
	// starts the app.
	embedding := flag.Bool("Embedding", false, "started by a notification click (set by Windows)")
	flag.Parse()
	*embedding = *embedding || slices.Contains(flag.Args(), "/Embedding")
	// A chat app idles most of the time; trade a little CPU during bursts
	// (history sync) for a smaller heap.
	rdebug.SetGCPercent(50)
	// A soft cap: near it the GC runs more often and returns memory to the
	// OS. The live heap is well below it; GOMEMLIMIT overrides it.
	if os.Getenv("GOMEMLIMIT") == "" {
		rdebug.SetMemoryLimit(96 << 20)
	}
	if *pprofAddr != "" {
		go func() { log.Println(http.ListenAndServe(*pprofAddr, nil)) }()
	}

	// One instance per data directory: a second launch shows the first
	// one's window and exits.
	demoDir := filepath.Join(os.TempDir(), "WazzapClients-demo")
	lockDir := *dataDir
	if *demo {
		lockDir = demoDir
	}
	if !desktop.Lock(lockDir) {
		return
	}

	var backend model.Backend
	opts := ui.Options{
		Window: []app.Option{
			app.Title("WazzapClients"),
			app.Size(unit.Dp(1200), unit.Dp(780)),
			app.MinSize(unit.Dp(760), unit.Dp(500)),
			// The UI draws its own WhatsApp-style title bar.
			app.Decorated(false),
		},
		Hidden:    *background || *embedding,
		NotifyDir: filepath.Join(*dataDir, "notifications"),
	}
	if *demo {
		backend = mock.New()
		opts.NotifyDir = demoDir
	} else {
		opts.Relaunch = []string{}
		if *dataDir != defaultDataDir() {
			opts.Relaunch = []string{"-data", *dataDir}
		}
		b, err := wa.Open(*dataDir, *debug)
		if err != nil {
			log.Fatal(err)
		}
		backend = b
	}

	go func() {
		if err := ui.Run(backend, opts); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "wazzap-data"
	}
	return filepath.Join(dir, "WazzapClients")
}
