//go:build !unix && !windows

package offheap

func alloc(int) []byte { return nil }

func free([]byte) {}
