//go:build go1.18

package bergamot

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bradenaw/juniper/slices"
	"github.com/bradenaw/juniper/stream"
	"github.com/bradenaw/juniper/xsync"
)

type Cache[K comparable, V any] struct {
	// Used for atomicity between cache misses and waits populates. Deletes from waits must hold in
	// W, interactions with storage+waits can hold in R.
	m sync.RWMutex

	storage Storage[K, V]
	waits   syncMap[K, *waitable[V]]
	reqs    chan request[K, V]
	hits    uint64
	misses  uint64

	bg *xsync.Group
}

type Storage[K any, V any] interface {
	Put(K, V)
	Get(K) (V, bool)
	Forget(K)
}

type waitable[T any] struct {
	value T
	err   error
	wait  chan struct{}
}

func newWaitable[T any]() *waitable[T] {
	return &waitable[T]{
		wait: make(chan struct{}),
	}
}

func (w *waitable[T]) Set(value T) {
	w.value = value
	close(w.wait)
}

func (w *waitable[T]) Err(err error) {
	w.err = err
	close(w.wait)
}

func (w *waitable[T]) WaitContext(ctx context.Context) (T, error) {
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-w.wait:
		return w.value, w.err
	}
}

type request[K comparable, V any] struct {
	key  K
	wait *waitable[V]
}

func NewCache[K comparable, V any](
	fetch func(ctx context.Context, key K) (V, error),
	fetchParallelism int,
	storage Storage[K, V],
) *Cache[K, V] {
	c := &Cache[K, V]{
		storage: storage,
		reqs:    make(chan request[K, V]),
		bg:      xsync.NewGroup(context.Background()),
	}

	for i := 0; i < fetchParallelism; i++ {
		c.bg.Once(func(ctx context.Context) {
			for {
				var req request[K, V]
				select {
				case <-ctx.Done():
					return
				case req = <-c.reqs:
				}

				value, err := fetch(ctx, req.key)
				if err != nil {
					req.wait.Err(err)
				} else {
					req.wait.Set(value)
					c.storage.Put(req.key, value)
				}
				c.m.Lock()
				c.waits.Delete(req.key)
				c.m.Unlock()
			}
		})
	}

	return c
}

func NewBatchFetchCache[K comparable, V any](
	fetchBatch func(ctx context.Context, batch []K) ([]V, error),
	fetchParallelism int,
	batchInterval time.Duration,
	batchSize int,
	storage Storage[K, V],
) *Cache[K, V] {
	c := &Cache[K, V]{
		storage: storage,
		reqs:    make(chan request[K, V]),
		bg:      xsync.NewGroup(context.Background()),
	}

	batches := make(chan []request[K, V])

	c.bg.Once(func(ctx context.Context) {
		batchStream := stream.Batch(stream.Chan(c.reqs), batchInterval, batchSize)
		defer batchStream.Close()

		for {
			batch, err := batchStream.Next(ctx)
			if err != nil {
				// Only errors if channel is closed or context expires, either way we're good.
				return
			}
			select {
			case <-ctx.Done():
				return
			case batches <- batch:
			}
		}
	})

	for i := 0; i < fetchParallelism; i++ {
		c.bg.Once(func(ctx context.Context) {
			for {
				var batch []request[K, V]
				select {
				case <-ctx.Done():
					return
				case batch = <-batches:
				}

				values, err := fetchBatch(
					ctx,
					slices.Map(batch, func(req request[K, V]) K { return req.key }),
				)
				if err != nil {
					for _, req := range batch {
						req.wait.Err(err)
					}
				} else {
					for i, req := range batch {
						req.wait.Set(values[i])
						c.storage.Put(req.key, values[i])
					}
				}
				c.m.Lock()
				for _, req := range batch {
					c.waits.Delete(req.key)
				}
				c.m.Unlock()
			}
		})
	}

	return c
}

func (c *Cache[K, V]) TryGet(key K) (V, bool) {
	value, ok := c.storage.Get(key)
	c.mark(ok)
	return value, ok
}

func (c *Cache[K, V]) Get(ctx context.Context, key K) (V, error) {
	c.m.RLock()
	v, ok := c.storage.Get(key)
	c.mark(ok)
	if ok {
		c.m.RUnlock()
		return v, nil
	}
	w, alreadyExisted := c.waits.LoadOrStoreFunc(key, newWaitable[V])
	if !alreadyExisted {
		select {
		case <-ctx.Done():
			c.m.RUnlock()
			var zero V
			return zero, ctx.Err()
		case c.reqs <- request[K, V]{key: key, wait: w}:
		}
	}
	c.m.RUnlock()
	return w.WaitContext(ctx)
}

func (c *Cache[K, V]) GetBatch(ctx context.Context, keys []K) ([]V, error) {
	values := make([]V, len(keys))
	var misses []K
	var missIdxs []int

	c.m.RLock()
	for i, key := range keys {
		var ok bool
		values[i], ok = c.storage.Get(keys[i])
		if !ok {
			misses = append(misses, key)
			missIdxs = append(missIdxs, i)
		}
	}
	c.markMany(len(keys)-len(misses), len(misses))

	waits := make([]*waitable[V], len(misses))
	for i, key := range misses {
		var alreadyExisted bool
		waits[i], alreadyExisted = c.waits.LoadOrStoreFunc(key, newWaitable[V])
		if !alreadyExisted {
			select {
			case <-ctx.Done():
				c.m.RUnlock()
				return nil, ctx.Err()
			case c.reqs <- request[K, V]{key: key, wait: waits[i]}:
			}
		}
	}
	c.m.RUnlock()

	for i, w := range waits {
		var err error
		values[missIdxs[i]], err = w.WaitContext(ctx)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (c *Cache[K, V]) Put(key K, value V) {
	c.storage.Put(key, value)
}

func (c *Cache[K, V]) Forget(key K) {
	c.storage.Forget(key)
}

func (c *Cache[K, V]) HitRate() float64 {
	hits := atomic.LoadUint64(&c.hits)
	misses := atomic.LoadUint64(&c.misses)
	return float64(hits) / (float64(hits) + float64(misses))
}

func (c *Cache[K, V]) Close() {
	c.bg.Wait()
}

func (c *Cache[K, V]) mark(hit bool) {
	if hit {
		atomic.AddUint64(&c.hits, 1)
	} else {
		atomic.AddUint64(&c.misses, 1)
	}
}

func (c *Cache[K, V]) markMany(hits int, misses int) {
	atomic.AddUint64(&c.hits, uint64(hits))
	atomic.AddUint64(&c.misses, uint64(misses))
}

type syncMap[K comparable, V any] struct{ xsync.Map[K, V] }

func (m *syncMap[K, V]) LoadOrStoreFunc(key K, mkv func() V) (V, bool) {
	v, ok := m.Load(key)
	if ok {
		return v, ok
	}
	return m.LoadOrStore(key, mkv())
}
