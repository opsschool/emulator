// Package edge stands in for the load balancer in front of the shop. Real
// traffic would pass through one, and it would see what customers see even
// when requests never reach the shop: refused or dropped connections, and
// timeouts. Edge records each load generator result the way such a load
// balancer would: Prometheus metrics, and an access log shipped to Loki.
package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/opsschool/emulator/internal/loadgen"
)

// Name is the load balancer's host name in metrics and logs.
const Name = "edge-1"

// Upstream is how the load balancer names the shop.
const Upstream = "shop-vm:80"

// maxBuffered caps log lines waiting for Loki, so a Loki outage can't grow
// memory without bound. Older lines are dropped first.
const maxBuffered = 20000

// Edge records results. Safe for concurrent use.
type Edge struct {
	// Registry holds the edge metrics.
	Registry *prometheus.Registry
	// LokiURL is Loki's base URL. Empty disables the access log.
	LokiURL string
	Client  *http.Client

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec

	mu    sync.Mutex
	lines [][2]string // [unix nanoseconds, line]
}

// New returns an edge that ships its access log to lokiURL.
func New(lokiURL string) *Edge {
	e := &Edge{
		Registry: prometheus.NewRegistry(),
		LokiURL:  lokiURL,
		Client:   &http.Client{Timeout: 5 * time.Second},
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "edge_requests_total",
			Help: "Requests the load balancer received, by route and the status it returned to the customer.",
		}, []string{"route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "edge_request_duration_seconds",
			Help:    "Time from the customer's request to the load balancer's response.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 15},
		}, []string{"route"}),
	}
	e.Registry.MustRegister(e.requests, e.duration)
	return e
}

// Status returns the status the load balancer gives the customer for r, and
// the reason when the shop sent no response.
func Status(r loadgen.Result) (int, string) {
	if r.Err == nil {
		return r.Code, ""
	}
	var ne net.Error
	switch {
	case errors.Is(r.Err, context.DeadlineExceeded), errors.As(r.Err, &ne) && ne.Timeout():
		return http.StatusGatewayTimeout, "upstream timed out while connecting to upstream"
	case errors.Is(r.Err, syscall.ECONNREFUSED):
		return http.StatusBadGateway, "connection refused while connecting to upstream"
	case errors.Is(r.Err, syscall.ECONNRESET):
		return http.StatusBadGateway, "connection reset by peer while reading response header from upstream"
	case errors.Is(r.Err, syscall.EHOSTUNREACH), errors.Is(r.Err, syscall.ENETUNREACH):
		return http.StatusBadGateway, "no route to host while connecting to upstream"
	}
	return http.StatusBadGateway, "upstream prematurely closed connection while reading response header from upstream"
}

// Observe records one result.
func (e *Edge) Observe(r loadgen.Result) {
	code, reason := Status(r)
	e.requests.WithLabelValues(r.Route, strconv.Itoa(code)).Inc()
	e.duration.WithLabelValues(r.Route).Observe(r.Duration.Seconds())
	if e.LokiURL == "" {
		return
	}
	now := time.Now()
	line := fmt.Sprintf(`%s - - [%s] "%s %s HTTP/1.1" %d rt=%.3f upstream=%s ua="shop-web/2.3"`,
		clientIP(r.Customer), now.Format("02/Jan/2006:15:04:05 -0700"), r.Method, r.Path, code, r.Duration.Seconds(), Upstream)
	if reason != "" {
		line += fmt.Sprintf(` error="%s"`, reason)
	}
	e.mu.Lock()
	e.lines = append(e.lines, [2]string{strconv.FormatInt(now.UnixNano(), 10), line})
	if n := len(e.lines); n > maxBuffered {
		e.lines = e.lines[n-maxBuffered:]
	}
	e.mu.Unlock()
}

// Run ships the access log to Loki every second until ctx is done.
func (e *Edge) Run(ctx context.Context) {
	if e.LokiURL == "" {
		return
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		e.flush(ctx)
	}
}

func (e *Edge) flush(ctx context.Context) {
	e.mu.Lock()
	lines := e.lines
	e.lines = nil
	e.mu.Unlock()
	if len(lines) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{"streams": []map[string]any{{
		"stream": map[string]string{"job": "edge", "host": Name},
		"values": lines,
	}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(e.LokiURL, "/")+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode < 300 {
			return
		}
	}
	// Keep the lines for the next try. Loki may be starting.
	e.mu.Lock()
	e.lines = append(lines, e.lines...)
	if n := len(e.lines); n > maxBuffered {
		e.lines = e.lines[n-maxBuffered:]
	}
	e.mu.Unlock()
}

// clientIP gives each customer a stable address in the documentation ranges.
func clientIP(customer int) string {
	h := fnv.New32a()
	fmt.Fprint(h, customer)
	v := h.Sum32()
	nets := []string{"198.51.100", "203.0.113", "192.0.2"}
	return fmt.Sprintf("%s.%d", nets[v%3], 1+(v>>8)%254)
}
