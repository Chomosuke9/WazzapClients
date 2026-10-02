// Command winres writes the Windows executable's resources: the icon that
// Explorer, Start and the taskbar show (the running app draws its own, the
// same icon.App) and the name in its version info. It also writes the
// installer's icon. cmd/wazzap runs it with go generate:
//
//	go generate ./cmd/wazzap
//
// The outputs are committed, so a plain go build needs nothing more.
package main

import (
	"flag"
	"image"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

func main() {
	syso := flag.String("syso", "rsrc_windows_amd64.syso", "the object file go build links in")
	ico := flag.String("ico", "../../installer/wazzap.ico", "the installer's icon")
	flag.Parse()

	// Every size Windows asks for, each drawn at its size rather than
	// scaled from a big one, so the small ones stay sharp.
	var imgs []image.Image
	for _, px := range []int{16, 20, 24, 32, 40, 48, 64, 256} {
		imgs = append(imgs, icon.App(px))
	}
	ic, err := winres.NewIconFromImages(imgs)
	if err != nil {
		log.Fatal(err)
	}

	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), ic); err != nil {
		log.Fatal(err)
	}
	var vi version.Info
	for k, v := range map[string]string{
		version.FileDescription:  "WazzapClients",
		version.ProductName:      "WazzapClients",
		version.InternalName:     "WazzapClients",
		version.OriginalFilename: "WazzapClients.exe",
	} {
		if err := vi.Set(version.LangDefault, k, v); err != nil {
			log.Fatal(err)
		}
	}
	rs.SetVersionInfo(vi)

	write(*syso, func(f *os.File) error { return rs.WriteObject(f, winres.ArchAMD64) })
	write(*ico, func(f *os.File) error { return ic.SaveICO(f) })
}

func write(name string, fn func(*os.File) error) {
	f, err := os.Create(name)
	if err != nil {
		log.Fatal(err)
	}
	if err := fn(f); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
