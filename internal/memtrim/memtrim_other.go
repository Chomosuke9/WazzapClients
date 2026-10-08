//go:build !windows

package memtrim

func trimWorkingSet() {}

// PrivateWorkingSet is only known on Windows.
func PrivateWorkingSet() uint64 { return 0 }
