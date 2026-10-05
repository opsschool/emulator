package payments

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestGatewayWorkers(t *testing.T) {
	// Two workers, 50ms each: six requests at once take three rounds.
	gw := Gateway{Concurrency: 2, MinLatency: 50 * time.Millisecond, MaxLatency: 50 * time.Millisecond}
	srv := httptest.NewServer(gw.Handler(t.Context()))
	defer srv.Close()
	c := NewClient(srv.URL, 5*time.Second, true, 0, 0)

	start := time.Now()
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Authorize(context.Background(), 1, 100)
		}()
	}
	wg.Wait()
	if d := time.Since(start); d < 150*time.Millisecond || d > time.Second {
		t.Errorf("six requests on two workers took %s, want about 150ms", d)
	}
}

func TestGatewayFinishesAbandonedRequests(t *testing.T) {
	// The caller gives up after 20ms; the gateway still spends its
	// worker on the request, so the next caller waits behind it.
	gw := Gateway{Concurrency: 1, MinLatency: 100 * time.Millisecond, MaxLatency: 100 * time.Millisecond}
	srv := httptest.NewServer(gw.Handler(t.Context()))
	defer srv.Close()
	impatient := NewClient(srv.URL, 20*time.Millisecond, true, 0, 0)
	if _, err := impatient.Authorize(context.Background(), 1, 100); err == nil {
		t.Fatal("want a timeout")
	}
	c := NewClient(srv.URL, 5*time.Second, true, 0, 0)
	start := time.Now()
	c.Authorize(context.Background(), 1, 100)
	if d := time.Since(start); d < 170*time.Millisecond {
		t.Errorf("second request took %s; the abandoned one should have held the worker", d)
	}
}

func TestGatewayDefault(t *testing.T) {
	srv := httptest.NewServer(Gateway{}.Handler(t.Context()))
	defer srv.Close()
	c := NewClient(srv.URL, time.Second, true, 2, 0)
	approved := 0
	for range 20 {
		if _, err := c.Authorize(context.Background(), 1, 100); err == nil {
			approved++
		}
	}
	if approved < 15 {
		t.Errorf("approved %d of 20", approved)
	}
}
