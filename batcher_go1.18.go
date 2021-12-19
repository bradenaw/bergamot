//go:build go1.18

package gokart

import (
	"context"
	"sync"
	"time"

	"github.com/bradenaw/juniper/maps"
)

type waitable[T any] struct {
	value T
	err error
	wait chan struct{}
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

type batchRequest[T comparable, U any] struct {
	buffer map[T]*waitable[U]
	batch []T
}

type Batcher[T comparable, U any] struct {
	m sync.Mutex
	buffer map[T]*waitable[U]
	bufferSize int
	bufferFull chan struct{}
	interval time.Duration
	split func(buffer []T) [][]T
	fetch func(ctx context.Context, batch []T) ([]U, error)
	timer *time.Timer
}

func NewBatcher[T comparable, U any](
	bufferSize int,
	interval time.Duration,
	split func(buffer []T) [][]T,
	fetch func(ctx context.Context, batch []T) ([]U, error),
	fetchParallelism int,
) (_b *Batcher[T, U], _close func()) {
	ctx, cancel := context.WithCancel(context.Background())

	batchReqs := make(chan batchRequest[T, U])

	b := &Batcher[T, U]{
		buffer: make(map[T]*waitable[U], bufferSize),
		bufferSize: bufferSize,
		bufferFull: make(chan struct{}, 1),
		timer: time.NewTimer(365 * 24 * time.Hour),
		interval: interval,
		split: split,
		fetch: fetch,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case <-b.timer.C:
			case <-b.bufferFull:
				if !b.timer.Stop() {
					<-b.timer.C
				}
			}

			b.m.Lock()
			batches := split(maps.Keys(b.buffer))
			buffer := b.buffer
			b.buffer = make(map[T]*waitable[U], bufferSize)
			b.m.Unlock()
			for _, batch := range batches {
				batchReqs <- batchRequest[T, U]{
					batch: batch,
					buffer: buffer,
				}
			}
		}
	}()

	wg.Add(fetchParallelism)
	for i := 0; i < fetchParallelism; i++ {
		go func() {
			defer wg.Done()

			for {
				var batchReq batchRequest[T, U]
				select {
				case <-ctx.Done():
					return
				case batchReq = <-batchReqs:
				}

				resps, err := b.fetch(ctx, batchReq.batch)
				if err != nil {
					for _, req := range batchReq.batch {
						batchReq.buffer[req].Err(err)	
					}
				} else {
					for i := range batchReq.batch {
						batchReq.buffer[batchReq.batch[i]].Set(resps[i])	
					}
				}
			}
		}()
	}

	return b, func() {
		cancel()
		wg.Wait()
	}
}

func (b *Batcher[T, U]) Request(ctx context.Context, req T) (U, error) {
	b.m.Lock()
	w, ok := b.buffer[req]
	if !ok {
		w = &waitable[U]{wait: make(chan struct{})}
		b.buffer[req] = w
		if len(b.buffer) == 1 {
			if !b.timer.Stop() {
				<-b.timer.C
			}
			b.timer.Reset(b.interval)
		}
		if len(b.buffer) == b.bufferSize {
			select {
			case <-b.bufferFull:
			default:
			}
		}
	}
	b.m.Unlock()
	return w.WaitContext(ctx)
}
