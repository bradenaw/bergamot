# `package bergamot`

```
import "github.com/bradenaw/bergamot"
```

## Overview



## Index

<samp><a href="#Backing">type Backing</a></samp>

<samp><a href="#CAR">type CAR</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#NewCAR">func NewCAR[K comparable, V any](size int) *CAR[K, V]</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Forget">func (c *CAR[K, V]) Forget(k K)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Get">func (c *CAR[K, V]) Get(k K) (V, bool)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Put">func (c *CAR[K, V]) Put(k K, v V)</a></samp>

<samp><a href="#Cache">type Cache</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#NewBatchFetchCache">func NewBatchFetchCache[K comparable, V any](
&nbsp;&nbsp;&nbsp;&nbsp;	fetchBatch func(ctx context.Context, batch []K) ([]V, error),
&nbsp;&nbsp;&nbsp;&nbsp;	fetchParallelism int,
&nbsp;&nbsp;&nbsp;&nbsp;	batchInterval time.Duration,
&nbsp;&nbsp;&nbsp;&nbsp;	batchSize int,
&nbsp;&nbsp;&nbsp;&nbsp;	backing Backing[K, V],
&nbsp;&nbsp;&nbsp;&nbsp;) *Cache[K, V]</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#NewCache">func NewCache[K comparable, V any](
&nbsp;&nbsp;&nbsp;&nbsp;	fetch func(ctx context.Context, key K) (V, error),
&nbsp;&nbsp;&nbsp;&nbsp;	fetchParallelism int,
&nbsp;&nbsp;&nbsp;&nbsp;	backing Backing[K, V],
&nbsp;&nbsp;&nbsp;&nbsp;) *Cache[K, V]</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Close">func (c *Cache[K, V]) Close()</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Forget">func (c *Cache[K, V]) Forget(key K)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Get">func (c *Cache[K, V]) Get(ctx context.Context, key K) (V, error)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#GetBatch">func (c *Cache[K, V]) GetBatch(ctx context.Context, keys []K) ([]V, error)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#HitRate">func (c *Cache[K, V]) HitRate() float64</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Put">func (c *Cache[K, V]) Put(key K, value V)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#TryGet">func (c *Cache[K, V]) TryGet(key K) (V, bool)</a></samp>

<samp><a href="#LRU">type LRU</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#NewLRU">func NewLRU[K comparable, V any](size int) *LRU[K, V]</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Forget">func (c *LRU[K, V]) Forget(key K)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Get">func (c *LRU[K, V]) Get(key K) (V, bool)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Put">func (c *LRU[K, V]) Put(key K, value V)</a></samp>

<samp><a href="#Unbounded">type Unbounded</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#NewUnbounded">func NewUnbounded[K comparable, V any]() *Unbounded[K, V]</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Forget">func (c *Unbounded[K, V]) Forget(key K)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Get">func (c *Unbounded[K, V]) Get(key K) (V, bool)</a></samp>

<samp>&nbsp;&nbsp;&nbsp;&nbsp;<a href="#Put">func (c *Unbounded[K, V]) Put(key K, value V)</a></samp>


## Constants

This section is empty.

## Variables

This section is empty.

## Functions

This section is empty.

## Types

<h3><a id="Backing"></a><samp>type Backing</samp></h3>
```go
type Backing[K any, V any] interface {
	// Put adds the given key and value to the Backing, possibly evicting another key.
	Put(K, V)
	// Get returns the value associated with the given key, or false in the second return if the key
	// is not resident.
	Get(K) (V, bool)
	// Forget removes the given key from the cache.
	Forget(K)
}
```

Backing is the in-memory backing storage for a Cache. This holds the actual keys and values, and
most importantly implements the eviction policy.

Backings are usable directly, but it's usually desirable to wrap a Backing in a Cache.

There are several provided implementations of Backing. If you're unsure of which to use, CAR is a
good default.


<h3><a id="CAR"></a><samp>type CAR</samp></h3>
```go
type CAR[K comparable, V any] struct {
	// contains filtered or unexported fields
}
```

CAR is a CLOCK-with-Adaptive-Replacement cache backing.

The eviction policy is an approximation to a combination between least-recently-used and
least-frequently-used, self-balancing resources between the two based on their relative
usefulness. Approximation allows lower lock contention

CAR's methods may be called concurrently.

https://www.usenix.org/legacy/publications/library/proceedings/fast04/tech/full_papers/bansal/bansal.pdf


<h3><a id="NewCAR"></a><samp>func NewCAR[K comparable, V any](size int) *<a href="#CAR">CAR</a>[K, V]</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/car.go#L70">src</a></small></sub></h3>

NewCAR returns a CAR that has space for the given number of items.


<h3><a id="Forget"></a><samp>func (c *<a href="#CAR">CAR</a>[K, V]) Forget(k K)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/car.go#L211">src</a></small></sub></h3>

Forget removes k from the cache if present.


<h3><a id="Get"></a><samp>func (c *<a href="#CAR">CAR</a>[K, V]) Get(k K) (V, bool)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/car.go#L82">src</a></small></sub></h3>

Get returns the value associated with k. The second return indicates whether k was in the cache,
and if false the first return is meaningless.


<h3><a id="Put"></a><samp>func (c *<a href="#CAR">CAR</a>[K, V]) Put(k K, v V)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/car.go#L104">src</a></small></sub></h3>

Put adds the given key/value pair to the cache.


<h3><a id="Cache"></a><samp>type Cache</samp></h3>
```go
type Cache[K comparable, V any] struct {
	// contains filtered or unexported fields
}
```

Cache is a Backing, used to store keys and values in memory, paired with read-through and
population coordination.

A common problem with caches is the thundering herd. A naive usage of a cache will check the
cache for a given key and on a miss will go fetch the authoritative data from wherever it's
stored. Unfortunately, many readers may ask for the same non-resident key simultaneously, and
this is likely for a key that suddenly becomes popular. Many goroutines will then all ask the
authoritative store for the value at once.

Cache coordinates populates to lessen this problem. If one goroutine asks for a key, and another
goroutine asks for the same key at the same time, the second will simply wait for the populate by
the first to finish and receive the same value.


<h3><a id="NewBatchFetchCache"></a><samp>func NewBatchFetchCache[K comparable, V any](fetchBatch func(ctx <a href="https://pkg.go.dev/context#Context">context.Context</a>, batch []K) ([]V, error), fetchParallelism int, batchInterval <a href="https://pkg.go.dev/time#Duration">time.Duration</a>, batchSize int, backing <a href="#Backing">Backing</a>[K, V]) *<a href="#Cache">Cache</a>[K, V]</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L101">src</a></small></sub></h3>

NewBatchFetchCache returns a Cache that uses backing as its backing storage. It has
fetchParallelism background goroutines used to call fetchBatch to fetch for each miss in Get and
GetBatch. On a miss, it will wait for up to batchInterval for other misses before calling
fetchBatch for up to batchSize of them at once.


<h3><a id="NewCache"></a><samp>func NewCache[K comparable, V any](fetch func(ctx <a href="https://pkg.go.dev/context#Context">context.Context</a>, key K) (V, error), fetchParallelism int, backing <a href="#Backing">Backing</a>[K, V]) *<a href="#Cache">Cache</a>[K, V]</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L59">src</a></small></sub></h3>

NewCache returns a Cache that uses backing as its backing store. It has fetchParallelism
background goroutines that will call fetch for each miss in Get and GetBatch.


<h3><a id="Close"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) Close()</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L265">src</a></small></sub></h3>

Close cleans up any background resources in use by the cache. It is invalid to call any methods
on c after calling Close.


<h3><a id="Forget"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) Forget(key K)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L251">src</a></small></sub></h3>

Forget removes key from the cache immediately.


<h3><a id="Get"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) Get(ctx <a href="https://pkg.go.dev/context#Context">context.Context</a>, key K) (V, error)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L180">src</a></small></sub></h3>

Get gets key from the cache. If key is not currently in the cache, causes the cache to fetch and
populate it.


<h3><a id="GetBatch"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) GetBatch(ctx <a href="https://pkg.go.dev/context#Context">context.Context</a>, keys []K) ([]V, error)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L203">src</a></small></sub></h3>

Get gets a set of keys from the cache at once. If any key in keys is not currently in the cache,
causes the cache to fetch and populate them.


<h3><a id="HitRate"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) HitRate() float64</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L257">src</a></small></sub></h3>

HitRate returns the hit rate of the cache: the number of times a 'Get' asked for a key that was
resident in the cache over the total number of requests for keys.


<h3><a id="Put"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) Put(key K, value V)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L246">src</a></small></sub></h3>

Put adds the given key and value to the cache, overwriting if already resident.


<h3><a id="TryGet"></a><samp>func (c *<a href="#Cache">Cache</a>[K, V]) TryGet(key K) (V, bool)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/cache.go#L172">src</a></small></sub></h3>

TryGet tries to get key from the cache. It returns false in the second return immediately if key
isn't currently resident in the cache, and does not try to populate.


<h3><a id="LRU"></a><samp>type LRU</samp></h3>
```go
type LRU[K comparable, V any] struct {
	// contains filtered or unexported fields
}
```

LRU is a least-recently-used eviction policy cache backing. It has a defined size in number of
items. If the LRU is full when putting an item, the key that was least recently Get or Put is
evicted to make space.

LRU's methods may be called concurrently.


<h3><a id="NewLRU"></a><samp>func NewLRU[K comparable, V any](size int) *<a href="#LRU">LRU</a>[K, V]</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/lru.go#L21">src</a></small></sub></h3>



<h3><a id="Forget"></a><samp>func (c *<a href="#LRU">LRU</a>[K, V]) Forget(key K)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/lru.go#L49">src</a></small></sub></h3>



<h3><a id="Get"></a><samp>func (c *<a href="#LRU">LRU</a>[K, V]) Get(key K) (V, bool)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/lru.go#L28">src</a></small></sub></h3>



<h3><a id="Put"></a><samp>func (c *<a href="#LRU">LRU</a>[K, V]) Put(key K, value V)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/lru.go#L40">src</a></small></sub></h3>



<h3><a id="Unbounded"></a><samp>type Unbounded</samp></h3>
```go
type Unbounded[K comparable, V any] struct {
	// contains filtered or unexported fields
}
```

Unbounded is an unbounded-size cache backing. It will never evict keys on its own, and thus will
grow to arbitrary size.

Unbounded's methods may be called concurrently.


<h3><a id="NewUnbounded"></a><samp>func NewUnbounded[K comparable, V any]() *<a href="#Unbounded">Unbounded</a>[K, V]</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/unbounded.go#L17">src</a></small></sub></h3>



<h3><a id="Forget"></a><samp>func (c *<a href="#Unbounded">Unbounded</a>[K, V]) Forget(key K)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/unbounded.go#L25">src</a></small></sub></h3>



<h3><a id="Get"></a><samp>func (c *<a href="#Unbounded">Unbounded</a>[K, V]) Get(key K) (V, bool)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/unbounded.go#L21">src</a></small></sub></h3>



<h3><a id="Put"></a><samp>func (c *<a href="#Unbounded">Unbounded</a>[K, V]) Put(key K, value V)</samp><sub class="float-right"><small><a href="https://github.com/bradenaw/bergamot/blob/main/unbounded.go#L23">src</a></small></sub></h3>



