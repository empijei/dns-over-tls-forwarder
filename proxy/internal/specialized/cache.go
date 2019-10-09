// This example demonstrates a priority queue built using the heap interface.
package specialized

import (
	"fmt"
	"sync"
)

type Value interface{}

// Cache is a Least-Recently-Used Most-Frequently-Accessed concurrent safe cache.
// All its methods are safe to call concurrently.
type Cache struct {
	mu sync.Mutex
	// lru keeps track of most recently accessed items
	lru *store
	// mfa keeps track of most frequently accessed items
	mfa *store
	// t is a number representing the current time.
	// It will be used as a clock by the builtin now().
	t uint
	// timeNow can be set to override the builtin timing mechanism
	timeNow func() uint
	// capacity is the maximum storage the cache can hold
	capacity int
}

// compute max size at compile time since it depends on the target architecture
const maxsize = (^uint(0) / 2)

// NewCache constructs a new Cache ready for use.
// The specified size should never be bigger or roughly as big as the maximum available value for uint.
func NewCache(size int) (*Cache, error) {
	if size <= 0 {
		return nil, nil
	}
	if size < 2 {
		return nil, fmt.Errorf("cache size < 2 not supported, %d provided", size)
	}
	if uint(size) > maxsize {
		return nil, fmt.Errorf("cache size(%d) above supported limit(%d)", size, maxsize)
	}
	c := Cache{
		lru:      newStore(size/2, byTime),
		mfa:      newStore(size/2+size%2, byAccesses),
		capacity: size,
	}
	return &c, nil
}

// SetTimer will set the cache internal timer to the given one.
// The given timer should behave as a monotonic clock and should update its value at least once a second.
// Calling this after the cache has already been used leads to undefined behavior.
func (c *Cache) SetTimer(timer func() uint) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.timeNow = timer
}

// Get retrieves an item from the cache.
// Its amortized worst-case complexity is ~O(log(c.Len())).
func (c *Cache) Get(k string) (v Value, ok bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	printf("\n %2d get %q", now, k)

	if v, ok := c.mfa.get(now, k); ok {
		// Hit on MFA
		printf("MFA hit")
		return v, true
	}
	if v, ok := c.lru.get(now, k); ok {
		// Hit on LRU
		printf("LRU hit")
		return v, true
	}
	printf("miss")
	return nil, false
}

// Put stores an item in the cache.
// Its amortized worst-case complexity is ~O(log(c.Len())).
func (c *Cache) Put(k string, v Value) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	printf("\n %2d put %q", now, k)

	if c.mfa.update(now, k, v) {
		// Item was in MFA and was updated
		printf("update MFA")
		return
	}
	if c.lru.update(now, k, v) {
		// Item was in LRU and was updated
		printf("update LRU")
		return
	}
	// Item was not in cache, put in LRU first
	lruovf := c.lru.put(now, k, v, 1)
	if lruovf.v == nil {
		// LRU had room to accommodate the new entry
		printf("LRU put(%q, %+v)", k, v)
		return
	}
	printf("put LRU, evict (%q,%+v)", lruovf.key, lruovf.v)
	// LRU popped out an item because of our push.
	// Let's promote to MFA if there is room.
	if c.mfa.Len() < c.mfa.cap() {
		printf("MFA put(%q, %+v)", lruovf.key, lruovf.v)
		c.mfa.put(now, lruovf.key, lruovf.v, lruovf.a)
		return
	}
	// No room in MFA.
	// Check if the evicted item was accessed enough times to be promoted to MFA.
	if c.mfa.peek().a < lruovf.a ||
		c.mfa.peek().a == lruovf.a && c.mfa.peek().t < lruovf.t {
		printf("discard %q (p%d), keep %q (p%d)", lruovf.key, lruovf.a, c.mfa.peek().key, c.mfa.peek().a)
		return
	}
	printf("MFA put(%q, %+v)", lruovf.key, lruovf.v)
	mfaovf := c.mfa.put(now, lruovf.key, lruovf.v, lruovf.a)
	printf("put MFA, evict %+v", mfaovf)
	// Pushing to MFA popped out an item. If the item was in MFA it means
	// it is probably worth keeping around for a while longer.
	// Reset access count and push it to LRU if it was accessed more than the
	// last item in LRU, discard otherwise.
	if c.lru.Len() > 0 && c.lru.peek().a < mfaovf.a {
		c.lru.put(now, mfaovf.key, mfaovf.v, 1)
		printf("LRU put(%q, %+v)", mfaovf.key, mfaovf.v)
	}
}

// Len returns the amount of items currently stored in the cache.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len() + c.mfa.Len()
}

// Cap returns the maximum amount of items the cache can hold.
func (c *Cache) Cap() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.cap() + c.mfa.cap()
}

func (c *Cache) now() uint {
	if c.timeNow != nil {
		return c.timeNow()
	}
	c.t++
	if c.t == 0 {
		// Overflow: walk over the stores and reset times in a way
		// that preserves invariants after a time wraparound.
		c.t = c.lru.reset(c.t)
		c.t = c.mfa.reset(c.t)
	}
	return c.t
}
