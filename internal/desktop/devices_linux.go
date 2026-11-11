//go:build linux

package desktop

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// Microphones lists the names of the system's audio inputs, as
// PulseAudio or PipeWire (through pactl) describes them. A system with
// neither lists none.
func Microphones() []string { return pulseDevices("sources") }

// Speakers lists the names of the system's audio outputs.
func Speakers() []string { return pulseDevices("sinks") }

func pulseDevices(kind string) []string {
	out, err := exec.Command("pactl", "--format=json", "list", kind).Output()
	if err != nil {
		return nil
	}
	var devs []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if json.Unmarshal(out, &devs) != nil {
		return nil
	}
	var names []string
	for _, d := range devs {
		// Every output has a "monitor" input that records what it plays:
		// not a microphone.
		if kind == "sources" && strings.HasSuffix(d.Name, ".monitor") {
			continue
		}
		if name := strings.TrimSpace(d.Description); name != "" {
			names = append(names, name)
		} else if d.Name != "" {
			names = append(names, d.Name)
		}
	}
	return names
}
