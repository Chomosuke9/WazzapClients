// Command wazzap is a lightweight native WhatsApp desktop client.
package main

import (
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/ui"
)

func main() {
	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("WazzapClients"),
			app.Size(unit.Dp(1200), unit.Dp(780)),
			app.MinSize(unit.Dp(760), unit.Dp(500)),
		)
		if err := ui.Run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
