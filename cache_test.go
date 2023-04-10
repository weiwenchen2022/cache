// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache_test

import (
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"

	"github.com/weiwenchen2022/cache"
)

type cacheOp string

const (
	opLoad  = cacheOp("Load")
	opStore = cacheOp("Store")

	opLoadOrStore   = cacheOp("LoadOrStore")
	opLoadAndDelete = cacheOp("LoadAndDelete")

	opDelete = cacheOp("Delete")
	opSwap   = cacheOp("Swap")

	opCompareAndSwap   = cacheOp("CompareAndSwap")
	opCompareAndDelete = cacheOp("CompareAndDelete")
)

var cacheOps = [...]cacheOp{
	opLoad,
	opStore,

	opLoadOrStore,
	opLoadAndDelete,

	opDelete,
	opSwap,

	opCompareAndSwap,
	opCompareAndDelete,
}

// cacheCall is a quick.Generator for calls on cacheInterface.
type cacheCall struct {
	op   cacheOp
	k, v string
}

func (cc cacheCall) apply(c cacheInterface[string, string]) (value string, ok bool) {
	switch cc.op {
	case opLoad:
		return c.Load(cc.k)
	case opStore:
		c.Store(cc.k, cc.v)
		return "", false
	case opLoadOrStore:
		return c.LoadOrStore(cc.k, cc.v)
	case opLoadAndDelete:
		return c.LoadAndDelete(cc.k)
	case opDelete:
		c.Delete(cc.k)
		return "", false
	case opSwap:
		return c.Swap(cc.k, cc.v)
	case opCompareAndSwap:
		if c.CompareAndSwap(cc.k, cc.v, string('a'+byte(rand.Intn(26)))) {
			c.Delete(cc.k)
			return cc.v, true
		}

		return "", false
	case opCompareAndDelete:
		if c.CompareAndDelete(cc.k, cc.v) {
			if _, ok := c.Load(cc.k); !ok {
				return "", true
			}
		}

		return "", false
	default:
		panic("invalid cacheOp " + cc.op)
	}
}

type cacheResult struct {
	value string
	ok    bool
}

func randValue(r *rand.Rand) string {
	b := make([]byte, r.Intn(4))

	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}

	return string(b)
}

func (cacheCall) Generate(r *rand.Rand, size int) reflect.Value {
	cc := cacheCall{op: cacheOps[rand.Intn(len(cacheOps))], k: randValue(r)}

	switch cc.op {
	case opStore, opLoadOrStore:
		cc.v = randValue(r)
	}

	return reflect.ValueOf(cc)
}

func applyCalls(c cacheInterface[string, string], calls []cacheCall) (results []cacheResult, final map[string]string) {
	results = make([]cacheResult, len(calls))
	for i, cc := range calls {
		v, ok := cc.apply(c)
		results[i] = cacheResult{v, ok}
	}

	final = make(map[string]string)
	c.Range(func(k, v string) bool {
		final[k] = v
		return true
	})

	return results, final
}

func applyCache(calls []cacheCall) ([]cacheResult, map[string]string) {
	return applyCalls(&cache.ComparableCache[string, string]{}, calls)
}

func applyRWMutexCache(calls []cacheCall) ([]cacheResult, map[string]string) {
	return applyCalls(&RWMutexCache[string, string]{}, calls)
}

func applyDeepCopyCache(calls []cacheCall) ([]cacheResult, map[string]string) {
	return applyCalls(&DeepCopyCache[string, string]{}, calls)
}

func applyMapCache(calls []cacheCall) ([]cacheResult, map[string]string) {
	return applyCalls(&MapCache[string, string]{}, calls)
}

func TestCacheMatchesRWMute(t *testing.T) {
	t.Parallel()

	if err := quick.CheckEqual(applyRWMutexCache, applyCache, nil); err != nil {
		t.Error(err)
	}
}

func TestCacheMatchesDeepCopy(t *testing.T) {
	t.Parallel()

	if err := quick.CheckEqual(applyDeepCopyCache, applyCache, nil); err != nil {
		t.Error(err)
	}
}

func TestCacheMatchesMap(t *testing.T) {
	t.Parallel()

	if err := quick.CheckEqual(applyMapCache, applyCache, nil); err != nil {
		t.Error(err)
	}
}

func TestConcurrentRange(t *testing.T) {
	t.Parallel()

	const cacheSize = 1 << 10

	var c cache.Cache[int64, int64]
	for n := int64(1); n <= cacheSize; n++ {
		c.Store(n, n)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(done)
		wg.Wait()
	}()

	for g := int64(runtime.GOMAXPROCS(0)); g > 0; g-- {
		r := rand.New(rand.NewSource(g))
		wg.Add(1)
		go func(g int64) {
			defer wg.Done()

			for i := int64(0); ; i++ {
				select {
				case <-done:
					return
				default:
				}

				for n := int64(1); n < cacheSize; n++ {
					if r.Int63n(cacheSize) == 0 {
						c.Store(n, n*i*g)
					} else {
						c.Load(n)
					}
				}
			}
		}(g)
	}

	iters := 1 << 10
	if testing.Short() {
		iters = 16
	}

	for n := iters; n > 0; n-- {
		seen := make(map[int64]bool, cacheSize)

		c.Range(func(k, v int64) bool {
			if v%k != 0 {
				t.Fatalf("while Storing multiples of %d, Range saw value %d", k, v)
			}

			if seen[k] {
				t.Fatalf("Range visited key %d twice", k)
			}

			seen[k] = true
			return true
		})

		if len(seen) != cacheSize {
			t.Fatalf("Range visited %d elements of %d-element Cache", len(seen), cacheSize)
		}
	}
}

func TestIssue40999(t *testing.T) {
	t.Parallel()

	var c cache.Cache[*int, struct{}]

	// Since the miss-counting in missLocked (via Delete)
	// compares the miss count with len(m.dirty),
	// add an initial entry to bias len(m.dirty) above the miss count.
	c.Store((*int)(nil), struct{}{})

	var finalized atomic.Uint32

	// Set finalizers that count for collected keys. A non-zero count
	// indicates that keys have not been leaked.
	for finalized.Load() == 0 {
		p := new(int)
		runtime.SetFinalizer(p, func(*int) {
			finalized.Add(1)
		})

		c.Store(p, struct{}{})
		c.Delete(p)
		runtime.GC()
	}
}

func TestCacheRangeNestedCall(t *testing.T) {
	t.Parallel()

	var c cache.Cache[int, string]
	for i, v := range [...]string{"hello", "world", "Go"} {
		c.Store(i, v)
	}

	c.Range(func(key int, value string) bool {
		c.Range(func(key int, value string) bool {
			// We should be able to load the key offered in the Range callback,
			// because there are no concurrent Delete involved in this tested cache.
			if v, ok := c.Load(key); !ok || value != v {
				t.Fatalf("Nested Range loads unexpected value, want %q got %q", value, v)
			}

			// We didn't keep 42 and a value into the cache before, if somehow we loaded
			// a value from such a key, meaning there must be an internal bug regarding
			// nested range in the Map.
			if _, loaded := c.LoadOrStore(42, "dummy"); loaded {
				t.Fatalf("Nested Range loads unexpected value, want store a new value")
			}

			// Try to Store then LoadAndDelete the corresponding value with the key
			// 42 to the Cache. In this case, the key 42 and associated value should be
			// removed from the Map. Therefore any future range won't observe key 42
			// as we checked in above.
			val := "cache.Cache"
			c.Store(42, val)
			if v, loaded := c.LoadAndDelete(42); !loaded || val != v {
				t.Fatalf("Nested Range loads unexpected value, want %q, got %q", val, v)
			}

			return true
		})

		// Remove key from Cache on-the-fly.
		c.Delete(key)
		return true
	})

	// After a Range of Delete, all keys should be removed and any
	// further Range won't invoke the callback. Hence length remains 0.
	length := 0
	c.Range(func(key int, value string) bool {
		length++
		return true
	})

	if length != 0 {
		t.Fatalf("Unexpected cache.Cache size, want %d got %d", 0, length)
	}
}

func TestCompareAndSwap_NonExistingKey(t *testing.T) {
	t.Parallel()

	var c cache.ComparableCache[any, any]

	if c.CompareAndSwap(&c, nil, 42) {
		// See https://go.dev/issue/51972#issuecomment-1126408637.
		t.Fatalf("CompareAndSwap on an non-existing key succeeded")
	}
}
