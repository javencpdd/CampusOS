package cache

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// Cache 缓存接口
type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// MemoryCache 内存缓存实现（Redis 不可用时回退）
type MemoryCache struct {
	data map[string]cacheItem
}

type cacheItem struct {
	value     []byte
	expiresAt time.Time
}

// NewMemoryCache 创建内存缓存
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{data: make(map[string]cacheItem)}
}

func (c *MemoryCache) Get(_ context.Context, key string, dest interface{}) error {
	item, ok := c.data[key]
	if !ok {
		return ErrCacheMiss
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		delete(c.data, key)
		return ErrCacheMiss
	}
	return json.Unmarshal(item.value, dest)
}

func (c *MemoryCache) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	item := cacheItem{value: data}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	c.data[key] = item
	return nil
}

func (c *MemoryCache) Delete(_ context.Context, key string) error {
	delete(c.data, key)
	return nil
}

func (c *MemoryCache) Exists(_ context.Context, key string) (bool, error) {
	item, ok := c.data[key]
	if !ok {
		return false, nil
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		delete(c.data, key)
		return false, nil
	}
	return true, nil
}

// NoopCache 空缓存（缓存未启用时）
type NoopCache struct{}

func NewNoopCache() *NoopCache { return &NoopCache{} }

func (c *NoopCache) Get(_ context.Context, _ string, _ interface{}) error { return ErrCacheMiss }
func (c *NoopCache) Set(_ context.Context, _ string, _ interface{}, _ time.Duration) error {
	return nil
}
func (c *NoopCache) Delete(_ context.Context, _ string) error         { return nil }
func (c *NoopCache) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

// Cache 错误
type cacheError string

func (e cacheError) Error() string { return string(e) }

const ErrCacheMiss = cacheError("cache miss")

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled bool
	Host    string
	Port    string
}

// NewCache 创建缓存实例
func NewCache(cfg CacheConfig) Cache {
	if !cfg.Enabled || cfg.Host == "" {
		log.Printf("📦 缓存未启用，使用内存缓存")
		return NewMemoryCache()
	}
	// TODO: 未来接入 Redis
	// redisClient := redis.NewClient(&redis.Options{
	//     Addr: cfg.Host + ":" + cfg.Port,
	// })
	log.Printf("📦 Redis 缓存功能待实现，回退到内存缓存")
	return NewMemoryCache()
}
