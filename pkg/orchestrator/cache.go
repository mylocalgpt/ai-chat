package orchestrator

import (
	"sync"
	"time"

	"github.com/mylocalgpt/ai-chat/pkg/store"
)

const defaultCacheTTL = 30 * time.Second

type modelCache struct {
	mu       sync.RWMutex
	configs  map[string]store.ModelConfig
	loadedAt time.Time
	ttl      time.Duration
}

func newModelCache(ttl time.Duration) *modelCache {
	return &modelCache{
		configs: make(map[string]store.ModelConfig),
		ttl:     ttl,
	}
}

// get returns the config for a role and whether it was found.
func (c *modelCache) get(role string) (store.ModelConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cfg, ok := c.configs[role]
	return cfg, ok
}

// isStale returns true if the cache has never been loaded or the TTL has elapsed.
func (c *modelCache) isStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loadedAt.IsZero() || time.Since(c.loadedAt) > c.ttl
}

// refresh replaces all cached configs with the given list.
func (c *modelCache) refresh(configs []store.ModelConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configs = make(map[string]store.ModelConfig, len(configs))
	for _, cfg := range configs {
		c.configs[cfg.Role] = cfg
	}
	c.loadedAt = time.Now()
}
