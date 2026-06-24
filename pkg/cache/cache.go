package cache

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache 缓存接口
type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Close() error
}

// ─── Redis 缓存实现 ───

// RedisCache 基于 Redis 的缓存实现
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache 创建 Redis 缓存实例
func NewRedisCache(addr, password string, db int) *RedisCache {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisCache{client: client}
}

func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) error {
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return ErrCacheMiss
		}
		return err
	}
	return json.Unmarshal(data, dest)
}

func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *RedisCache) Close() error {
	return c.client.Close()
}

// Ping 测试 Redis 连接
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// ─── 内存缓存实现 ───

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

func (c *MemoryCache) Close() error { return nil }

// ─── 空缓存实现 ───

// NoopCache 空缓存（缓存未启用时）
type NoopCache struct{}

func NewNoopCache() *NoopCache                                            { return &NoopCache{} }
func (c *NoopCache) Get(_ context.Context, _ string, _ interface{}) error { return ErrCacheMiss }
func (c *NoopCache) Set(_ context.Context, _ string, _ interface{}, _ time.Duration) error {
	return nil
}
func (c *NoopCache) Delete(_ context.Context, _ string) error         { return nil }
func (c *NoopCache) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (c *NoopCache) Close() error                                     { return nil }

// ─── 通用 ───

// Cache 错误
type cacheError string

func (e cacheError) Error() string { return string(e) }

const ErrCacheMiss = cacheError("cache miss")

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled  bool
	Host     string
	Port     string
	Password string
	DB       int
}

// NewCache 创建缓存实例
func NewCache(cfg CacheConfig) Cache {
	if !cfg.Enabled || cfg.Host == "" {
		log.Printf("📦 缓存未启用，使用内存缓存")
		return NewMemoryCache()
	}

	addr := cfg.Host + ":" + cfg.Port
	redisCache := NewRedisCache(addr, cfg.Password, cfg.DB)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := redisCache.Ping(ctx); err != nil {
		log.Printf("⚠️  Redis 连接失败（%s），回退到内存缓存: %v", addr, err)
		return NewMemoryCache()
	}

	log.Printf("✅ Redis 缓存已连接: %s", addr)
	return redisCache
}
