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

// Cache is a Backing, used to store keys and values in memory, paired with read-through and
// population coordination.
//
// A common problem with caches is the thundering herd. A naive usage of a cache will check the
// cache for a given key and on a miss will go fetch the authoritative data from wherever it's
// stored. Unfortunately, many readers may ask for the same non-resident key simultaneously, and
// this is likely for a key that suddenly becomes popular. Many goroutines will then all ask the
// authoritative store for the value at once.
//
// Cache coordinates populates to lessen this problem. If one goroutine asks for a key, and another
// goroutine asks for the same key at the same time, the second will simply wait for the populate by
// the first to finish and receive the same value.
type Cache[K comparable, V any] struct {
	// Used for atomicity between cache misses and waiters populates. Deletes from waiters must hold
	// in W, interactions with backing+waiters can hold in R.
	m sync.RWMutex

	backing Backing[K, V]
	waiters syncMap[K, *future[V]]
	reqs    chan request[K, V]
	hits    uint64
	misses  uint64

	bg *xsync.Group
}

// Backing is the in-memory backing storage for a Cache. This holds the actual keys and values, and
// most importantly implements the eviction policy.
//
// Backings are usable directly, but it's usually desirable to wrap a Backing in a Cache.
//
// There are several provided implementations of Backing. If you're unsure of which to use, CAR is a
// good default.
type Backing[K any, V any] interface {
	// Put adds the given key and value to the Backing, possibly evicting another key.
	Put(K, V)
	// Get returns the value associated with the given key, or false in the second return if the key
	// is not resident.
	Get(K) (V, bool)
	// Forget removes the given key from the cache.
	Forget(K)
}

// NewCache returns a Cache that uses backing as its backing store. It has fetchParallelism
// background goroutines that will call fetch for each miss in Get and GetBatch.
func NewCache[K comparable, V any](
	fetch func(ctx context.Context, key K) (V, error),
	fetchParallelism int,
	backing Backing[K, V],
) *Cache[K, V] {
	c := &Cache[K, V]{
		backing: backing,
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
					req.future.Err(err)
				} else {
					req.future.Fill(value)
					c.backing.Put(req.key, value)
				}
				c.m.Lock()
				c.waiters.Delete(req.key)
				c.m.Unlock()
			}
		})
	}

	return c
}

// NewBatchFetchCache returns a Cache that uses backing as its backing storage. It has
// fetchParallelism background goroutines used to call fetchBatch to fetch for each miss in Get and
// GetBatch. On a miss, it will wait for up to batchInterval for other misses before calling
// fetchBatch for up to batchSize of them at once.
func NewBatchFetchCache[K comparable, V any](
	fetchBatch func(ctx context.Context, batch []K) ([]V, error),
	fetchParallelism int,
	batchInterval time.Duration,
	batchSize int,
	backing Backing[K, V],
) *Cache[K, V] {
	c := &Cache[K, V]{
		backing: backing,
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
						req.future.Err(err)
					}
				} else {
					for i, req := range batch {
						req.future.Fill(values[i])
						c.backing.Put(req.key, values[i])
					}
				}
				c.m.Lock()
				for _, req := range batch {
					c.waiters.Delete(req.key)
				}
				c.m.Unlock()
			}
		})
	}

	return c
}

// TryGet tries to get key from the cache. It returns false in the second return immediately if key
// isn't currently resident in the cache, and does not try to populate.
func (c *Cache[K, V]) TryGet(key K) (V, bool) {
	value, ok := c.backing.Get(key)
	c.mark(ok)
	return value, ok
}

// Get gets key from the cache. If key is not currently in the cache, causes the cache to fetch and
// populate it.
func (c *Cache[K, V]) Get(ctx context.Context, key K) (V, error) {
	c.m.RLock()
	v, ok := c.backing.Get(key)
	c.mark(ok)
	if ok {
		c.m.RUnlock()
		return v, nil
	}
	w, alreadyExisted := c.waiters.LoadOrStoreFunc(key, newFuture[V])
	c.m.RUnlock()
	if !alreadyExisted {
		select {
		case <-ctx.Done():
			var zero V
			return zero, ctx.Err()
		case c.reqs <- request[K, V]{key: key, future: w}:
		}
	}
	return w.waitContext(ctx)
}

// Get gets a set of keys from the cache at once. If any key in keys is not currently in the cache,
// causes the cache to fetch and populate them.
func (c *Cache[K, V]) GetBatch(ctx context.Context, keys []K) ([]V, error) {
	values := make([]V, len(keys))
	var misses []K
	var missIdxs []int

	c.m.RLock()
	for i, key := range keys {
		var ok bool
		values[i], ok = c.backing.Get(keys[i])
		if !ok {
			misses = append(misses, key)
			missIdxs = append(missIdxs, i)
		}
	}
	c.markMany(len(keys)-len(misses), len(misses))

	futures := make([]*future[V], len(misses))
	alreadyExisted := make([]bool, len(misses))
	for i, key := range misses {
		futures[i], alreadyExisted[i] = c.waiters.LoadOrStoreFunc(key, newFuture[V])
	}
	c.m.RUnlock()
	for i := range alreadyExisted {
		if !alreadyExisted[i] {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case c.reqs <- request[K, V]{key: misses[i], future: futures[i]}:
			}
		}
	}

	for i, w := range futures {
		var err error
		values[missIdxs[i]], err = w.waitContext(ctx)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

// Put adds the given key and value to the cache, overwriting if already resident.
func (c *Cache[K, V]) Put(key K, value V) {
	c.backing.Put(key, value)
}

// Forget removes key from the cache immediately.
func (c *Cache[K, V]) Forget(key K) {
	c.backing.Forget(key)
}

// HitRate returns the hit rate of the cache: the number of times a 'Get' asked for a key that was
// resident in the cache over the total number of requests for keys.
func (c *Cache[K, V]) HitRate() float64 {
	hits := atomic.LoadUint64(&c.hits)
	misses := atomic.LoadUint64(&c.misses)
	return float64(hits) / (float64(hits) + float64(misses))
}

// Close cleans up any background resources in use by the cache. It is invalid to call any methods
// on c after calling Close.
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

type future[T any] struct {
	value T
	err   error
	wait  chan struct{}
}

func newFuture[T any]() *future[T] {
	return &future[T]{
		wait: make(chan struct{}),
	}
}

func (w *future[T]) Fill(value T) {
	w.value = value
	close(w.wait)
}

func (w *future[T]) Err(err error) {
	w.err = err
	close(w.wait)
}

func (w *future[T]) waitContext(ctx context.Context) (T, error) {
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-w.wait:
		return w.value, w.err
	}
}

type request[K comparable, V any] struct {
	key    K
	future *future[V]
}

func (req *request[K, V]) Fill(v V)      { req.future.Fill(v) }
func (req *request[K, V]) Err(err error) { req.future.Err(err) }

type syncMap[K comparable, V any] struct{ xsync.Map[K, V] }

func (m *syncMap[K, V]) LoadOrStoreFunc(key K, mkv func() V) (V, bool) {
	v, ok := m.Load(key)
	if ok {
		return v, ok
	}
	return m.LoadOrStore(key, mkv())
}
