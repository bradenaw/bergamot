package bergamot

import (
	"sync"
)

// LRU is a least-recently-used eviction policy cache. It has a defined size in number of items. If
// the LRU is full when putting an item, the key that was least recently Get or Put is evicted to
// make space.
//
// LRU's methods may be called concurrently.
type LRU[K comparable, V any] struct {
	m sync.Mutex

	items mapList[K, V]
	size  int
}

var _ Cache[byte, int] = &LRU[byte, int]{}

func NewLRU[K comparable, V any](size int) *LRU[K, V] {
	return &LRU[K, V]{
		items: newMapList[K, V](size),
		size:  size,
	}
}

func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.m.Lock()
	defer c.m.Unlock()
	node, ok := c.items.Get(key)
	if !ok {
		var zero V
		return zero, false
	}
	c.items.MoveToBack(node)
	return node.Value.v, true
}

func (c *LRU[K, V]) Put(key K, value V) {
	c.m.Lock()
	defer c.m.Unlock()
	c.items.PushBack(key, value)
	if c.items.Len() > c.size {
		c.items.Remove(c.items.Front())
	}
}

func (c *LRU[K, V]) Forget(key K) {
	c.m.Lock()
	defer c.m.Unlock()
	c.items.Delete(key)
}
