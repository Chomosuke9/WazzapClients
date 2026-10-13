// Command mkpatch makes the patch that turns an older release's executable
// into a new one, for updates that download only what changed
// (internal/update):
//
//	go run . <old executable> <new executable> <patch>
//
// The patch is bsdiff's BSDIFF40 format. internal/update applies it with a
// bspatch of its own, on the standard library's bzip2, so the app doesn't
// link this module's dependency; mkpatch is a module of its own for that
// reason. It checks the patch by applying it before writing it.
package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/gabstv/go-bsdiff/pkg/bsdiff"
	"github.com/gabstv/go-bsdiff/pkg/bspatch"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: mkpatch <old executable> <new executable> <patch>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "mkpatch:", err)
		os.Exit(1)
	}
}

func run(oldPath, newPath, out string) error {
	old, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(newPath)
	if err != nil {
		return err
	}
	patch, err := bsdiff.Bytes(old, cur)
	if err != nil {
		return err
	}
	if got, err := bspatch.Bytes(old, patch); err != nil || !bytes.Equal(got, cur) {
		return fmt.Errorf("the patch doesn't rebuild %s (%v)", newPath, err)
	}
	if err := os.WriteFile(out, patch, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d bytes (%.1f%% of %s)\n", out, len(patch), 100*float64(len(patch))/float64(len(cur)), newPath)
	return nil
}
