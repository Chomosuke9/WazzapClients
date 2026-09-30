package ui

import "testing"

func TestFollowers(t *testing.T) {
	tests := map[int]string{
		0:         "Channel",
		1:         "1 follower",
		950:       "950 followers",
		1500:      "1.5k followers",
		17400:     "17k followers",
		736100:    "736k followers",
		1_450_000: "1.4m followers",
		7_000_000: "7m followers",
	}
	for n, want := range tests {
		if got := followers(n); got != want {
			t.Errorf("followers(%d) = %q, want %q", n, got, want)
		}
	}
}
