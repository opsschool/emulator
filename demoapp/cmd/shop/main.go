// Command shop is the demo service every scenario runs against: a small
// online store with an HTTP API, an order worker and a maintenance job.
//
//	shop serve        HTTP API on SHOP_LISTEN, metrics and pprof on SHOP_ADMIN_LISTEN
//	shop worker       process queued orders
//	shop maintenance  nightly report and cleanup (run from cron)
//	shop payments     the stand-in payments service
//	shop migrate      create the schema
//	shop seed         fill an empty database with catalog and order history
//	shop check-config validate the configuration in the environment and exit
//	shop version      print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/opsschool/emulator/demoapp/internal/api"
	"github.com/opsschool/emulator/demoapp/internal/cache"
	"github.com/opsschool/emulator/demoapp/internal/config"
	"github.com/opsschool/emulator/demoapp/internal/logging"
	"github.com/opsschool/emulator/demoapp/internal/metrics"
	"github.com/opsschool/emulator/demoapp/internal/payments"
	"github.com/opsschool/emulator/demoapp/internal/store"
	"github.com/opsschool/emulator/demoapp/internal/worker"
)

// Version is set at build time with -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: shop serve|worker|maintenance|payments|migrate|seed|check-config|version")
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "version" {
		fmt.Println(Version)
		return
	}
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shop:", err)
		os.Exit(1)
	}
	if cmd == "check-config" {
		fmt.Println("configuration OK")
		return
	}
	log, err := logging.New(cfg.LogLevel, cfg.LogFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shop: logging:", err)
		os.Exit(1)
	}
	log = log.With("component", cmd, "version", Version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		err = serve(ctx, cfg, log)
	case "worker":
		err = runWorker(ctx, cfg, log)
	case "maintenance":
		err = maintenance(ctx, cfg, log)
	case "payments":
		err = listen(ctx, log, "payments", paymentsAddr(args), payments.Handler())
	case "migrate":
		err = withStore(cfg, func(s *store.Store) error { return s.Migrate(ctx) })
	case "seed":
		err = seed(ctx, cfg, log, args)
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func openStore(cfg config.Config) (*store.Store, error) {
	db, err := store.Open(cfg.DBDSN, cfg.DBPoolSize, cfg.DBMaxIdle)
	if err != nil {
		return nil, err
	}
	s := &store.Store{DB: db, ReadReplica: cfg.ReadReplica}
	if cfg.DBReplicaDSN != "" {
		if s.Replica, err = store.Open(cfg.DBReplicaDSN, cfg.DBPoolSize, cfg.DBMaxIdle); err != nil {
			return nil, err
		}
	}
	metrics.Registry.MustRegister(dbStats(db, "source"))
	if s.Replica != nil {
		metrics.Registry.MustRegister(dbStats(s.Replica, "replica"))
	}
	return s, nil
}

func withStore(cfg config.Config, f func(*store.Store) error) error {
	s, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer s.DB.Close()
	return f(s)
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	s, err := openStore(cfg)
	if err != nil {
		return err
	}
	metrics.BuildInfo.WithLabelValues(Version).Set(1)
	srv := &api.Server{
		Store:        s,
		Cache:        cache.New(cfg.RedisAddr, cfg.CacheTTL, log),
		Payments:     payments.NewClient(cfg.PaymentsURL, cfg.ClientTimeout, cfg.ClientKeepAlive, cfg.RetryMax, cfg.RetryBackoff),
		Log:          log,
		Version:      Version,
		QueryTimeout: cfg.DBQueryTimeout,
		SessionDir:   cfg.SessionDir,
	}
	if cfg.SessionDir != "" {
		if err := os.MkdirAll(cfg.SessionDir, 0o700); err != nil {
			return err
		}
	}
	go func() {
		if err := listen(ctx, log, "admin", cfg.AdminListen, api.AdminHandler()); err != nil {
			log.Error("admin server failed", "err", err)
		}
	}()
	log.Info("starting", "listen", cfg.Listen, "admin", cfg.AdminListen, "log_level", cfg.LogLevel,
		"db_pool_size", cfg.DBPoolSize, "read_from_replica", cfg.ReadReplica, "cache_ttl", cfg.CacheTTL.String())
	return listen(ctx, log, "api", cfg.Listen, srv.Handler())
}

func runWorker(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	s, err := openStore(cfg)
	if err != nil {
		return err
	}
	metrics.BuildInfo.WithLabelValues(Version).Set(1)
	go func() {
		if err := listen(ctx, log, "admin", workerAdminAddr(cfg.AdminListen), api.AdminHandler()); err != nil {
			log.Error("admin server failed", "err", err)
		}
	}()
	w := &worker.Worker{Store: s, Log: log, Concurrency: cfg.WorkerConcurrency, BufferMB: cfg.WorkerBufferMB, Poll: cfg.WorkerPoll, Batch: 10}
	w.Run(ctx)
	return nil
}

// workerAdminAddr puts the worker's admin server on the port after the API's.
func workerAdminAddr(api string) string {
	host, port, ok := strings.Cut(api, ":")
	if !ok {
		return api
	}
	var p int
	fmt.Sscan(port, &p)
	return fmt.Sprintf("%s:%d", host, p+1)
}

func maintenance(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	return withStore(cfg, func(s *store.Store) error {
		start := time.Now()
		if err := s.DailyReport(ctx); err != nil {
			return fmt.Errorf("daily report: %w", err)
		}
		removed := cleanSessions(cfg.SessionDir, time.Hour, log)
		log.Info("maintenance done", "duration_ms", time.Since(start).Milliseconds(), "sessions_removed", removed)
		return nil
	})
}

func cleanSessions(dir string, maxAge time.Duration, log *slog.Logger) int {
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn("session cleanup failed", "err", err)
		return 0
	}
	n := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "sess_") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				n++
			}
		}
	}
	return n
}

func seed(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	var sz store.SeedSizes
	fs.IntVar(&sz.Products, "products", 5000, "catalog size")
	fs.IntVar(&sz.Customers, "customers", 200000, "customer count")
	fs.IntVar(&sz.Orders, "orders", 3000000, "order history size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withStore(cfg, func(s *store.Store) error {
		if err := s.Migrate(ctx); err != nil {
			return err
		}
		return s.Seed(ctx, sz, func(f string, a ...any) { log.Info(fmt.Sprintf(f, a...)) })
	})
}

func paymentsAddr(args []string) string {
	fs := flag.NewFlagSet("payments", flag.ExitOnError)
	addr := fs.String("listen", "127.0.0.1:8081", "listen address")
	fs.Parse(args)
	return *addr
}

func listen(ctx context.Context, log *slog.Logger, name, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Info("listening", "server", name, "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
