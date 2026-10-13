//go:build !windows

package desktop

// Microphones lists the system's audio inputs; none are listed here.
func Microphones() []string { return nil }

// Speakers lists the system's audio outputs; none are listed here.
func Speakers() []string { return nil }
