package sdk

import (
	"testing"
	"time"
)

// TestLRU covers the per caller cache: at most max entries, the least recently used dropped
// first, each entry kept for ttl, and clear dropping them all.
func TestLRU(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	c := newLRU[int](2, 30*time.Second)
	c.now = func() time.Time { return now }

	c.put("a", 1)
	c.put("b", 2)
	if _, ok := c.get("a"); !ok { // a is now the most recently used
		t.Fatal("a missing")
	}
	c.put("c", 3)
	if _, ok := c.get("b"); ok {
		t.Error("b kept, want it dropped as the least recently used")
	}
	if v, ok := c.get("a"); !ok || v != 1 {
		t.Errorf("a = %d %v, want 1 true", v, ok)
	}
	if c.len() != 2 {
		t.Errorf("len = %d, want 2", c.len())
	}

	now = now.Add(29 * time.Second)
	if _, ok := c.get("c"); !ok {
		t.Error("c dropped before its 30 seconds")
	}
	now = now.Add(time.Second)
	if _, ok := c.get("c"); ok {
		t.Error("c kept after its 30 seconds")
	}

	c.clear()
	if _, ok := c.get("a"); ok || c.len() != 0 {
		t.Errorf("clear kept %d entries", c.len())
	}
}
