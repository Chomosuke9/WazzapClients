//go:build linux

package memtrim

/*
#include <malloc.h>

// malloc_trim is glibc's; musl's malloc gives memory back by itself.
#ifdef __GLIBC__
static void trim(void) { malloc_trim(0); }
#else
static void trim(void) {}
#endif
*/
import "C"

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"
)

// trimWorkingSet hands the C heap's free memory back: the graphics driver
// and the video decoders allocate there, and glibc keeps what they free.
// The kernel takes untouched pages by itself when it needs them, so there
// is no working set to trim as on Windows.
func trimWorkingSet() { C.trim() }

// PrivateWorkingSet returns the memory in use that is the process's own,
// in bytes: its resident pages that no other process shares, the nearest
// thing to what Windows' Task Manager shows. It is 0 where it can't be
// read.
func PrivateWorkingSet() uint64 {
	// The kernel adds up every mapping's numbers here.
	if data, err := os.ReadFile("/proc/self/smaps_rollup"); err == nil {
		var kb uint64
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			name, rest, ok := strings.Cut(sc.Text(), ":")
			if !ok || name != "Private_Clean" && name != "Private_Dirty" {
				continue
			}
			n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			kb += n
		}
		if kb > 0 {
			return kb << 10
		}
	}
	// Older kernels: resident pages less the file-backed and shared ones.
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(data))
	if len(f) < 3 {
		return 0
	}
	rss, _ := strconv.ParseUint(f[1], 10, 64)
	shared, _ := strconv.ParseUint(f[2], 10, 64)
	if shared > rss {
		return 0
	}
	return (rss - shared) * uint64(os.Getpagesize())
}
