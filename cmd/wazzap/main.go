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

	"gioui.org/app"
	"gioui.org/unit"

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
	flag.Parse()
	// A chat app idles most of the time; trade a little CPU during bursts
	// (history sync) for a smaller heap.
	rdebug.SetGCPercent(50)
	if *pprofAddr != "" {
		go func() { log.Println(http.ListenAndServe(*pprofAddr, nil)) }()
	}

	var backend model.Backend
	if *demo {
		backend = mock.New()
	} else {
		b, err := wa.Open(*dataDir, *debug)
		if err != nil {
			log.Fatal(err)
		}
		backend = b
	}

	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("WazzapClients"),
			app.Size(unit.Dp(1200), unit.Dp(780)),
			app.MinSize(unit.Dp(760), unit.Dp(500)),
			// The UI draws its own WhatsApp-style title bar.
			app.Decorated(false),
		)
		if err := ui.Run(w, backend); err != nil {
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
