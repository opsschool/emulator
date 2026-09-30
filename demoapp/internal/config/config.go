// Package config reads the shop's settings from the environment. In the VM,
// systemd loads them from /etc/shop/shop.env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting. Field comments give the environment variable.
type Config struct {
	Listen      string // SHOP_LISTEN
	AdminListen string // SHOP_ADMIN_LISTEN: /metrics and pprof

	LogLevel string // SHOP_LOG_LEVEL: debug, info, warn, error
	LogFile  string // SHOP_LOG_FILE: empty logs to stdout only

	DBDSN          string        // SHOP_DB_DSN
	DBReplicaDSN   string        // SHOP_DB_REPLICA_DSN
	ReadReplica    bool          // SHOP_READ_FROM_REPLICA
	DBPoolSize     int           // SHOP_DB_POOL_SIZE: max open connections
	DBMaxIdle      int           // SHOP_DB_MAX_IDLE
	DBQueryTimeout time.Duration // SHOP_DB_QUERY_TIMEOUT

	RedisAddr string        // SHOP_REDIS_ADDR
	CacheTTL  time.Duration // SHOP_CACHE_TTL

	WorkerConcurrency int           // SHOP_WORKER_CONCURRENCY
	WorkerBufferMB    int           // SHOP_WORKER_BUFFER_MB: invoice render buffer per worker
	WorkerPoll        time.Duration // SHOP_WORKER_POLL

	PaymentsURL     string        // SHOP_PAYMENTS_URL
	ClientTimeout   time.Duration // SHOP_CLIENT_TIMEOUT: outbound HTTP timeout
	ClientKeepAlive bool          // SHOP_CLIENT_KEEPALIVE
	RetryMax        int           // SHOP_RETRY_MAX: attempts after the first
	RetryBackoff    time.Duration // SHOP_RETRY_BACKOFF: base delay, doubled per attempt

	SessionDir string // SHOP_SESSION_DIR: empty disables file sessions
}

// FromEnv reads the configuration, applying defaults.
func FromEnv() (Config, error) { return parse(os.Getenv) }

func parse(get func(string) string) (Config, error) {
	p := parser{get: get}
	c := Config{
		Listen:      p.str("SHOP_LISTEN", "127.0.0.1:8080"),
		AdminListen: p.str("SHOP_ADMIN_LISTEN", "0.0.0.0:9091"),

		LogLevel: p.str("SHOP_LOG_LEVEL", "info"),
		LogFile:  p.str("SHOP_LOG_FILE", ""),

		DBDSN:          p.str("SHOP_DB_DSN", "shop:shop@tcp(127.0.0.1:3306)/shop"),
		DBReplicaDSN:   p.str("SHOP_DB_REPLICA_DSN", ""),
		ReadReplica:    p.boolean("SHOP_READ_FROM_REPLICA", false),
		DBPoolSize:     p.integer("SHOP_DB_POOL_SIZE", 20),
		DBMaxIdle:      p.integer("SHOP_DB_MAX_IDLE", 10),
		DBQueryTimeout: p.duration("SHOP_DB_QUERY_TIMEOUT", 5*time.Second),

		RedisAddr: p.str("SHOP_REDIS_ADDR", "127.0.0.1:6379"),
		CacheTTL:  p.duration("SHOP_CACHE_TTL", 60*time.Second),

		WorkerConcurrency: p.integer("SHOP_WORKER_CONCURRENCY", 4),
		WorkerBufferMB:    p.integer("SHOP_WORKER_BUFFER_MB", 32),
		WorkerPoll:        p.duration("SHOP_WORKER_POLL", 500*time.Millisecond),

		PaymentsURL:     p.str("SHOP_PAYMENTS_URL", "http://127.0.0.1:8081"),
		ClientTimeout:   p.duration("SHOP_CLIENT_TIMEOUT", 2*time.Second),
		ClientKeepAlive: p.boolean("SHOP_CLIENT_KEEPALIVE", true),
		RetryMax:        p.integer("SHOP_RETRY_MAX", 2),
		RetryBackoff:    p.duration("SHOP_RETRY_BACKOFF", 100*time.Millisecond),

		SessionDir: p.str("SHOP_SESSION_DIR", ""),
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		p.errs = append(p.errs, fmt.Sprintf("SHOP_LOG_LEVEL: unknown level %q", c.LogLevel))
	}
	if c.DBPoolSize < 1 {
		p.errs = append(p.errs, "SHOP_DB_POOL_SIZE must be at least 1")
	}
	if c.WorkerConcurrency < 0 {
		p.errs = append(p.errs, "SHOP_WORKER_CONCURRENCY must not be negative")
	}
	if len(p.errs) > 0 {
		return c, fmt.Errorf("invalid configuration: %s", strings.Join(p.errs, "; "))
	}
	return c, nil
}

type parser struct {
	get  func(string) string
	errs []string
}

func (p *parser) str(k, def string) string {
	if v, ok := p.lookup(k); ok {
		return v
	}
	return def
}

func (p *parser) lookup(k string) (string, bool) {
	v := strings.TrimSpace(p.get(k))
	return v, v != ""
}

func (p *parser) integer(k string, def int) int {
	v, ok := p.lookup(k)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Sprintf("%s: %q is not a number", k, v))
		return def
	}
	return n
}

func (p *parser) boolean(k string, def bool) bool {
	v, ok := p.lookup(k)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Sprintf("%s: %q is not true or false", k, v))
		return def
	}
	return b
}

func (p *parser) duration(k string, def time.Duration) time.Duration {
	v, ok := p.lookup(k)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Sprintf("%s: %q is not a duration like 5s", k, v))
		return def
	}
	return d
}
