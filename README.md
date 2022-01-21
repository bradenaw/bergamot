# Bergamot

Bergamot is a small Go library for caching. It has a few backings with different eviction policies
that can be used on their own or paired with the `Cache` type to coordinate populates on cache
misses.

It uses Go generics, and so requires Go 1.18 to build.
