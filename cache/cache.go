package cache

import (
	"time"

	"github.com/alexanderGugel/arc"
	"github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
)

const maxTTL = time.Duration(24) * time.Hour

type Cache struct {
	c *arc.ARC
}

type CacheValue struct {
	m   dns.Msg
	exp time.Time
}

func New(size int) *Cache {
	return &Cache{c: arc.New(size)}
}

func (c *Cache) Get(mk *dns.Msg) (*dns.Msg, bool) {
	k := key(mk)
	r, ok := c.c.Get(k)
	if !ok || r == nil {
		log.Debugf("[CACHE] MISS %v", k)
		return nil, false
	}
	v := r.(CacheValue)
	mv := v.m.Copy()
	// Rewrite the answer ID to match the question ID.
	mv.Id = mk.Id
	// If the TTL has expired, speculatively return the cache entry anyway with a short TTL, and refresh it.
	if v.exp.Before(time.Now().UTC()) {
		log.Debugf("[CACHE] MISS + REFRESH due to expired TTL for %v", k)
		// Set a very short TTL
		for _, a := range mv.Answer {
			a.Header().Ttl = 60
		}
		return mv, false
	}
	log.Debugf("[CACHE] HIT %v", k)
	// Rewrite TTL
	for _, a := range mv.Answer {
		a.Header().Ttl = uint32(time.Since(v.exp).Seconds() * -1)
	}
	return mv, true
}

func (c *Cache) Put(k *dns.Msg, v *dns.Msg) {
	now := time.Now().UTC()
	minExpirationTime := now.Add(maxTTL)
	// Do not cache negative results.
	if len(v.Answer) == 0 {
		log.Debugf("[CACHE] Did not cache empty answer %v", key(k))
		return
	}
	for _, a := range v.Answer {
		exp := now.Add(time.Duration(a.Header().Ttl) * time.Second)
		if exp.Before(minExpirationTime) {
			minExpirationTime = exp
		}
	}
	cm := v.Copy()
	// Always set the TC bit to off.
	cm.Truncated = false
	// Always compress on the wire.
	cm.Compress = true

	c.c.Put(key(k), CacheValue{m: *cm, exp: minExpirationTime})
}

func key(k *dns.Msg) string {
	return k.Question[0].String()
}
