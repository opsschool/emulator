// Package payments is a client for the payments service, and a stand-in
// payments service that runs inside the VM.
package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/opsschool/emulator/demoapp/internal/faults"
	"github.com/opsschool/emulator/demoapp/internal/metrics"
)

// ErrDeclined is returned when the payment is declined. It is not retried.
var ErrDeclined = errors.New("payment declined")

// Client authorizes payments with retries.
type Client struct {
	BaseURL  string
	HTTP     *http.Client
	RetryMax int
	Backoff  time.Duration
}

// NewClient builds a client. With keepAlive false every call opens a new
// TCP connection.
func NewClient(baseURL string, timeout time.Duration, keepAlive bool, retryMax int, backoff time.Duration) *Client {
	tr := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
		DisableKeepAlives:   !keepAlive,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Transport: tr, Timeout: timeout}, RetryMax: retryMax, Backoff: backoff}
}

type authorizeRequest struct {
	CustomerID  int64 `json:"customer_id"`
	AmountCents int64 `json:"amount_cents"`
}

type authorizeResponse struct {
	Ref      string `json:"ref"`
	Approved bool   `json:"approved"`
}

// Authorize returns a payment reference.
func (c *Client) Authorize(ctx context.Context, customerID, amountCents int64) (string, error) {
	body, _ := json.Marshal(authorizeRequest{CustomerID: customerID, AmountCents: amountCents})
	var lastErr error
	for attempt := 0; attempt <= c.RetryMax; attempt++ {
		if attempt > 0 {
			if err := c.wait(ctx, attempt); err != nil {
				return "", err
			}
		}
		ref, err := c.once(ctx, body)
		if err == nil || errors.Is(err, ErrDeclined) {
			return ref, err
		}
		lastErr = err
	}
	return "", lastErr
}

// wait sleeps before a retry: Backoff doubled per attempt, with jitter.
func (c *Client) wait(ctx context.Context, attempt int) error {
	if faults.NoBackoff || c.Backoff <= 0 {
		return ctx.Err()
	}
	d := c.Backoff << (attempt - 1)
	d = d/2 + rand.N(d/2+1)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) once(ctx context.Context, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/authorize", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		metrics.PaymentRequests.WithLabelValues("error").Inc()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		metrics.PaymentRequests.WithLabelValues("error").Inc()
		return "", fmt.Errorf("payments: %s", resp.Status)
	}
	var ar authorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		metrics.PaymentRequests.WithLabelValues("error").Inc()
		return "", err
	}
	if !ar.Approved {
		metrics.PaymentRequests.WithLabelValues("declined").Inc()
		return "", ErrDeclined
	}
	metrics.PaymentRequests.WithLabelValues("approved").Inc()
	return ar.Ref, nil
}

// Gateway configures the stand-in payments service. The zero value
// approves at once (2-8ms) with no limit on concurrent requests.
type Gateway struct {
	// Concurrency is how many authorizations run at once; 0 means no
	// limit. The rest wait in line, and like many real services the
	// gateway finishes a request even after its caller has given up.
	Concurrency int
	// MinLatency and MaxLatency bound how long an authorization takes.
	MinLatency, MaxLatency time.Duration
	Log                    *slog.Logger // nil: no queue reports
}

// Handler returns the stand-in payments service, which approves 99% of
// requests. Until ctx ends it logs the queue every ten seconds while
// requests are waiting.
func (g Gateway) Handler(ctx context.Context) http.Handler {
	lo, hi := g.MinLatency, g.MaxLatency
	if lo <= 0 && hi <= 0 {
		lo, hi = 2*time.Millisecond, 8*time.Millisecond
	}
	hi = max(hi, lo)
	var slots chan struct{}
	if g.Concurrency > 0 {
		slots = make(chan struct{}, g.Concurrency)
	}
	var waiting atomic.Int64
	if slots != nil && g.Log != nil {
		go g.report(ctx, &waiting)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authorize", func(w http.ResponseWriter, r *http.Request) {
		var req authorizeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AmountCents <= 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if slots != nil {
			waiting.Add(1)
			slots <- struct{}{}
			waiting.Add(-1)
			defer func() { <-slots }()
		}
		time.Sleep(lo + rand.N(hi-lo+1))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(authorizeResponse{
			Ref:      fmt.Sprintf("pay_%016x", rand.Uint64()),
			Approved: rand.IntN(100) != 0,
		})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	return mux
}

func (g Gateway) report(ctx context.Context, waiting *atomic.Int64) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := waiting.Load(); n > 0 {
				g.Log.Warn("authorizations waiting for a worker", "waiting", n, "workers", g.Concurrency)
			}
		}
	}
}
