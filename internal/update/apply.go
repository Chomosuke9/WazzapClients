package update

import (
	"errors"
	"os"
	"runtime"
)

// Staged is where Download puts the new executable for exe.
func Staged(exe string) string { return exe + ".new" }

// old is where Apply moves the running executable on Windows.
func old(exe string) string { return exe + ".old" }

// Apply puts the executable staged next to exe (see Staged) in its place.
// The running process goes on with the old code; start exe again to run
// the new one.
//
// Windows won't overwrite or delete a running executable, but it renames
// one: the old file moves aside to exe+".old", which Cleanup removes once
// it's no longer running. Elsewhere a rename replaces the file in one step.
func Apply(exe string) error {
	staged := Staged(exe)
	if _, err := os.Stat(staged); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(staged, exe)
	}
	os.Remove(old(exe)) // left by the update before, if it's not in use
	if err := os.Rename(exe, old(exe)); err != nil {
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		// Put the running one back, so the app still starts.
		return errors.Join(err, os.Rename(old(exe), exe))
	}
	return nil
}

// Cleanup removes what an update left next to exe: the old executable,
// and a download that never got applied.
func Cleanup(exe string) {
	os.Remove(old(exe))
	os.Remove(Staged(exe))
}
