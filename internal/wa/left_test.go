package wa

import (
	"context"
	"testing"
)

// TestLeftGroup checks that a group you left is marked so, whether you
// left it while connected or while offline, and that rejoining clears it.
func TestLeftGroup(t *testing.T) {
	ctx := context.Background()
	s, db := testStore(t)
	for _, j := range []string{"a@g.us", "b@g.us"} {
		if err := s.ensureChat(ctx, db, j, true, j); err != nil {
			t.Fatal(err)
		}
	}
	left := func(j string) bool {
		c, ok := s.chat(ctx, j)
		if !ok {
			t.Fatalf("no chat %s", j)
		}
		return c.Left
	}
	s.setLeft(ctx, "a@g.us", true)
	if !left("a@g.us") || left("b@g.us") {
		t.Errorf("after leaving a: a left %v, b left %v", left("a@g.us"), left("b@g.us"))
	}
	// Back in a, and out of b while offline.
	s.keepMembers(ctx, []string{"a@g.us"})
	if left("a@g.us") || !left("b@g.us") {
		t.Errorf("after the joined groups: a left %v, b left %v", left("a@g.us"), left("b@g.us"))
	}
	// A failed fetch lists no groups, which marks none left.
	s.keepMembers(ctx, nil)
	if left("a@g.us") {
		t.Error("a failed fetch marked a group left")
	}
}
