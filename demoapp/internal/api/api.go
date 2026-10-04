// Package api serves the shop's public HTTP API and its admin endpoints.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/opsschool/emulator/demoapp/internal/cache"
	"github.com/opsschool/emulator/demoapp/internal/faults"
	"github.com/opsschool/emulator/demoapp/internal/metrics"
	"github.com/opsschool/emulator/demoapp/internal/payments"
	"github.com/opsschool/emulator/demoapp/internal/store"
)

// Server holds the API's dependencies.
type Server struct {
	Store        *store.Store
	Cache        *cache.Cache
	Payments     *payments.Client
	Log          *slog.Logger
	Version      string
	QueryTimeout time.Duration
	SessionDir   string

	recent recentlyViewed
}

// Handler returns the public API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /products", s.listProducts)
	mux.HandleFunc("GET /products/{id}", s.getProduct)
	mux.HandleFunc("POST /orders", s.createOrder)
	mux.HandleFunc("GET /orders/{id}", s.getOrder)
	mux.HandleFunc("GET /customers/{id}/orders", s.customerOrders)
	return s.instrument(mux)
}

// AdminHandler returns /metrics and pprof.
func AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// instrument records metrics and, at debug level, logs every request.
func (s *Server) instrument(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		mux.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		d := time.Since(start)
		metrics.HTTPRequests.WithLabelValues(route, strconv.Itoa(sw.code)).Inc()
		metrics.HTTPDuration.WithLabelValues(route).Observe(d.Seconds())
		s.Log.Debug("request", "route", route, "path", r.URL.Path, "code", sw.code,
			"duration_ms", float64(d.Microseconds())/1000, "remote", r.RemoteAddr,
			"user_agent", r.UserAgent(), "customer", r.Header.Get("X-Customer-ID"))
	})
}

func (s *Server) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), s.QueryTimeout)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, payments.ErrDeclined):
		writeJSON(w, http.StatusPaymentRequired, map[string]string{"error": "payment declined"})
	default:
		s.Log.Error("request failed", "route", r.Pattern, "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		s.Log.Error("health check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "version": s.Version})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	ps, err := cache.Get(ctx, s.Cache, fmt.Sprintf("products:page:%d", page), func() ([]store.Product, error) {
		return s.Store.ListProducts(ctx, page)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) getProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, store.ErrNotFound)
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	p, err := cache.Get(ctx, s.Cache, fmt.Sprintf("product:%d", id), func() (store.Product, error) {
		return s.Store.GetProduct(ctx, id)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if c := r.Header.Get("X-Customer-ID"); c != "" {
		s.recent.add(c, p)
	}
	writeJSON(w, http.StatusOK, p)
}

type orderRequest struct {
	CustomerID int64 `json:"customer_id"`
	ProductID  int64 `json:"product_id"`
	Quantity   int64 `json:"quantity"`
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID <= 0 || req.ProductID <= 0 || req.Quantity <= 0 || req.Quantity > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "customer_id, product_id and quantity (1-100) are required"})
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	release, err := s.Cache.Lock(ctx, fmt.Sprintf("checkout:%d", req.CustomerID), 10*time.Second)
	if errors.Is(err, cache.ErrLocked) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a checkout for this customer is already in progress"})
		return
	}
	if err != nil {
		s.fail(w, r, fmt.Errorf("checkout lock: %w", err))
		return
	}
	defer release()
	// Authorize an estimate first; the real total is computed from the
	// catalog price inside the order transaction.
	// Checkout keeps its state in a session file, as many web frameworks
	// do. Without it the order can't complete.
	if err := s.writeSession(req); err != nil {
		s.fail(w, r, fmt.Errorf("checkout session: %w", err))
		return
	}
	ref, err := s.Payments.Authorize(ctx, req.CustomerID, req.Quantity*100)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	o, err := s.Store.CreateOrder(ctx, store.NewOrder{CustomerID: req.CustomerID, ProductID: req.ProductID, Quantity: req.Quantity, PaymentRef: ref})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

// writeSession stores a checkout session file. The maintenance job removes
// old ones.
func (s *Server) writeSession(req orderRequest) error {
	if s.SessionDir == "" {
		return nil
	}
	b := make([]byte, 16)
	rand.Read(b)
	name := filepath.Join(s.SessionDir, "sess_"+hex.EncodeToString(b))
	data := fmt.Sprintf(`{"customer_id":%d,"product_id":%d,"quantity":%d,"created":%q}`,
		req.CustomerID, req.ProductID, req.Quantity, time.Now().UTC().Format(time.RFC3339))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(data); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	return f.Close()
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, store.ErrNotFound)
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	o, err := s.Store.GetOrder(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) customerOrders(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, store.ErrNotFound)
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	os, err := s.Store.CustomerOrders(ctx, id, 20)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, os)
}

// recentlyViewed keeps each customer's last few viewed products, for
// recommendations.
type recentlyViewed struct {
	mu    sync.Mutex
	items map[string][]store.Product
}

const (
	recentLimit     = 10
	recentCustomers = 10000
)

func (rv *recentlyViewed) add(customer string, p store.Product) {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	if rv.items == nil {
		rv.items = map[string][]store.Product{}
	}
	if _, ok := rv.items[customer]; !ok && len(rv.items) >= recentCustomers {
		for k := range rv.items { // evict an arbitrary customer
			delete(rv.items, k)
			break
		}
	}
	list := append(rv.items[customer], p)
	if len(list) > recentLimit && !faults.MemLeak {
		list = append([]store.Product(nil), list[len(list)-recentLimit:]...)
	}
	rv.items[customer] = list
}
