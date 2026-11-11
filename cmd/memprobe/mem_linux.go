package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/memtrim"
)

// memory returns the nearest Linux has to Windows' numbers, in bytes: the
// process's private resident memory, all of its resident memory (VmRSS)
// and the memory it has to itself whether or not it's touched (VmData).
func memory() (private, ws, commit uint64) {
	data, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, _ := strings.Cut(line, ":")
		kb, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		switch name {
		case "VmRSS":
			ws = kb << 10
		case "VmData":
			commit = kb << 10
		}
	}
	return memtrim.PrivateWorkingSet(), ws, commit
}

// cpuTime returns the CPU time the process has used, user and kernel.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
