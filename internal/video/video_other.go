//go:build !windows

package video

// start has no backend here yet: videos open in the system's player app.
func start(p *Player, path string) error { return ErrUnsupported }
