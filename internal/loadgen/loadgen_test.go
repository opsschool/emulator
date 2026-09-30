package loadgen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProfiles(t *testing.T) {
	p := Peak{Base: 10, Factor: 4, Period: time.Minute, Burst: 10 * time.Second}
	if p.RateAt(0) != 10 || p.RateAt(55*time.Second) != 40 || p.RateAt(65*time.Second) != 10 {
		t.Error("peak profile wrong")
	}
	s := Schedule{{RPS: 5, Duration: time.Second}, {RPS: 50, Duration: time.Second}}
	if s.RateAt(500*time.Millisecond) != 5 || s.RateAt(1500*time.Millisecond) != 50 || s.RateAt(2500*time.Millisecond) != 5 {
		t.Error("schedule profile wrong")
	}
	if AtPeak(p).RateAt(0) != 40 || AtPeak(s).RateAt(0) != 50 || AtPeak(Steady(20)).RateAt(0) != 80 {
		t.Error("AtPeak wrong")
	}
	if _, err := ProfileByName("bursty", nil); err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestRunMix(t *testing.T) {
	var orders, products atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orders":
			orders.Add(1)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id": 42}`))
		case r.URL.Path == "/products":
			products.Add(1)
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	g := New(srv.URL, Steady(400))
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	g.Run(ctx)
	sent := g.Stats.Sent.Load()
	if sent < 300 || sent > 900 {
		t.Errorf("sent %d requests in 1.5s at 400 rps", sent)
	}
	if orders.Load() == 0 || products.Load() == 0 || g.Stats.ServerErr.Load() != 0 {
		t.Errorf("orders=%d products=%d 5xx=%d", orders.Load(), products.Load(), g.Stats.ServerErr.Load())
	}
}
