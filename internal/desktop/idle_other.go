//go:build !windows

package desktop

import "time"

// IdleTime is how long the user hasn't touched the keyboard or mouse. This
// system has no way to tell, so it's always 0.
func IdleTime() time.Duration { return 0 }
