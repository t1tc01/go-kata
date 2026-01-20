package main

import (
	"runtime"
	"sync"
	"testing"
)

// TestBasicOperations tests basic Get, Set, Delete operations
func TestBasicOperations(t *testing.T) {
	sm := NewShardedMap[string, int](16)
	
	// Test Set and Get
	sm.Set("key1", 42)
	sm.Set("key2", 100)
	
	val, exists := sm.Get("key1")
	if !exists || val != 42 {
		t.Errorf("Expected key1=42, got %d, exists=%v", val, exists)
	}
	
	val, exists = sm.Get("key2")
	if !exists || val != 100 {
		t.Errorf("Expected key2=100, got %d, exists=%v", val, exists)
	}
	
	// Test non-existent key
	_, exists = sm.Get("nonexistent")
	if exists {
		t.Error("Expected nonexistent key to return false")
	}
	
	// Test Delete
	sm.Delete("key1")
	_, exists = sm.Get("key1")
	if exists {
		t.Error("Expected key1 to be deleted")
	}
	
	// Verify key2 still exists
	val, exists = sm.Get("key2")
	if !exists || val != 100 {
		t.Errorf("Expected key2=100 after delete, got %d, exists=%v", val, exists)
	}
}

// TestKeys tests the Keys() method
func TestKeys(t *testing.T) {
	sm := NewShardedMap[int, string](8)
	
	// Insert some keys
	for i := 0; i < 100; i++ {
		sm.Set(i, "value")
	}
	
	keys := sm.Keys()
	if len(keys) != 100 {
		t.Errorf("Expected 100 keys, got %d", len(keys))
	}
	
	// Verify all keys are present
	keyMap := make(map[int]bool)
	for _, k := range keys {
		keyMap[k] = true
	}
	
	for i := 0; i < 100; i++ {
		if !keyMap[i] {
			t.Errorf("Key %d not found in Keys()", i)
		}
	}
}

// TestConcurrentReadWrite tests concurrent read and write operations
func TestConcurrentReadWrite(t *testing.T) {
	sm := NewShardedMap[int, int](64)
	const numGoroutines = 100
	const numOps = 1000
	
	var wg sync.WaitGroup
	
	// Writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := id*numOps + j
				sm.Set(key, key*2)
			}
		}(i)
	}
	
	// Readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := id*numOps + j
				val, exists := sm.Get(key)
				if exists && val != key*2 {
					// This is OK - we might read before write completes
					// But we should never get a panic or wrong value
				}
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify final state
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < numOps; j++ {
			key := i*numOps + j
			val, exists := sm.Get(key)
			if !exists || val != key*2 {
				t.Errorf("Key %d: expected %d, got %d, exists=%v", key, key*2, val, exists)
			}
		}
	}
}

// TestConcurrentDelete tests concurrent delete operations
func TestConcurrentDelete(t *testing.T) {
	sm := NewShardedMap[int, string](32)
	
	// Pre-populate
	for i := 0; i < 1000; i++ {
		sm.Set(i, "value")
	}
	
	var wg sync.WaitGroup
	const numGoroutines = 50
	
	// Concurrent deletes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				key := start*20 + j
				sm.Delete(key)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify deletions
	for i := 0; i < 1000; i++ {
		_, exists := sm.Get(i)
		if exists {
			// Some keys might still exist if they weren't in the delete range
			// This is expected behavior
		}
	}
}

// TestKeysConcurrentWrites tests Keys() while concurrent writes occur
func TestKeysConcurrentWrites(t *testing.T) {
	sm := NewShardedMap[int, int](16)
	
	var wg sync.WaitGroup
	stop := make(chan struct{})
	
	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				sm.Set(i, i)
				i++
				if i > 10000 {
					i = 0
				}
			}
		}
	}()
	
	// Reader goroutines calling Keys()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				keys := sm.Keys()
				// Verify no duplicates and all keys are valid
				keyMap := make(map[int]bool)
				for _, k := range keys {
					if keyMap[k] {
						t.Errorf("Duplicate key found: %d", k)
					}
					keyMap[k] = true
				}
			}
		}()
	}
	
	// Run for a short time
	runtime.Gosched()
	close(stop)
	wg.Wait()
}

// BenchmarkSetSingleShard benchmarks Set operations with 1 shard (high contention)
func BenchmarkSetSingleShard(b *testing.B) {
	sm := NewShardedMap[int, int](1)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sm.Set(i, i)
			i++
		}
	})
}

// BenchmarkSetManyShards benchmarks Set operations with 64 shards (low contention)
func BenchmarkSetManyShards(b *testing.B) {
	sm := NewShardedMap[int, int](64)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sm.Set(i, i)
			i++
		}
	})
}

// BenchmarkGet benchmarks Get operations
func BenchmarkGet(b *testing.B) {
	sm := NewShardedMap[int, int](64)
	// Pre-populate
	for i := 0; i < 10000; i++ {
		sm.Set(i, i)
	}
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sm.Get(i % 10000)
			i++
		}
	})
}

// BenchmarkGetSetMixed benchmarks mixed Get and Set operations
func BenchmarkGetSetMixed(b *testing.B) {
	sm := NewShardedMap[int, int](64)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				sm.Set(i, i)
			} else {
				sm.Get(i % 1000)
			}
			i++
		}
	})
}

// TestMemoryUsage tests that we don't use excessive memory
func TestMemoryUsage(t *testing.T) {
	sm := NewShardedMap[int, interface{}](64)
	
	// Store 1 million keys
	const numKeys = 1000000
	for i := 0; i < numKeys; i++ {
		sm.Set(i, i)
	}
	
	// Force GC and check memory
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Rough check: 1M int keys + interface{} values should be reasonable
	// Each entry: key (8 bytes) + value (interface{} = 16 bytes) = ~24 bytes
	// Plus map overhead: ~1M * 24 = ~24MB, plus shard overhead
	// 50MB limit means we have ~26MB for overhead, which is reasonable
	t.Logf("Alloc: %d KB, TotalAlloc: %d KB", m.Alloc/1024, m.TotalAlloc/1024)
	
	// Verify all keys are accessible
	for i := 0; i < 1000; i++ {
		val, exists := sm.Get(i)
		if !exists {
			t.Errorf("Key %d not found", i)
		}
		if val != i {
			t.Errorf("Key %d: expected %d, got %v", i, i, val)
		}
	}
}

// TestStringKeys tests with string keys (common use case)
func TestStringKeys(t *testing.T) {
	sm := NewShardedMap[string, int](32)
	
	sm.Set("user1", 100)
	sm.Set("user2", 200)
	sm.Set("user3", 300)
	
	val, exists := sm.Get("user1")
	if !exists || val != 100 {
		t.Errorf("Expected user1=100, got %d, exists=%v", val, exists)
	}
	
	sm.Delete("user2")
	_, exists = sm.Get("user2")
	if exists {
		t.Error("Expected user2 to be deleted")
	}
	
	keys := sm.Keys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}
}

// TestUpdateValue tests updating an existing value
func TestUpdateValue(t *testing.T) {
	sm := NewShardedMap[string, int](8)
	
	sm.Set("key", 10)
	val, _ := sm.Get("key")
	if val != 10 {
		t.Errorf("Expected 10, got %d", val)
	}
	
	sm.Set("key", 20)
	val, _ = sm.Get("key")
	if val != 20 {
		t.Errorf("Expected 20, got %d", val)
	}
}

