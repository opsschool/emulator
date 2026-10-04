// Package loadgen drives realistic shop traffic from the host: browsing,
// viewing products and placing orders, at a rate set by a profile.
package loadgen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Step is one stage of a rate schedule.
type Step struct {
	RPS      float64
	Duration time.Duration
}

// Profile returns the request rate at an offset from the start.
type Profile interface {
	RateAt(elapsed time.Duration) float64
}

// Steady is a constant rate.
type Steady float64

func (s Steady) RateAt(time.Duration) float64 { return float64(s) }

// Peak runs at Base and bursts to Base*Factor for Burst out of every Period.
type Peak struct {
	Base   float64
	Factor float64
	Period time.Duration
	Burst  time.Duration
}

func (p Peak) RateAt(e time.Duration) float64 {
	if e%p.Period >= p.Period-p.Burst {
		return p.Base * p.Factor
	}
	return p.Base
}

// Schedule repeats its steps in order.
type Schedule []Step

func (s Schedule) RateAt(e time.Duration) float64 {
	var total time.Duration
	for _, st := range s {
		total += st.Duration
	}
	if total == 0 {
		return 0
	}
	e %= total
	for _, st := range s {
		if e < st.Duration {
			return st.RPS
		}
		e -= st.Duration
	}
	return 0
}

// Default rates, sized so the single-node image handles them comfortably.
const (
	DefaultRPS  = 20
	PeakFactor  = 4
	PeakPeriod  = 3 * time.Minute
	PeakBurst   = 45 * time.Second
	maxInFlight = 256
)

// ProfileByName returns a built-in profile.
func ProfileByName(name string, schedule []Step) (Profile, error) {
	switch {
	case len(schedule) > 0:
		return Schedule(schedule), nil
	case name == "" || name == "steady":
		return Steady(DefaultRPS), nil
	case name == "peak":
		return Peak{Base: DefaultRPS, Factor: PeakFactor, Period: PeakPeriod, Burst: PeakBurst}, nil
	}
	return nil, fmt.Errorf("unknown load profile %q", name)
}

// AtPeak returns a profile that holds the highest rate p reaches. Fix
// verification replays load at peak.
func AtPeak(p Profile) Profile {
	switch v := p.(type) {
	case Steady:
		return Steady(float64(v) * PeakFactor)
	case Peak:
		return Steady(v.Base * v.Factor)
	case Schedule:
		hi := 0.0
		for _, st := range v {
			hi = max(hi, st.RPS)
		}
		return Steady(hi)
	}
	return p
}

// Stats counts results. Safe for concurrent use.
type Stats struct {
	Sent, OK, ClientErr, ServerErr, Failed atomic.Int64
}

// Result is the outcome of one request.
type Result struct {
	Method, Path, Route string
	Customer            int
	// Code is the HTTP status, or 0 when the request got no response.
	Code     int
	Err      error
	Duration time.Duration
}

// Generator sends traffic to a shop.
type Generator struct {
	BaseURL   string
	Client    *http.Client
	Stats     Stats
	Products  int // catalog size to draw product IDs from
	Customers int
	// NewConnShare is the share of requests sent on a fresh connection, the
	// way first visits from new customers arrive. The rest reuse idle
	// connections.
	NewConnShare float64
	// OnResult, if set, is called after every request.
	OnResult func(Result)

	mu          sync.Mutex
	recentOrder []int64
	Profile     Profile // guarded by mu after Run starts; use SetProfile
}

// SetProfile changes the rate profile of a running generator.
func (g *Generator) SetProfile(p Profile) {
	g.mu.Lock()
	g.Profile = p
	g.mu.Unlock()
}

// CurrentProfile returns the active profile.
func (g *Generator) CurrentProfile() Profile {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.Profile
}

// New returns a generator with sensible defaults for the seeded shop.
func New(baseURL string, p Profile) *Generator {
	return &Generator{
		BaseURL:   baseURL,
		Profile:   p,
		Client:    &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: maxInFlight}},
		Products:  5000,
		Customers: 200000,
	}
}

// Run sends requests until ctx is done. Arrivals are open-loop (Poisson at
// the profile's rate), so a slow shop builds up in-flight requests the way
// real traffic does, capped at maxInFlight.
func (g *Generator) Run(ctx context.Context) {
	start := time.Now()
	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		rate := g.CurrentProfile().RateAt(time.Since(start))
		wait := time.Second
		if rate > 0 {
			wait = time.Duration(rand.ExpFloat64() / rate * float64(time.Second))
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if rate <= 0 {
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			g.Stats.Failed.Add(1) // shop is too slow; drop, like an impatient user
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			g.one(ctx)
		}()
	}
}

func (g *Generator) one(ctx context.Context) {
	customer := 1 + rand.IntN(g.Customers)
	var (
		method = http.MethodGet
		path   string
		route  string
		body   []byte
	)
	switch n := rand.IntN(100); {
	case n < 45:
		path, route = "/products?page="+strconv.Itoa(1+rand.IntN(20)), "GET /products"
	case n < 75:
		path, route = "/products/"+strconv.Itoa(g.productID()), "GET /products/{id}"
	case n < 88:
		product := g.productID()
		if rand.IntN(50) == 0 {
			product = g.Products + 1 + rand.IntN(1000) // a discontinued product from a stale page
		}
		method, path, route = http.MethodPost, "/orders", "POST /orders"
		body, _ = json.Marshal(map[string]int{"customer_id": customer, "product_id": product, "quantity": 1 + rand.IntN(3)})
	case n < 95:
		id, ok := g.recentOrderID()
		if !ok {
			return
		}
		path, route = "/orders/"+strconv.FormatInt(id, 10), "GET /orders/{id}"
	default:
		path, route = "/customers/"+strconv.Itoa(customer)+"/orders", "GET /customers/{id}/orders"
	}
	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shop-web/2.3")
	req.Header.Set("X-Customer-ID", strconv.Itoa(customer))
	req.Close = g.NewConnShare > 0 && rand.Float64() < g.NewConnShare
	g.Stats.Sent.Add(1)
	res := Result{Method: method, Path: path, Route: route, Customer: customer}
	start := time.Now()
	resp, err := g.Client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			g.Stats.Failed.Add(1)
			res.Err, res.Duration = err, time.Since(start)
			g.report(res)
		}
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		g.Stats.ServerErr.Add(1)
	case resp.StatusCode >= 400:
		g.Stats.ClientErr.Add(1)
	default:
		g.Stats.OK.Add(1)
	}
	if route == "POST /orders" && resp.StatusCode == http.StatusCreated {
		var o struct{ ID int64 }
		if json.NewDecoder(resp.Body).Decode(&o) == nil && o.ID > 0 {
			g.rememberOrder(o.ID)
		}
	}
	io.Copy(io.Discard, resp.Body)
	res.Code, res.Duration = resp.StatusCode, time.Since(start)
	g.report(res)
}

func (g *Generator) report(r Result) {
	if g.OnResult != nil {
		g.OnResult(r)
	}
}

// productID favours popular products, as real catalogs do.
func (g *Generator) productID() int {
	if rand.IntN(10) < 7 {
		return 1 + rand.IntN(max(g.Products/20, 1))
	}
	return 1 + rand.IntN(g.Products)
}

func (g *Generator) rememberOrder(id int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recentOrder = append(g.recentOrder, id)
	if len(g.recentOrder) > 1000 {
		g.recentOrder = g.recentOrder[500:]
	}
}

func (g *Generator) recentOrderID() (int64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.recentOrder) == 0 {
		return 0, false
	}
	return g.recentOrder[rand.IntN(len(g.recentOrder))], true
}
