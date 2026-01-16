package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	// Test context cancellation
	// testContextCancellation()
	testContextCancellationWitherrorgroup()
}

// When the task slow 5 seconds done, the main thread will be blocked for 5 seconds.
func testContextCancellation() {
	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		secondNumer := 5
		time.Sleep(time.Duration(secondNumer) * time.Second)
		fmt.Printf("Task slow %d seconds done\n", secondNumer)
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("All task Done")
	case <-time.After(3 * time.Second):
		fmt.Println("Timeout")
	}
}

func testContextCancellationWitherrorgroup() {

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	g, groupCtx := errgroup.WithContext(ctx)

	urls := []string{
		"User Service",
		"Order Service",
		"Product Service",
	}

	for _, url := range urls {
		url := url
		g.Go(func() error {
			return mockAPICall(groupCtx, url)
		})
	}

	// 3. Đợi kết quả
	if err := g.Wait(); err != nil {
		fmt.Printf("Kết thúc với lỗi: %v\n", err)
	} else {
		fmt.Println("Tất cả API đã phản hồi thành công!")
	}
}

func mockAPICall(ctx context.Context, url string) error {
	var delay time.Duration
	switch url {
	case "User Service":
		delay = 1 * time.Second
	case "Order Service":
		delay = 2 * time.Second
	case "Product Service":
		delay = 5 * time.Second
	default:
		delay = 10 * time.Second
	}

	select {
	case <-time.After(delay):
		fmt.Printf("[Success] %s đã xong sau %v\n", url, delay)
		return nil
	case <-ctx.Done():
		fmt.Printf("[Cancelled] %s dừng lại vì: %v\n", url, ctx.Err())
		return ctx.Err()
	}
}
