// Command wazzap is a lightweight native WhatsApp desktop client.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

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
	flag.Parse()

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
