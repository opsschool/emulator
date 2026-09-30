// Package metrics defines the shop's Prometheus metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry holds every shop metric plus Go runtime and process metrics.
var Registry = prometheus.NewRegistry()

var (
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP requests by route and status code.",
	}, []string{"route", "code"})

	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by route.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"route"})

	CacheRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shop_cache_requests_total",
		Help: "Cache lookups by result: hit, miss or error.",
	}, []string{"result"})

	PaymentRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shop_payment_requests_total",
		Help: "Calls to the payments service by result, including retries.",
	}, []string{"result"})

	QueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "shop_worker_queue_depth",
		Help: "Orders waiting for the worker.",
	})

	OrdersProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "shop_worker_orders_processed_total",
		Help: "Orders the worker has processed.",
	})

	WorkerErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "shop_worker_errors_total",
		Help: "Worker batches that failed.",
	})

	BuildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "shop_build_info",
		Help: "Always 1; labels give the running version.",
	}, []string{"version"})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		HTTPRequests, HTTPDuration, CacheRequests, PaymentRequests,
		QueueDepth, OrdersProcessed, WorkerErrors, BuildInfo,
	)
}
