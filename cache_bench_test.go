// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache_test

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiwenchen2022/cache"
)

type bench[K, E comparable] struct {
	setup func(*testing.B, cacheInterface[K, E])
	perG  func(b *testing.B, pb *testing.PB, i int, c cacheInterface[K, E])
}

func benchCache[K, E comparable](b *testing.B, bench bench[K, E]) {
	for _, c := range [...]cacheInterface[K, E]{
		// &DeepCopyCache[K, E]{},
		// &RWMutexCache[K, E]{},
		&MapCache[K, E]{},
		&cache.ComparableCache[K, E]{},
	} {
		name := fmt.Sprintf("%T", c)
		if index := strings.Index(name, "["); index >= 0 {
			name = name[:index]
		}

		b.Run(name, func(b *testing.B) {
			c = reflect.New(reflect.TypeOf(c).Elem()).Interface().(cacheInterface[K, E])

			if bench.setup != nil {
				bench.setup(b, c)
			}

			b.ResetTimer()
			var i atomic.Int64
			b.RunParallel(func(pb *testing.PB) {
				id := int(i.Add(1) - 1)
				bench.perG(b, pb, id*b.N, c)
			})
		})
	}
}

func BenchmarkLoadMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.Load(i % (hits + misses))
			}
		},
	})
}

func BenchmarkLoadMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.Load(i % (hits + misses))
			}
		},
	})
}

func BenchmarkLoadOrStoreBalanced(b *testing.B) {
	const hits, misses = 128, 128

	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			if _, ok := c.(*DeepCopyCache[int, int]); ok {
				b.Skip("DeepCopyCache has quadratic running time.")
			}

			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				j := i % (hits + misses)
				if j < hits {
					if _, ok := c.LoadOrStore(j, i); !ok {
						b.Fatalf("unexpected miss for %d", j)
					}
				} else {
					if v, loaded := c.LoadOrStore(i, i); loaded {
						b.Fatalf("failed to store %d: existing value %d", i, v)
					}
				}
			}
		},
	})
}

func BenchmarkLoadOrStoreUnique(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			if _, ok := c.(*DeepCopyCache[int, int]); ok {
				b.Skip("DeepCopyCache has quadratic running time.")
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.LoadOrStore(i, i)
			}
		},
	})
}

func BenchmarkLoadOrStoreCollision(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.LoadOrStore(0, 0)
			}
		},
	})
}

func BenchmarkLoadAndDeleteBalance(b *testing.B) {
	const hits, misses = 128, 128

	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			if _, ok := c.(*DeepCopyCache[int, int]); ok {
				b.Skip("DeepCopyCache has quadratic running time.")
			}

			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				j := i % (hits + misses)
				if j < hits {
					c.LoadAndDelete(j)
				} else {
					c.LoadAndDelete(i)
				}
			}
		},
	})
}

func BenchmarkLoadAndDeleteUnique(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			if _, ok := c.(*DeepCopyCache[int, int]); ok {
				b.Skip("DeepCopyCache has quadratic running time.")
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.LoadAndDelete(i)
			}
		},
	})
}

func BenchmarkLoadAndDeleteCollision(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				if _, loaded := c.LoadAndDelete(0); loaded {
					c.Store(0, 0)
				}
			}
		},
	})
}

func BenchmarkRange(b *testing.B) {
	const cacheSize = 1 << 10

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < cacheSize; i++ {
				c.Store(i, i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.Range(func(_, _ int) bool { return true })
			}
		},
	})
}

// BenchmarkAdversarialAlloc tests performance when we store a new value
// immediately whenever the map is promoted to clean and otherwise load a
// unique, missing key.
//
// This forces the Load calls to always acquire the cache's mutex.
func BenchmarkAdversarialAlloc(b *testing.B) {
	benchCache(b, bench[int, int64]{
		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int64]) {
			var stores, loadsSinceStore int64

			for ; pb.Next(); i++ {
				c.Load(i)
				if loadsSinceStore++; loadsSinceStore > stores {
					c.LoadOrStore(i, stores)
					loadsSinceStore = 0
					stores++
				}
			}
		},
	})
}

// BenchmarkAdversarialDelete tests performance when we periodically delete
// one key and add a different one in a large map.
//
// This forces the Load calls to always acquire the map's mutex and periodically
// makes a full copy of the map despite changing only one entry.
func BenchmarkAdversarialDelete(b *testing.B) {
	const cacheSize = 1 << 10

	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < cacheSize; i++ {
				c.Store(i, i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.Load(i)

				if i%cacheSize == 0 {
					c.Range(func(key, _ int) bool {
						c.Delete(key)
						return false
					})

					c.Store(i, i)
				}
			}
		},
	})
}

func BenchmarkDeleteCollision(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.Delete(0)
			}
		},
	})
}

func BenchmarkSwapCollision(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.Swap(0, 0)
			}
		},
	})
}

func BenchmarkSwapMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				if i%(hits+misses) < hits {
					v := i % (hits + misses)
					c.Swap(v, v)
				} else {
					c.Swap(i, i)
					c.Delete(i)
				}
			}
		},
	})
}

func BenchmarkSwapMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				if i%(hits+misses) < hits {
					v := i % (hits + misses)
					c.Swap(v, v)
				} else {
					c.Swap(i, i)
					c.Delete(i)
				}
			}
		},
	})
}

func BenchmarkCompareAndSwapCollision(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for pb.Next() {
				if c.CompareAndSwap(0, 0, 42) {
					c.CompareAndSwap(0, 42, 0)
				}
			}
		},
	})
}

func BenchmarkCompareAndSwapNoExistingKey(b *testing.B) {
	benchCache(b, bench[int, int]{
		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				if c.CompareAndSwap(i, 0, 0) {
					c.Delete(i)
				}
			}
		},
	})
}

func BenchmarkCompareAndSwapValueNotEqual(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.Store(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				c.CompareAndSwap(0, 1, 2)
			}
		},
	})
}

func BenchmarkCompareAndSwapMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			if _, ok := c.(*DeepCopyCache[int, int]); ok {
				b.Skip("DeepCopyCache has quadratic running time.")
			}

			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				c.CompareAndSwap(v, v, v)
			}
		},
	})
}

func BenchmarkCompareAndSwapMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				c.CompareAndSwap(v, v, v)
			}
		},
	})
}

func BenchmarkCompareAndDeleteCollision(b *testing.B) {
	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			c.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				if c.CompareAndDelete(0, 0) {
					c.Store(0, 0)
				}
			}
		},
	})
}

func BenchmarkCompareAndDeleteMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchCache(b, bench[int, int]{
		setup: func(b *testing.B, c cacheInterface[int, int]) {
			if _, ok := c.(*DeepCopyCache[int, int]); ok {
				b.Skip("DeepCopyCache has quadratic running time.")
			}

			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				if c.CompareAndDelete(v, v) {
					c.Store(v, v)
				}
			}
		},
	})
}

func BenchmarkCompareAndDeleteMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchCache(b, bench[int, int]{
		setup: func(_ *testing.B, c cacheInterface[int, int]) {
			for i := 0; i < hits; i++ {
				c.LoadOrStore(i, i)
			}

			// Prime the map to get it into a steady state.
			for i := 0; i < hits*2; i++ {
				c.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, c cacheInterface[int, int]) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				if c.CompareAndDelete(v, v) {
					c.Store(v, v)
				}
			}
		},
	})
}
