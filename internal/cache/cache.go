package cache

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type entry struct {
	timestamp time.Time
	ips       []string
}

type Cache struct {
	mu         sync.RWMutex
	entries    map[string]entry
	DefaultTTL time.Duration
}

func New(defaultTTL time.Duration) *Cache {
	return &Cache{
		entries:    make(map[string]entry),
		DefaultTTL: defaultTTL,
	}
}

// Key returns a canonical cache key for a set of hostnames (order-independent).
func Key(hosts []string) string {
	sorted := make([]string, len(hosts))
	copy(sorted, hosts)
	sort.Strings(sorted)
	return strings.Join(sorted, "\x00")
}

// Get returns cached IPs if the entry exists and is fresher than ttl.
func (c *Cache) Get(key string, ttl time.Duration) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || ttl <= 0 || time.Since(e.timestamp) >= ttl {
		return nil, false
	}
	return e.ips, true
}

func (c *Cache) Set(key string, ips []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{timestamp: time.Now(), ips: ips}
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]entry)
}

func (c *Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
