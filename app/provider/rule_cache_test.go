package provider

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestCompiledRuleRetentionIsBoundedAcrossConcurrentSources(t *testing.T) {
	var cache compiledRules
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("%d-%d", worker, i)
				cache.Store(key, i)
				cache.Load(key)
			}
		}(worker)
	}
	workers.Wait()
	if len(cache.entries) > compiledRuleCacheLimit || len(cache.order) > compiledRuleCacheLimit {
		t.Fatal("compiled rule cache grew beyond capacity")
	}
	cache.Store(strings.Repeat("x", 8193), "oversized")
	if _, ok := cache.Load(strings.Repeat("x", 8193)); ok {
		t.Fatal("oversized cache key retained")
	}
}
