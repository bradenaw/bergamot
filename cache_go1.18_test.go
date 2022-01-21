package bergamot

import (
	"context"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStressCache(t *testing.T) {
	check := func(storage Backing[int, string]) {
		c := NewCache[int, string](
			func(ctx context.Context, key int) (string, error) {
				time.Sleep(4 * time.Millisecond)
				return strconv.Itoa(key), nil
			},
			4,
			storage,
		)
		ctx := context.Background()
		attempts := uint64(0)

		start := time.Now()
		d := 10 * time.Second
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				zipf := rand.NewZipf(
					rand.New(rand.NewSource(0)),
					1.5, 1, 200,
				)

				for time.Since(start) < d {
					key := int(zipf.Uint64())
					value, err := c.Get(ctx, key)
					require.NoError(t, err)
					require.Equal(t, value, strconv.Itoa(key))
					atomic.AddUint64(&attempts, 1)
				}
			}()
		}

		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				r := rand.New(rand.NewSource(0))
				zipf := rand.NewZipf(r, 1.5, 1, 200)

				for time.Since(start) < d {
					keys := make([]int, r.Intn(10)+1)
					for i := range keys {
						keys[i] = int(zipf.Uint64())
					}
					values, err := c.GetBatch(ctx, keys)
					require.NoError(t, err)
					for i := range values {
						require.Equal(t, values[i], strconv.Itoa(keys[i]))
					}
					atomic.AddUint64(&attempts, uint64(len(keys)))
				}
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Since(start) < d {
				time.Sleep(2 * time.Second)
				t.Logf(
					"%s %d attempts, hit rate = %.2f%%",
					time.Since(start).Round(time.Second),
					atomic.LoadUint64(&attempts),
					c.HitRate()*100,
				)
			}
		}()

		wg.Wait()
	}

	t.Run("CAR", func(t *testing.T) {
		check(NewCAR[int, string](100))
	})
	t.Run("LRU", func(t *testing.T) {
		check(NewLRU[int, string](100))
	})
	t.Run("Unbounded", func(t *testing.T) {
		check(NewUnbounded[int, string]())
	})
}
