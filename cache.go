package bergamot

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bradenaw/juniper/stream"
	"github.com/bradenaw/juniper/xslices"
	"github.com/bradenaw/juniper/xsync"
)

// ReadThrough is a cache used to store keys and values in memory paired with read-through and
// population coordination.
//
// A common problem with caches is the thundering herd. A naive usage of a cache will check the
// cache for a given key and on a miss will go fetch the authoritative data from wherever it's
// stored. Unfortunately, many readers may ask for the same non-resident key simultaneously, and
// this is likely for a key that suddenly becomes popular. Many goroutines will then all ask the
// authoritative store for the value at once.
//
// ReadThrough coordinates populates to lessen this problem. If one goroutine asks for a key, and another
// goroutine asks for the same key at the same time, the second will simply wait for the populate by
// the first to finish and receive the same value.
type ReadThrough[K comparable, V any] struct {
	// Used for atomicity between cache misses and waiters populates. Deletes from waiters must hold
	// in W, interactions with cache+waiters can hold in R.
	m sync.RWMutex

	cache   Cache[K, V]
	waiters syncMap[K, *future[V]]
	reqs    chan request[K, V]
	hits    uint64
	misses  uint64

	bg *xsync.Group
}

// Cache is an in-memory cache. This holds the actual keys and values, and most importantly
// implements the eviction policy.
//
// There are several provided implementations of Cache. If you're unsure of which to use, CAR is a
// good default.
type Cache[K any, V any] interface {
	// Put adds the given key and value to the Cache, possibly evicting another key.
	Put(K, V)
	// Get returns the value associated with the given key, or false in the second return if the key
	// is not resident.
	Get(K) (V, bool)
	// Forget removes the given key from the cache.
	Forget(K)
}

// NewReadThrough returns a ReadThrough that stores items in cache and reads-through using fetch on
// misses. It has fetchParallelism background goroutines that will call fetch for each miss in Get
// and GetBatch.
func NewReadThrough[K comparable, V any](
	fetch func(ctx context.Context, key K) (V, error),
	fetchParallelism int,
	cache Cache[K, V],
) *ReadThrough[K, V] {
	rt := &ReadThrough[K, V]{
		cache: cache,
		reqs:  make(chan request[K, V]),
		bg:    xsync.NewGroup(context.Background()),
	}

	for i := 0; i < fetchParallelism; i++ {
		rt.bg.Once(func(ctx context.Context) {
			for {
				var req request[K, V]
				select {
				case <-ctx.Done():
					return
				case req = <-rt.reqs:
				}

				value, err := fetch(ctx, req.key)
				if err != nil {
					req.future.Err(err)
				} else {
					req.future.Fill(value)
					rt.cache.Put(req.key, value)
				}
				rt.m.Lock()
				rt.waiters.Delete(req.key)
				rt.m.Unlock()
			}
		})
	}

	return rt
}

// NewBatchFetchReadThrough returns a ReadThrough that uses cache as its cache storage. It has
// fetchParallelism background goroutines used to call fetchBatch to fetch for each miss in Get and
// GetBatch. On a miss, it will wait for up to batchInterval for other misses before calling
// fetchBatch for up to batchSize of them at once.
func NewBatchFetchReadThrough[K comparable, V any](
	fetchBatch func(ctx context.Context, batch []K) ([]V, error),
	fetchParallelism int,
	batchInterval time.Duration,
	batchSize int,
	cache Cache[K, V],
) *ReadThrough[K, V] {
	rt := &ReadThrough[K, V]{
		cache: cache,
		reqs:  make(chan request[K, V]),
		bg:    xsync.NewGroup(context.Background()),
	}

	batches := make(chan []request[K, V])

	rt.bg.Once(func(ctx context.Context) {
		batchStream := stream.Batch(stream.Chan(rt.reqs), batchInterval, batchSize)
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
		rt.bg.Once(func(ctx context.Context) {
			for {
				var batch []request[K, V]
				select {
				case <-ctx.Done():
					return
				case batch = <-batches:
				}

				values, err := fetchBatch(
					ctx,
					xslices.Map(batch, func(req request[K, V]) K { return req.key }),
				)
				if err != nil {
					for _, req := range batch {
						req.future.Err(err)
					}
				} else {
					for i, req := range batch {
						req.future.Fill(values[i])
						rt.cache.Put(req.key, values[i])
					}
				}
				rt.m.Lock()
				for _, req := range batch {
					rt.waiters.Delete(req.key)
				}
				rt.m.Unlock()
			}
		})
	}

	return rt
}

// TryGet tries to get key from the cache. It returns false in the second return immediately if key
// isn't currently resident in the cache, and does not try to populate.
func (rt *ReadThrough[K, V]) TryGet(key K) (V, bool) {
	value, ok := rt.cache.Get(key)
	rt.mark(ok)
	return value, ok
}

// Get gets key from the cache. If key is not currently in the cache, causes the cache to fetch and
// populate it.
func (rt *ReadThrough[K, V]) Get(ctx context.Context, key K) (V, error) {
	rt.m.RLock()
	v, ok := rt.cache.Get(key)
	rt.mark(ok)
	if ok {
		rt.m.RUnlock()
		return v, nil
	}
	w, alreadyExisted := rt.waiters.LoadOrStoreFunc(key, newFuture[V])
	rt.m.RUnlock()
	if !alreadyExisted {
		select {
		case <-ctx.Done():
			var zero V
			return zero, ctx.Err()
		case rt.reqs <- request[K, V]{key: key, future: w}:
		}
	}
	return w.waitContext(ctx)
}

// Get gets a set of keys from the cache at once. If any key in keys is not currently in the cache,
// causes the cache to fetch and populate them.
func (rt *ReadThrough[K, V]) GetBatch(ctx context.Context, keys []K) ([]V, error) {
	values := make([]V, len(keys))
	var misses []K
	var missIdxs []int

	rt.m.RLock()
	for i, key := range keys {
		var ok bool
		values[i], ok = rt.cache.Get(keys[i])
		if !ok {
			misses = append(misses, key)
			missIdxs = append(missIdxs, i)
		}
	}
	rt.markMany(len(keys)-len(misses), len(misses))

	futures := make([]*future[V], len(misses))
	alreadyExisted := make([]bool, len(misses))
	for i, key := range misses {
		futures[i], alreadyExisted[i] = rt.waiters.LoadOrStoreFunc(key, newFuture[V])
	}
	rt.m.RUnlock()
	for i := range alreadyExisted {
		if !alreadyExisted[i] {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case rt.reqs <- request[K, V]{key: misses[i], future: futures[i]}:
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
func (rt *ReadThrough[K, V]) Put(key K, value V) {
	rt.cache.Put(key, value)
}

// Forget removes key from the cache immediately.
func (rt *ReadThrough[K, V]) Forget(key K) {
	rt.cache.Forget(key)
}

// HitRate returns the hit rate of the cache: the number of times a 'Get' asked for a key that was
// resident in the cache over the total number of requests for keys.
func (rt *ReadThrough[K, V]) HitRate() float64 {
	hits := atomic.LoadUint64(&rt.hits)
	misses := atomic.LoadUint64(&rt.misses)
	return float64(hits) / (float64(hits) + float64(misses))
}

// Close cleans up any background resources in use by the cache. It is invalid to call any methods
// on rt after calling Close.
func (rt *ReadThrough[K, V]) Close() {
	rt.bg.Wait()
}

func (rt *ReadThrough[K, V]) mark(hit bool) {
	if hit {
		atomic.AddUint64(&rt.hits, 1)
	} else {
		atomic.AddUint64(&rt.misses, 1)
	}
}

func (rt *ReadThrough[K, V]) markMany(hits int, misses int) {
	atomic.AddUint64(&rt.hits, uint64(hits))
	atomic.AddUint64(&rt.misses, uint64(misses))
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
