// Package payments is a client for the payments service, and a stand-in
// payments service that runs inside the VM.
package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/opsschool/simulator/demoapp/internal/faults"
	"github.com/opsschool/simulator/demoapp/internal/metrics"
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

// Handler is the stand-in payments service: it approves 99% of requests
// after a few milliseconds.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authorize", func(w http.ResponseWriter, r *http.Request) {
		var req authorizeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AmountCents <= 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		time.Sleep(time.Duration(2+rand.IntN(6)) * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(authorizeResponse{
			Ref:      fmt.Sprintf("pay_%016x", rand.Uint64()),
			Approved: rand.IntN(100) != 0,
		})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	return mux
}
