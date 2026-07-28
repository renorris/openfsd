package db

import (
	"context"
	"sync"
	"time"
)

// Default cache TTLs.
// User cache is short so password/ban changes propagate across nodes without mesh invalidate.
const (
	DefaultUserCacheTTL   = 5 * time.Second
	DefaultConfigCacheTTL = 2 * time.Second
)

type cacheEntry[T any] struct {
	val       T
	expiresAt time.Time
}

// UserCache is a required process cache for UserRepository reads.
// Invalidates on Create/Update/Delete for the affected CID.
type UserCache struct {
	inner UserRepository
	ttl   time.Duration
	mu    sync.RWMutex
	byCID map[int]cacheEntry[*User]
}

// NewUserCache wraps inner. ttl <= 0 uses DefaultUserCacheTTL.
func NewUserCache(inner UserRepository, ttl time.Duration) *UserCache {
	if ttl <= 0 {
		ttl = DefaultUserCacheTTL
	}
	return &UserCache{
		inner: inner,
		ttl:   ttl,
		byCID: make(map[int]cacheEntry[*User]),
	}
}

func (c *UserCache) GetUserByCID(ctx context.Context, cid int) (*User, error) {
	now := time.Now()
	c.mu.RLock()
	if e, ok := c.byCID[cid]; ok && now.Before(e.expiresAt) {
		u := e.val
		c.mu.RUnlock()
		return cloneUser(u), nil
	}
	c.mu.RUnlock()

	u, err := c.inner.GetUserByCID(ctx, cid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.byCID[cid] = cacheEntry[*User]{val: cloneUser(u), expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return u, nil
}

func (c *UserCache) CreateUser(ctx context.Context, user *User) error {
	if err := c.inner.CreateUser(ctx, user); err != nil {
		return err
	}
	c.invalidate(user.CID)
	return nil
}

func (c *UserCache) UpdateUser(ctx context.Context, user *User) error {
	if err := c.inner.UpdateUser(ctx, user); err != nil {
		return err
	}
	c.invalidate(user.CID)
	return nil
}

func (c *UserCache) DeleteUser(ctx context.Context, cid int) error {
	if err := c.inner.DeleteUser(ctx, cid); err != nil {
		return err
	}
	c.invalidate(cid)
	return nil
}

func (c *UserCache) ListUsers(ctx context.Context, filter UserListFilter) ([]*User, error) {
	return c.inner.ListUsers(ctx, filter)
}

func (c *UserCache) CountUsers(ctx context.Context, filter UserListFilter) (int, error) {
	return c.inner.CountUsers(ctx, filter)
}

func (c *UserCache) VerifyPasswordHash(plaintext, hash string) bool {
	return c.inner.VerifyPasswordHash(plaintext, hash)
}

func (c *UserCache) invalidate(cid int) {
	c.mu.Lock()
	delete(c.byCID, cid)
	c.mu.Unlock()
}

func cloneUser(u *User) *User {
	if u == nil {
		return nil
	}
	cp := *u
	if u.FirstName != nil {
		s := *u.FirstName
		cp.FirstName = &s
	}
	if u.LastName != nil {
		s := *u.LastName
		cp.LastName = &s
	}
	return &cp
}

// ConfigCache is a required process cache for ConfigRepository.
// JWT secret and other hot keys must go through this under multi-instance.
// Invalidates on Set; SetIfNotExists invalidates the key as well.
type ConfigCache struct {
	inner ConfigRepository
	ttl   time.Duration
	mu    sync.RWMutex
	byKey map[string]cacheEntry[string]
}

// NewConfigCache wraps inner. ttl <= 0 uses DefaultConfigCacheTTL.
func NewConfigCache(inner ConfigRepository, ttl time.Duration) *ConfigCache {
	if ttl <= 0 {
		ttl = DefaultConfigCacheTTL
	}
	return &ConfigCache{
		inner: inner,
		ttl:   ttl,
		byKey: make(map[string]cacheEntry[string]),
	}
}

func (c *ConfigCache) Get(ctx context.Context, key string) (string, error) {
	now := time.Now()
	c.mu.RLock()
	if e, ok := c.byKey[key]; ok && now.Before(e.expiresAt) {
		v := e.val
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	v, err := c.inner.Get(ctx, key)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.byKey[key] = cacheEntry[string]{val: v, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return v, nil
}

func (c *ConfigCache) Set(ctx context.Context, key, value string) error {
	if err := c.inner.Set(ctx, key, value); err != nil {
		return err
	}
	c.mu.Lock()
	c.byKey[key] = cacheEntry[string]{val: value, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return nil
}

func (c *ConfigCache) SetIfNotExists(ctx context.Context, key, value string) error {
	if err := c.inner.SetIfNotExists(ctx, key, value); err != nil {
		return err
	}
	// Value may or may not have been written; invalidate so next Get is fresh.
	c.mu.Lock()
	delete(c.byKey, key)
	c.mu.Unlock()
	return nil
}
