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
	"time"

	"gioui.org/app"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui"
	"github.com/chomosuke9/wazzapclients/internal/update"
	"github.com/chomosuke9/wazzapclients/internal/wa"
)

// version is the release this build is, set by the release build
// (-ldflags "-X main.version=v1.2.3"). Builds without one don't update.
var version = ""

func main() {
	demo := flag.Bool("demo", false, "show demo chats instead of connecting to WhatsApp")
	debug := flag.Bool("debug", false, "verbose protocol logging")
	dataDir := flag.String("data", defaultDataDir(), "directory for the session database and logs")
	pprofAddr := flag.String("pprof", "", "serve runtime profiles on this localhost address (debugging)")
	background := flag.Bool("background", false, "start in the notification area, without a window (used at login)")
	// COM adds -Embedding (or /Embedding) when a notification click
	// starts the app.
	embedding := flag.Bool("Embedding", false, "started by a notification click (set by Windows)")
	waitPID := flag.Int("wait-pid", 0, "wait for this process to exit first (set by an update's restart)")
	flag.Parse()
	*embedding = *embedding || slices.Contains(flag.Args(), "/Embedding")
	// A chat app idles most of the time; trade a little CPU during bursts
	// (history sync) for a smaller heap. No memory limit: a long session's
	// live heap can come near any fixed one, and then the GC runs back to
	// back on several cores, every frame of a scroll (see memprobe
	// -ballast). Idle trims (memtrim) give the memory back instead.
	rdebug.SetGCPercent(50)
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
	if *waitPID != 0 {
		desktop.WaitExit(*waitPID, 15*time.Second)
	}
	if !desktop.Lock(lockDir) {
		return
	}
	if exe, err := os.Executable(); err == nil && version != "" {
		update.Cleanup(exe)
	}

	// Each linked account has a data directory of its own (see accounts);
	// the open one's backend runs.
	root := *dataDir
	open := func(dir string) (model.Backend, error) { return wa.Open(dir, *debug) }
	if *demo {
		root = demoDir
		open = func(dir string) (model.Backend, error) { return demoAccount(root, dir), nil }
	}
	list := accounts.Load(root)
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
		Accounts:  list,
		Open:      open,
		Version:   version,
	}
	if *demo {
		opts.NotifyDir = demoDir
	} else {
		opts.Relaunch = []string{}
		if *dataDir != defaultDataDir() {
			opts.Relaunch = []string{"-data", *dataDir}
		}
	}
	backend, err := open(list.Path(list.Active))
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		if err := ui.Run(backend, opts); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

// demoAccount is the demo backend of an account's directory: the usual
// demo data, under another name for the accounts added after the first.
func demoAccount(root, dir string) model.Backend {
	if filepath.Clean(dir) == filepath.Clean(root) {
		return mock.New()
	}
	n := filepath.Base(dir)
	return mock.NewAccount("Demo account "+n, "+1 555 010"+n, "1555010"+n+"@s.whatsapp.net")
}

func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "wazzap-data"
	}
	return filepath.Join(dir, "WazzapClients")
}
