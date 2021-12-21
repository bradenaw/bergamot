//go:build go1.18

package gokart

import (
	"testing"
)

func FuzzCache(f *testing.F) {
	f.Fuzz(func(t *testing.T, size byte, b []byte) {
		if size == 0 {
			return
		}

		t.Logf("size %d", size)

		populate := func(b byte) uint32 {
			return (uint32(b) * 2547493511) ^ 0x95835b12
		}

		cache := NewCache[byte, uint32](int(size))

		for i := range b {
			v, ok := cache.Get(b[i])
			exp := populate(b[i])
			if ok {
				t.Logf("Get(%#v) hit", b[i])
				if v != exp {
					t.Fatalf("got a wrong value back")
				}
				continue
			}
			t.Logf("Get(%#v) miss, Put(%#v, %08x)", b[i], b[i], exp)
			cache.Put(b[i], exp)
			logCache(t, cache)
		}
	})
}

func logCache[K comparable, V any](t *testing.T, c *Cache[K, V]) {
	t.Log("cache =================")
	t.Log("  shortTerm ===========")
	for node := c.shortTerm.ml.l.Front(); node != nil; node = node.Next() {
		pfx := "  "
		if node == c.shortTerm.curr {
			pfx = "->"
		}
		t.Logf("    %s%#v: %#v (ref %t)", pfx, node.Value.k, node.Value.v.v, node.Value.v.ref != 0)
	}
	t.Log("  shortTermHistory ====")
	for node := c.shortTermHistory.Front(); node != nil; node = node.Next() {
		t.Logf("      %#v", node.Value.k)
	}
	t.Log("  longTerm ============")
	for node := c.longTerm.ml.l.Front(); node != nil; node = node.Next() {
		pfx := "  "
		if node == c.longTerm.curr {
			pfx = "->"
		}
		t.Logf("    %s%#v: %#v (ref %t)", pfx, node.Value.k, node.Value.v.v, node.Value.v.ref != 0)
	}
	t.Log("  longTermHistory =====")
	for node := c.longTermHistory.Front(); node != nil; node = node.Next() {
		t.Logf("      %#v", node.Value.k)
	}
}
