//go:build go1.18

package bergamot

import (
	"github.com/bradenaw/juniper/xsync"
)

// Unbounded is an unbounded-size cache backing. It will never evict keys on its own, and thus will
// grow to arbitrary size.
//
// Unbounded's methods may be called concurrently.
type Unbounded[K comparable, V any] struct {
	m xsync.Map[K, V]
}

var _ Storage[byte, int] = &Unbounded[byte, int]{}

func NewUnbounded[K comparable, V any]() *Unbounded[K, V] {
	return &Unbounded[K, V]{}
}

func (c *Unbounded[K, V]) Get(key K) (V, bool) { return c.m.Load(key) }

func (c *Unbounded[K, V]) Put(key K, value V) { c.m.Store(key, value) }

func (c *Unbounded[K, V]) Forget(key K) { c.m.Delete(key) }
