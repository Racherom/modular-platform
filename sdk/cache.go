package sdk

import (
	"container/list"
	"sync"
	"time"
)

// lru holds at most max entries, each for ttl; a new key at a full cache takes the place of the
// least recently used one, as spec 0003 does for rate limit buckets.
type lru[V any] struct {
	max int
	ttl time.Duration
	now func() time.Time

	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List // of *lruItem[V]; front is the most recently used
}

type lruItem[V any] struct {
	key string
	val V
	at  time.Time
}

func newLRU[V any](maxEntries int, ttl time.Duration) *lru[V] {
	return &lru[V]{max: maxEntries, ttl: ttl, now: time.Now, items: map[string]*list.Element{}, order: list.New()}
}

// get returns key's value unless it is missing or older than ttl.
func (c *lru[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	e, ok := c.items[key]
	if !ok {
		return zero, false
	}
	it := e.Value.(*lruItem[V])
	if c.now().Sub(it.at) >= c.ttl {
		c.order.Remove(e)
		delete(c.items, key)
		return zero, false
	}
	c.order.MoveToFront(e)
	return it.val, true
}

func (c *lru[V]) put(key string, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		e.Value = &lruItem[V]{key: key, val: val, at: c.now()}
		c.order.MoveToFront(e)
		return
	}
	if c.order.Len() >= c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*lruItem[V]).key)
	}
	c.items[key] = c.order.PushFront(&lruItem[V]{key: key, val: val, at: c.now()})
}

func (c *lru[V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
	c.order.Init()
}

func (c *lru[V]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
