package proxy

import (
	"container/heap"
	"time"

	"github.com/miekg/dns"
)

// store keeps a cache of dns responses.
// It evicts records in order of expiration.
// Only new, get and put should be used by consumers of this type.
type store struct {
	// pq is a priority queue implemented as a min heap
	pq []cacheItem
	// m is a lokup map for the priority queue underlying data
	m map[string]int
	// t is the current time expressed as unix seconds. This doesn't need to be kept up to date
	// as long as heap operations observe this value atomically (e.g. it should not change while a
	// call to heap.Push is running).
	t int64
	// now is a function that returns the current unix seconds time.
	now func() int64
}

func newStore(size int) *store {
	c := &store{
		pq:  make([]cacheItem, 0, size),
		m:   make(map[string]int, size),
		now: func() int64 { return time.Now().Unix() },
	}
	c.t = c.now()
	heap.Init(c)
	return c
}

func (c *store) get(key string) *dns.Msg {
	v, ok := c.m[key]
	if !ok {
		return nil
	}
	return c.pq[v].value
}

func (c *store) put(key string, v *dns.Msg, ttl int) {
	c.t = c.now()
	if i, ok := c.m[key]; ok {
		// Just update the value that is already in store.
		c.pq[i].value = v
		c.pq[i].ttl = int64(ttl)
		heap.Fix(c, i)
		return
	}
	if len(c.pq) == cap(c.pq) {
		// We are full, discard item with lowest score before inserting.
		_ = heap.Pop(c)
	}
	heap.Push(c, cacheItem{key: key, value: v, ttl: int64(ttl)})
}

// Impl of containers/heap.Interface

func (c *store) Len() int           { return len(c.pq) }
func (c *store) Less(i, j int) bool { return c.pq[i].secondsLeft(c.t) < c.pq[j].secondsLeft(c.t) }

func (c *store) Swap(i, j int) {
	c.pq[i], c.pq[j] = c.pq[j], c.pq[i]
	c.pq[i].index, c.pq[j].index = i, j
	c.m[c.pq[i].key], c.m[c.pq[j].key] = i, j
}

func (c *store) Push(x interface{}) {
	item := x.(cacheItem)
	n := len(c.pq)
	item.index = n
	c.m[item.key] = n
	c.pq = append(c.pq, item)
}

func (c *store) Pop() interface{} {
	n := len(c.pq)
	item := c.pq[n-1]
	item.index = -1
	delete(c.m, item.key)
	c.pq = c.pq[:n-1]
	return item
}

type cacheItem struct {
	// The key for the store lookup
	key   string
	value *dns.Msg
	// The ttl of the cached response
	ttl int64
	// t is the insertion time of the item.
	t int64

	// Index of the item in the heap.
	index int
}

func (c *cacheItem) secondsLeft(unixNow int64) int64 { return c.ttl - (unixNow - c.t) }
