# Bergamot

[![Go Reference](https://pkg.go.dev/badge/github.com/bradenaw/bergamot.svg)](https://pkg.go.dev/github.com/bradenaw/bergamot)

Bergamot is a small Go library for caching. It has a few backings with different eviction policies
that can be used on their own or paired with the `Cache` type to coordinate populates on cache
misses.

It uses Go generics, and so requires Go 1.18 to build.

Here's a straightforward usage:

```
c := bergamot.NewCache[uint64, User](
    // fetch: used to populate the cache on a miss
    func(ctx context.Context, userID uint64) (User, error) {
        return callSomeExpensiveRPC(ctx, userID)
    },
    8, // how many fetches can be active at once
    // use CAR (Clock with Adaptive Replacement) as the eviction policy
    bergamot.NewCAR(
        10000, // the cache capacity, in number of items
    ),
)

// ...

u, err := c.Get(ctx, 12345)
```

See the [docs](https://pkg.go.dev/github.com/bradenaw/bergamot) for more.
