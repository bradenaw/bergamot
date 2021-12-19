//go:build go1.18

package gokart

import (
	"sync"
	"sync/atomic"

	"github.com/bradenaw/juniper/container/xlist"
)

type vAndRef[V any] struct {
	v   V
	ref uint32
}

type Cache[K comparable, V any] struct {
	m sync.RWMutex

	// Size of the cache, in number of items. We additionally keep this many keys worth of eviction
	// history.
	//
	// So:
	//   shortTermTargetSize <= size
	//   shortTerm.Len() + longTerm.Len() <= size
	//   shortTermHistory.Len() + longTermHistory() <= size
	size int

	//        Put() if not in history                      Put() if in history (recently evicted) //
	//                  │                                                       │                 //
	//                  v                                                       v                 //
	//            ┌───────────┐    promote when ref=1                     ┌──────────┐            //
	//            │ shortTerm ├──────────────────────────────────────────>│ longTerm │            //
	//            └─────┬─────┘                                           └─────┬────┘            //
	//                  │ evict when ref=0                                      │ evict           //
	//                  v                                                       v                 //
	//  ┌──────────────────────────────┐                         ┌─────────────────────────────┐  //
	//  │ shortTermHistory (keys only) │                         │ longTermHistory (keys only) │  //
	//  └───────────────┬──────────────┘                         └──────────────┬──────────────┘  //
	//                  │                                                       │ evict           //
	//                  v                                                       v                 //
	//              [removed]                                               [removed]             //

	// Items we think have short-term utility. On a Put(), if k isn't in history anywhere, then
	// it gets added here.
	shortTerm mapRing[K, vAndRef[V]]
	// Items that got recently evicted from shortTerm and weren't referenced.
	shortTermHistory mapList[K, struct{}]
	// Items we think have some long-term utility, judging by the fact that they got referenced in
	// shortTerm before it was time to evict them.
	longTerm mapRing[K, vAndRef[V]]
	// Items that got recently evicted from longTerm.
	longTermHistory mapList[K, struct{}]

	// The target size of shortTerm. Always <=size. Only guides evictions, which is when we actually
	// adjust the relative sizes of shortTerm and longTerm.
	shortTermTargetSize int
}

func NewCache[K comparable, V any](size int) *Cache[K, V] {
	return &Cache[K, V]{
		size: size,
		shortTerm:        newMapRing[K, vAndRef[V]](size / 2),
		shortTermHistory: newMapList[K, struct{}](size / 2),
		longTerm:         newMapRing[K, vAndRef[V]](size / 2),
		longTermHistory:  newMapList[K, struct{}](size / 2),
	}
}

func (c *Cache[K, V]) Get(k K) (V, bool) {
	c.m.RLock()
	defer c.m.RUnlock()

	node, ok := c.shortTerm.Get(k)
	if ok {
		atomic.StoreUint32(&node.Value.v.ref, 1)
		return node.Value.v.v, true
	}
	node, ok = c.longTerm.Get(k)
	if ok {
		atomic.StoreUint32(&node.Value.v.ref, 1)
		return node.Value.v.v, true
	}
	var zero V
	return zero, false
}

func (c *Cache[K, V]) Put(k K, v V) {
	c.m.Lock()
	defer c.m.Unlock()

	node, ok := c.shortTerm.Get(k)
	if ok {
		node.Value.v.v = v
		return
	}
	node, ok = c.longTerm.Get(k)
	if ok {
		node.Value.v.v = v
		return
	}

	kInShortTermHistory := c.shortTermHistory.Contains(k)
	kInLongTermHistory := c.longTermHistory.Contains(k)

	if c.shortTerm.Len()+c.longTerm.Len() >= c.size {
		// Cache is full, so we need to evict something. This will move one item from one of the
		// main lists to one of the history lists (and might do some other shuffling between
		// shortTerm and longTerm in the mean time).
		c.evictLocked()

		// If k appears in history we're going to remove it below, so we don't need to evict
		// anything from history.
		if !(kInShortTermHistory || kInLongTermHistory) {
			if c.shortTermHistory.Len() >= c.size-c.shortTerm.Len() {
				c.shortTermHistory.Remove(c.shortTermHistory.Front())
			} else if c.longTermHistory.Len() >= c.size-c.shortTermHistory.Len() {
				c.longTermHistory.Remove(c.longTermHistory.Front())
			}
		}
	}

	if kInShortTermHistory {
		// If k appears in shortTermHistory that means we evicted it from shortTerm recently, which
		// means we'd probably benefit from shortTerm being a little larger. Maybe we'd still have k
		// around right now.
		//
		// Why by this ratio? Not sure yet. It appears that way in the paper but with no description
		// about why.
		c.shortTermTargetSize = minInt(
			c.shortTermTargetSize+maxInt(1, c.longTermHistory.Len()/c.shortTermHistory.Len()),
			c.size,
		)
		c.shortTermHistory.Delete(k)
		// Send straight to longTerm, since that's what we would've done if we hadn't evicted before
		// this access.
		c.longTerm.PushBack(k, vAndRef[V]{v: v, ref: 0})
	} else if kInLongTermHistory {
		// If k appears in longTerm history that means we evicted it from longTerm recently, which
		// means we'd probably benefit from longTerm being a little larger.
		c.shortTermTargetSize = maxInt(
			c.shortTermTargetSize-maxInt(0, c.shortTermHistory.Len()/c.longTermHistory.Len()),
			0,
		)
		c.longTermHistory.Delete(k)
		c.longTerm.PushBack(k, vAndRef[V]{v: v, ref: 0})
	} else {
		// We've not recently evicted k as far as we know, so assume shortTerm.
		c.shortTerm.PushBack(k, vAndRef[V]{v: v, ref: 0})
	}
}

func (c *Cache[K, V]) evictLocked() {
	for {
		if c.shortTerm.Len() >= maxInt(1, c.shortTermTargetSize) {
			// shortTerm is larger than its target size, so prefer to remove from it.

			// shortTerm.Curr() is the least-recently-added item.
			node := c.shortTerm.Curr()
			c.shortTerm.Remove(c.shortTerm.Curr())

			// Was this node referenced since it got inserted?
			oldRef := atomic.LoadUint32(&node.Value.v.ref)

			if oldRef == 0 {
				// If not, just move it to history. Now c.shortTerm.Len()+c.longTerm.Len()<size, and
				// we're done.
				c.shortTermHistory.PushBack(node.Value.k, struct{}{})
				return
			} else {
				// If so, move it to longTerm, and continue looking for a victim.
				c.longTerm.PushBack(node.Value.k, vAndRef[V]{v: node.Value.v.v, ref: 0})
			}
		} else {
			// shortTerm is smaller than its target size, so we evict from longTerm.
			// This is done using a CLOCK, which approximates LRU but with lower contention.
			// Walk the items in longTerm in a ring until we see one with ref==0, and mark each item
			// with ref=0 as we go. We're guaranteed to find one even if that means walking all the
			// way back to where we started.
			node := c.longTerm.Curr()
			c.longTerm.Advance()
			oldRef := atomic.SwapUint32(&node.Value.v.ref, 0)
			if oldRef == 0 {
				// Move it to history, so now c.shortTerm.Len()+c.longTerm.Len()<size, and we're
				// done.
				c.longTerm.Remove(node)
				c.longTermHistory.PushBack(node.Value.k, struct{}{})
				return
			}
		}
	}
}

func (c *Cache[K, V]) Forget(k K) {
	c.m.Lock()
	defer c.m.Unlock()
	c.shortTerm.Delete(k)
	c.shortTermHistory.Delete(k)
	c.longTerm.Delete(k)
	c.longTermHistory.Delete(k)
}

type kvPair[K any, V any] struct {
	k K
	v V
}

type mapRing[K comparable, V any] struct {
	curr *xlist.Node[kvPair[K, V]]
	ml   mapList[K, V]
}

func newMapRing[K comparable, V any](n int) mapRing[K, V] {
	return mapRing[K, V]{
		ml: newMapList[K, V](n),
	}
}

func (r *mapRing[K, V]) Len() int                        { return r.ml.Len() }
func (r *mapRing[K, V]) Curr() *xlist.Node[kvPair[K, V]] { return r.curr }
func (r *mapRing[K, V]) Advance() {
	if r.curr == nil {
		r.curr = r.ml.Front()
		return
	}
	r.curr = r.curr.Next()
	if r.curr == nil {
		r.curr = r.ml.Front()
	}
}
func (r *mapRing[K, V]) PushBack(k K, v V) {
	if r.Len() == 0 {
		r.curr = r.ml.PushBack(k, v)
	} else {
		r.ml.InsertBefore(k, v, r.curr)
	}
}
func (r *mapRing[K, V]) Get(k K) (*xlist.Node[kvPair[K, V]], bool) {
	return r.ml.Get(k)
}
func (r *mapRing[K, V]) Contains(k K) bool {
	return r.ml.Contains(k)
}
func (r *mapRing[K, V]) Delete(k K) {
	if r.curr.Value.k == k {
		r.Advance()
	}
	r.ml.Delete(k)
	if r.Len() == 0 {
		r.curr = nil
	}
}
func (r *mapRing[K, V]) Remove(node *xlist.Node[kvPair[K, V]]) {
	if r.curr == node {
		r.Advance()
	}
	r.ml.Remove(node)
	if r.Len() == 0 {
		r.curr = nil
	}
}

type mapList[K comparable, V any] struct {
	l xlist.List[kvPair[K, V]]
	m map[K]*xlist.Node[kvPair[K, V]]
}

func newMapList[K comparable, V any](n int) mapList[K, V] {
	return mapList[K, V]{
		m: make(map[K]*xlist.Node[kvPair[K, V]], n),
	}
}

func (ml *mapList[K, V]) Len() int                         { return ml.l.Len() }
func (ml *mapList[K, V]) Front() *xlist.Node[kvPair[K, V]] { return ml.l.Front() }

func (ml *mapList[K, V]) PushBack(k K, v V) *xlist.Node[kvPair[K, V]] {
	ml.Delete(k)
	node := ml.l.PushBack(kvPair[K, V]{k, v})
	ml.m[k] = node
	return node
}

func (ml *mapList[K, V]) InsertBefore(
	k K,
	v V,
	mark *xlist.Node[kvPair[K, V]],
) *xlist.Node[kvPair[K, V]] {
	ml.Delete(k)
	node := ml.l.InsertBefore(kvPair[K, V]{k, v}, mark)
	ml.m[k] = node
	return node
}

func (ml *mapList[K, V]) Get(k K) (*xlist.Node[kvPair[K, V]], bool) {
	node, ok := ml.m[k]
	return node, ok
}

func (ml *mapList[K, V]) Contains(k K) bool {
	_, ok := ml.m[k]
	return ok
}

func (ml *mapList[K, V]) Delete(k K) {
	node, ok := ml.m[k]
	if !ok {
		return
	}
	ml.Remove(node)
}

func (ml *mapList[K, V]) Remove(node *xlist.Node[kvPair[K, V]]) {
	ml.l.Remove(node)
	delete(ml.m, node.Value.k)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
