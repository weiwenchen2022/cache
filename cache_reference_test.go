// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache_test

import (
	"sync"
	"sync/atomic"
)

// This file contains reference cache implementations for unit-tests.

// cacheInterface is the interface Cache implements.
type cacheInterface[K, E comparable] interface {
	Load(K) (E, bool)
	Store(key K, value E)

	LoadOrStore(key K, value E) (actual E, loaded bool)
	LoadAndDelete(key K) (value E, loaded bool)

	Delete(K)

	Swap(key K, value E) (previous E, loaded bool)

	CompareAndSwap(key K, old, new E) (swapped bool)
	CompareAndDelete(key K, old E) (deleted bool)

	Range(func(key K, value E) (shouldContinue bool))
}

var (
	_ cacheInterface[int, int] = &RWMutexCache[int, int]{}
	_ cacheInterface[int, int] = &DeepCopyCache[int, int]{}
	_ cacheInterface[int, int] = &MapCache[int, int]{}
)

// RWMutexCache is an implementation of cacheInterface using a sync.RWMutex.
type RWMutexCache[K, E comparable] struct {
	mu    sync.RWMutex
	dirty map[K]E
}

func (c *RWMutexCache[K, E]) Load(key K) (value E, ok bool) {
	c.mu.RLock()
	value, ok = c.dirty[key]
	c.mu.RUnlock()
	return value, ok
}

func (c *RWMutexCache[K, E]) Store(key K, value E) {
	c.mu.Lock()
	if c.dirty == nil {
		c.dirty = make(map[K]E)
	}
	c.dirty[key] = value
	c.mu.Unlock()
}

func (c *RWMutexCache[K, E]) LoadOrStore(key K, value E) (actual E, loaded bool) {
	c.mu.Lock()
	actual, loaded = c.dirty[key]
	if loaded {
		c.mu.Unlock()
		return actual, true
	}

	actual = value
	if c.dirty == nil {
		c.dirty = make(map[K]E)
	}
	c.dirty[key] = value
	c.mu.Unlock()

	return actual, false
}

func (c *RWMutexCache[K, E]) Swap(key K, value E) (previous E, loaded bool) {
	c.mu.Lock()
	if c.dirty == nil {
		c.dirty = make(map[K]E)
	}
	previous, loaded = c.dirty[key]
	c.dirty[key] = value
	c.mu.Unlock()

	return previous, loaded
}

func (c *RWMutexCache[K, E]) LoadAndDelete(key K) (value E, loaded bool) {
	c.mu.Lock()
	value, loaded = c.dirty[key]
	if !loaded {
		c.mu.Unlock()
		return value, false
	}

	delete(c.dirty, key)
	c.mu.Unlock()
	return value, true
}

func (c *RWMutexCache[K, E]) Delete(key K) {
	_, _ = c.LoadAndDelete(key)
}

func (c *RWMutexCache[K, E]) CompareAndSwap(key K, old, new E) (swapped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dirty == nil {
		return false
	}

	value, loaded := c.dirty[key]
	if loaded && value == old {
		c.dirty[key] = new
		return true
	}

	return false
}

func (c *RWMutexCache[K, E]) CompareAndDelete(key K, old E) (deleted bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dirty == nil {
		return false
	}

	value, loaded := c.dirty[key]
	if loaded && value == old {
		delete(c.dirty, key)
		return true
	}

	return false
}

func (c *RWMutexCache[K, E]) Range(f func(key K, value E) (shouldContinue bool)) {
	c.mu.Lock()
	keys := make([]K, len(c.dirty))
	i := 0
	for k := range c.dirty {
		keys[i] = k
		i++
	}
	c.mu.Unlock()

	for _, k := range keys {
		v, ok := c.Load(k)
		if !ok {
			continue
		}

		if !f(k, v) {
			break
		}
	}
}

// DeepCopyCache is an implementation of cacheInterface using a Mutex and
// atomic.Value. It makes deep copies of the cache on every write to avoid
// acquiring the Mutex in Load.
type DeepCopyCache[K, E comparable] struct {
	mu    sync.Mutex
	clean atomic.Value
}

func (c *DeepCopyCache[K, E]) Load(key K) (value E, ok bool) {
	clean := c.loadClean()
	value, ok = clean[key]
	return value, ok
}

func (c *DeepCopyCache[K, E]) Store(key K, value E) {
	c.mu.Lock()
	dirty := c.dirty()
	dirty[key] = value
	c.clean.Store(dirty)
	c.mu.Unlock()
}

func (c *DeepCopyCache[K, E]) LoadOrStore(key K, value E) (actual E, loaded bool) {
	clean := c.loadClean()
	actual, loaded = clean[key]
	if loaded {
		return actual, true
	}

	c.mu.Lock()
	// Reload clean in case it changed while we were waiting on m.mu.
	clean = c.loadClean()
	actual, loaded = clean[key]
	if loaded {
		c.mu.Unlock()
		return actual, true
	}

	actual = value
	dirty := c.dirty()
	dirty[key] = value
	c.clean.Store(dirty)
	c.mu.Unlock()

	return actual, false
}

func (c *DeepCopyCache[K, E]) Swap(key K, value E) (previous E, loaded bool) {
	c.mu.Lock()
	dirty := c.dirty()
	previous, loaded = dirty[key]
	dirty[key] = value
	c.clean.Store(dirty)
	c.mu.Unlock()

	return previous, loaded
}

func (c *DeepCopyCache[K, E]) LoadAndDelete(key K) (value E, loaded bool) {
	c.mu.Lock()
	clean := c.loadClean()
	value, loaded = clean[key]
	if !loaded {
		c.mu.Unlock()
		return value, false
	}

	dirty := c.dirty()
	delete(dirty, key)
	c.clean.Store(dirty)
	c.mu.Unlock()

	return value, true
}

func (c *DeepCopyCache[K, E]) Delete(key K) {
	_, _ = c.LoadAndDelete(key)
}

func (c *DeepCopyCache[K, E]) CompareAndSwap(key K, old, new E) (swapped bool) {
	clean := c.loadClean()
	if previous, ok := clean[key]; !ok || previous != old {
		return false
	}

	c.mu.Lock()
	clean = c.loadClean()
	if previous, ok := clean[key]; !ok || previous != old {
		c.mu.Unlock()
		return false
	}

	dirty := c.dirty()
	dirty[key] = new
	c.clean.Store(dirty)
	c.mu.Unlock()
	return true
}

func (c *DeepCopyCache[K, E]) CompareAndDelete(key K, old E) (deleted bool) {
	clean := c.loadClean()
	if previous, ok := clean[key]; !ok || previous != old {
		return false
	}

	c.mu.Lock()
	clean = c.loadClean()
	if previous, ok := clean[key]; !ok || previous != old {
		c.mu.Unlock()
		return false
	}

	dirty := c.dirty()
	delete(dirty, key)
	c.clean.Store(dirty)
	c.mu.Unlock()
	return true
}

func (c *DeepCopyCache[K, E]) Range(f func(key K, value E) (shouldContinue bool)) {
	clean := c.loadClean()
	for k, v := range clean {
		if !f(k, v) {
			break
		}
	}
}

func (c *DeepCopyCache[K, E]) loadClean() map[K]E {
	if clean := c.clean.Load(); clean != nil {
		return clean.(map[K]E)
	}

	return nil
}

func (c *DeepCopyCache[K, E]) dirty() map[K]E {
	clean := c.loadClean()
	dirty := make(map[K]E, len(clean)+1)
	for k, v := range clean {
		dirty[k] = v
	}

	return dirty
}

// MapCache is an implementation of cacheInterface using a sync.Map.
type MapCache[K, E comparable] struct {
	m sync.Map
}

func (c *MapCache[K, E]) Load(key K) (value E, ok bool) {
	v, ok := c.m.Load(key)
	value, _ = v.(E)
	return value, ok
}

func (c *MapCache[K, E]) Store(key K, value E) {
	c.m.Store(key, value)
}

func (c *MapCache[K, E]) LoadOrStore(key K, value E) (actual E, loaded bool) {
	v, loaded := c.m.LoadOrStore(key, value)
	actual, _ = v.(E)
	return actual, loaded
}

func (c *MapCache[K, E]) Swap(key K, value E) (previous E, loaded bool) {
	v, loaded := c.m.Swap(key, value)
	previous, _ = v.(E)
	return previous, loaded
}

func (c *MapCache[K, E]) LoadAndDelete(key K) (value E, loaded bool) {
	v, loaded := c.m.LoadAndDelete(key)
	value, _ = v.(E)
	return value, loaded
}

func (c *MapCache[K, E]) Delete(key K) {
	_, _ = c.LoadAndDelete(key)
}

func (c *MapCache[K, E]) CompareAndSwap(key K, old, new E) (swapped bool) {
	return c.m.CompareAndSwap(key, old, new)
}

func (c *MapCache[K, E]) CompareAndDelete(key K, old E) (deleted bool) {
	return c.m.CompareAndDelete(key, old)
}

func (c *MapCache[K, E]) Range(f func(key K, value E) (shouldContinue bool)) {
	c.m.Range(func(key, value any) bool {
		k, v := key.(K), value.(E)
		return f(k, v)
	})
}
