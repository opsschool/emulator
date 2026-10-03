// Package worker processes queued orders.
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/opsschool/emulator/demoapp/internal/metrics"
	"github.com/opsschool/emulator/demoapp/internal/store"
)

// Worker polls the order queue with a fixed number of goroutines.
type Worker struct {
	Store       *store.Store
	Log         *slog.Logger
	Concurrency int
	BufferMB    int // invoice render buffer each goroutine keeps
	Poll        time.Duration
	Batch       int
}

// Run processes orders until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx)
		}()
	}
	wg.Add(2)
	go func() { defer wg.Done(); w.every(ctx, 5*time.Second, w.reportDepth) }()
	go func() { defer wg.Done(); w.every(ctx, time.Minute, w.reconcile) }()
	w.Log.Info("worker started", "concurrency", w.Concurrency, "buffer_mb", w.BufferMB)
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	// Each goroutine renders invoices into its own buffer. Touch every page so
	// the memory is really in use, as a renderer's working set would be.
	buf := make([]byte, w.BufferMB<<20)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}
	for ctx.Err() == nil {
		n, err := w.Store.ClaimJobs(ctx, w.Batch, func(j store.Job) error {
			render(buf, j.OrderID)
			return nil
		})
		switch {
		case err != nil && ctx.Err() == nil:
			metrics.WorkerErrors.Inc()
			w.Log.Error("processing orders failed", "err", err)
			sleep(ctx, w.Poll*4)
		case n == 0:
			sleep(ctx, w.Poll)
		default:
			metrics.OrdersProcessed.Add(float64(n))
			w.Log.Debug("processed orders", "count", n)
		}
	}
}

// render simulates laying out an invoice: it walks the buffer.
func render(buf []byte, orderID int64) {
	b := byte(orderID)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] ^= b
	}
}

func (w *Worker) reportDepth(ctx context.Context) {
	if n, err := w.Store.QueueDepth(ctx); err == nil {
		metrics.QueueDepth.Set(float64(n))
	}
}

func (w *Worker) reconcile(ctx context.Context) {
	n, err := w.Store.Reconcile(ctx)
	if err != nil {
		w.Log.Error("reconcile failed", "err", err)
		return
	}
	if n > 0 {
		w.Log.Warn("orders stuck in pending", "count", n)
	}
}

func (w *Worker) every(ctx context.Context, d time.Duration, f func(context.Context)) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		f(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
