package main

import (
	"sync"
	"unsafe"
)

// ShardedMap is a thread-safe map implementation using sharding to reduce contention.
// It uses RWMutex for read optimization and FNV64 hashing for key distribution.
type ShardedMap[K comparable, V any] struct {
	shards     []map[K]V
	shardMutex []sync.RWMutex
	shardCount uint64
}

// NewShardedMap creates a new ShardedMap with the specified number of shards.
// shardCount should be a power of 2 for optimal distribution.
func NewShardedMap[K comparable, V any](shardCount int) *ShardedMap[K, V] {
	if shardCount < 1 {
		shardCount = 1
	}
	sm := &ShardedMap[K, V]{
		shards:     make([]map[K]V, shardCount),
		shardMutex: make([]sync.RWMutex, shardCount),
		shardCount: uint64(shardCount),
	}
	for i := range sm.shards {
		sm.shards[i] = make(map[K]V)
	}
	return sm
}

// fnv64aHash computes FNV-1a 64-bit hash without allocations.
// This is an inline implementation of FNV-1a algorithm.
func fnv64aHash(data []byte) uint64 {
	const (
		offset64 uint64 = 14695981039346656037
		prime64  uint64 = 1099511628211
	)
	hash := offset64
	for _, b := range data {
		hash ^= uint64(b)
		hash *= prime64
	}
	return hash
}

// getShardIndex computes the shard index for a given key using FNV64 hashing.
// This function is designed to avoid allocations in the hot path.
func (sm *ShardedMap[K, V]) getShardIndex(key K) uint64 {
	var hash uint64
	switch k := any(key).(type) {
	case string:
		// Direct byte access for strings (no allocation)
		// Use unsafe to access string bytes directly
		hash = fnv64aHash(unsafe.Slice(unsafe.StringData(k), len(k)))
	case int:
		// Direct hashing for int (no allocation)
		// Use xxhash-style mixing for better distribution
		hash = uint64(k)
		hash ^= hash >> 33
		hash *= 0xff51afd7ed558ccd
		hash ^= hash >> 33
		hash *= 0xc4ceb9fe1a85ec53
		hash ^= hash >> 33
	case int64:
		hash = uint64(k)
		hash ^= hash >> 33
		hash *= 0xff51afd7ed558ccd
		hash ^= hash >> 33
		hash *= 0xc4ceb9fe1a85ec53
		hash ^= hash >> 33
	case uint64:
		hash = k
		hash ^= hash >> 33
		hash *= 0xff51afd7ed558ccd
		hash ^= hash >> 33
		hash *= 0xc4ceb9fe1a85ec53
		hash ^= hash >> 33
	case uint32:
		hash = uint64(k)
		hash ^= hash >> 33
		hash *= 0xff51afd7ed558ccd
		hash ^= hash >> 33
		hash *= 0xc4ceb9fe1a85ec53
		hash ^= hash >> 33
	case int32:
		hash = uint64(k)
		hash ^= hash >> 33
		hash *= 0xff51afd7ed558ccd
		hash ^= hash >> 33
		hash *= 0xc4ceb9fe1a85ec53
		hash ^= hash >> 33
	default:
		// Fallback: use FNV64 on the key's memory representation
		// This is safe for comparable types and avoids string conversion
		keyPtr := unsafe.Pointer(&key)
		keySize := unsafe.Sizeof(key)
		hash = fnv64aHash(unsafe.Slice((*byte)(keyPtr), keySize))
	}
	return hash % sm.shardCount
}

// Get retrieves a value from the map. Returns the value and a boolean indicating existence.
// Uses RLock for read optimization.
func (sm *ShardedMap[K, V]) Get(key K) (V, bool) {
	shardIndex := sm.getShardIndex(key)
	sm.shardMutex[shardIndex].RLock()
	defer sm.shardMutex[shardIndex].RUnlock()

	value, exists := sm.shards[shardIndex][key]
	return value, exists
}

// Set inserts or updates a value in the map.
// Uses Lock for write operations.
func (sm *ShardedMap[K, V]) Set(key K, value V) {
	shardIndex := sm.getShardIndex(key)
	sm.shardMutex[shardIndex].Lock()
	defer sm.shardMutex[shardIndex].Unlock()

	sm.shards[shardIndex][key] = value
}

// Delete removes a key from the map.
// Uses Lock for write operations.
func (sm *ShardedMap[K, V]) Delete(key K) {
	shardIndex := sm.getShardIndex(key)
	sm.shardMutex[shardIndex].Lock()
	defer sm.shardMutex[shardIndex].Unlock()

	delete(sm.shards[shardIndex], key)
}

// Keys returns all keys from all shards.
// This operation locks all shards to prevent data races during iteration.
// The order of keys is not guaranteed.
func (sm *ShardedMap[K, V]) Keys() []K {
	// Lock all shards for reading to ensure consistency
	for i := range sm.shardMutex {
		sm.shardMutex[i].RLock()
	}
	defer func() {
		for i := range sm.shardMutex {
			sm.shardMutex[i].RUnlock()
		}
	}()

	// Pre-allocate with estimated capacity to reduce allocations
	totalKeys := 0
	for i := range sm.shards {
		totalKeys += len(sm.shards[i])
	}

	keys := make([]K, 0, totalKeys)
	for i := range sm.shards {
		for key := range sm.shards[i] {
			keys = append(keys, key)
		}
	}

	return keys
}
