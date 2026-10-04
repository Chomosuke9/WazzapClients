package wa

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

func TestOutgoingTyping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		p := outgoingTyping{wake: make(chan struct{}, 1)}
		var got []string
		go p.run(ctx, func(chat string, on bool) {
			if on {
				chat += ":typing"
			} else {
				chat += ":paused"
			}
			got = append(got, chat)
		})
		report := func(chat string) { p.report(chat, time.Now()); synctest.Wait() }
		check := func(want ...string) {
			t.Helper()
			synctest.Wait()
			if !slices.Equal(got, want) {
				t.Fatalf("presence = %v, want %v", got, want)
			}
			got = nil
		}
		report("a")
		check("a:typing")
		for range 30 {
			report("a")
		}
		check() // bursts don't flood the network
		time.Sleep(2 * time.Second)
		report("a")
		check()
		time.Sleep(2 * time.Second)
		report("a")
		check("a:typing")
		time.Sleep(3*time.Second - time.Nanosecond)
		check()
		time.Sleep(time.Nanosecond)
		check("a:paused") // no UI frames are needed for idle expiry
		report("a")
		report("b")
		check("a:typing", "a:paused", "b:typing")
		report("")
		check("b:paused")
		time.Sleep(typingIdle * 2)
		check() // stopping cancels the idle timer
	})
}
