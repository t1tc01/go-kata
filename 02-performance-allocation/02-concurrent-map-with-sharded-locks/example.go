package main

import (
	"fmt"
	"sync"
	"time"
)

// Example usage of ShardedMap for a rate limiter scenario
func main() {
	// Create a sharded map with 64 shards for high concurrency
	// This simulates a rate limiter tracking request counts per user
	rateLimiter := NewShardedMap[string, int](64)
	
	var wg sync.WaitGroup
	const numUsers = 1000
	const requestsPerUser = 100
	
	// Simulate concurrent requests from multiple users
	start := time.Now()
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			for j := 0; j < requestsPerUser; j++ {
				// Check current count
				count, exists := rateLimiter.Get(fmt.Sprintf("user-%d", userID))
				if !exists {
					count = 0
				}
				
				// Increment (simulating rate limit check)
				rateLimiter.Set(fmt.Sprintf("user-%d", userID), count+1)
			}
		}(i)
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	fmt.Printf("Processed %d users × %d requests = %d total operations\n", 
		numUsers, requestsPerUser, numUsers*requestsPerUser)
	fmt.Printf("Time taken: %v\n", duration)
	fmt.Printf("Throughput: %.0f ops/sec\n", 
		float64(numUsers*requestsPerUser*2)/duration.Seconds())
	
	// Verify final counts
	allKeys := rateLimiter.Keys()
	fmt.Printf("Total unique users tracked: %d\n", len(allKeys))
	
	// Sample a few users to verify correctness
	for i := 0; i < 5 && i < len(allKeys); i++ {
		userID := allKeys[i]
		count, _ := rateLimiter.Get(userID)
		fmt.Printf("  %s: %d requests\n", userID, count)
	}
}

