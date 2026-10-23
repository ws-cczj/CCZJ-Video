package provider

import "sync"

const compiledRuleCacheLimit = 512

// compiledRules memoizes immutable compilation results, including failures.
// FIFO eviction and a key-size ceiling bound retention across deleted sources.
type compiledRules struct {
	mu      sync.Mutex
	entries map[string]any
	order   []string
	next    int
}

func (c *compiledRules) Load(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.entries[key]
	return value, ok
}
func (c *compiledRules) Store(key string, value any) {
	if len(key) > 8192 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]any)
	}
	if _, exists := c.entries[key]; exists {
		c.entries[key] = value
		return
	}
	if len(c.order) < compiledRuleCacheLimit {
		c.order = append(c.order, key)
	} else {
		delete(c.entries, c.order[c.next])
		c.order[c.next] = key
		c.next = (c.next + 1) % compiledRuleCacheLimit
	}
	c.entries[key] = value
}
