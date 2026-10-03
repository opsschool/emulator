// Package store is the shop's MySQL access layer.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/opsschool/emulator/demoapp/internal/faults"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Product is a catalog entry.
type Product struct {
	ID          int64  `json:"id"`
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
}

// Order is a customer order.
type Order struct {
	ID         int64     `json:"id"`
	CustomerID int64     `json:"customer_id"`
	ProductID  int64     `json:"product_id"`
	Quantity   int64     `json:"quantity"`
	TotalCents int64     `json:"total_cents"`
	Status     string    `json:"status"`
	PaymentRef string    `json:"payment_ref"`
	CreatedAt  time.Time `json:"created_at"`
}

// Store wraps the source database and an optional replica.
type Store struct {
	DB          *sql.DB
	Replica     *sql.DB // nil when no replica is configured
	ReadReplica bool
}

// Open connects to MySQL. The DSN is a go-sql-driver/mysql DSN.
func Open(dsn string, poolSize, maxIdle int) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(poolSize)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// Migrate creates the schema if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

// Ping checks the source database.
func (s *Store) Ping(ctx context.Context) error { return s.DB.PingContext(ctx) }

func (s *Store) reader() *sql.DB {
	if s.ReadReplica && s.Replica != nil {
		return s.Replica
	}
	return s.DB
}

// PageSize is the number of products per catalog page.
const PageSize = 50

// ListProducts returns one page of the catalog, starting at page 1.
func (s *Store) ListProducts(ctx context.Context, page int) ([]Product, error) {
	rows, err := s.reader().QueryContext(ctx,
		`SELECT id, sku, name, description, price_cents FROM products ORDER BY id LIMIT ? OFFSET ?`,
		PageSize, (page-1)*PageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Name, &p.Description, &p.PriceCents); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProduct returns one product.
func (s *Store) GetProduct(ctx context.Context, id int64) (Product, error) {
	var p Product
	err := s.reader().QueryRowContext(ctx,
		`SELECT id, sku, name, description, price_cents FROM products WHERE id = ?`, id).
		Scan(&p.ID, &p.SKU, &p.Name, &p.Description, &p.PriceCents)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// NewOrder is the input to CreateOrder.
type NewOrder struct {
	CustomerID int64
	ProductID  int64
	Quantity   int64
	PaymentRef string
}

// CreateOrder records an order and queues it for the worker.
func (s *Store) CreateOrder(ctx context.Context, in NewOrder) (Order, error) {
	var tx *sql.Tx
	var err error
	if faults.ConnLeak {
		tx, err = s.DB.Begin()
	} else {
		tx, err = s.DB.BeginTx(ctx, nil)
	}
	if err != nil {
		return Order{}, err
	}
	var price int64
	err = tx.QueryRowContext(ctx, `SELECT price_cents FROM products WHERE id = ? FOR SHARE`, in.ProductID).Scan(&price)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		if !faults.ConnLeak {
			tx.Rollback()
		}
		return Order{}, err
	}
	o := Order{CustomerID: in.CustomerID, ProductID: in.ProductID, Quantity: in.Quantity,
		TotalCents: price * in.Quantity, Status: "pending", PaymentRef: in.PaymentRef, CreatedAt: time.Now().UTC()}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO orders (customer_id, product_id, quantity, total_cents, status, payment_ref, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		o.CustomerID, o.ProductID, o.Quantity, o.TotalCents, o.Status, o.PaymentRef, o.CreatedAt)
	if err != nil {
		tx.Rollback()
		return Order{}, err
	}
	if o.ID, err = res.LastInsertId(); err != nil {
		tx.Rollback()
		return Order{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO order_queue (order_id) VALUES (?)`, o.ID); err != nil {
		tx.Rollback()
		return Order{}, err
	}
	return o, tx.Commit()
}

const orderCols = `id, customer_id, product_id, quantity, total_cents, status, payment_ref, created_at`

func scanOrder(sc interface{ Scan(...any) error }) (Order, error) {
	var o Order
	err := sc.Scan(&o.ID, &o.CustomerID, &o.ProductID, &o.Quantity, &o.TotalCents, &o.Status, &o.PaymentRef, &o.CreatedAt)
	return o, err
}

// GetOrder returns one order.
func (s *Store) GetOrder(ctx context.Context, id int64) (Order, error) {
	o, err := scanOrder(s.reader().QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

// CustomerOrders returns a customer's most recent orders.
func (s *Store) CustomerOrders(ctx context.Context, customerID int64, limit int) ([]Order, error) {
	rows, err := s.reader().QueryContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE customer_id = ? ORDER BY created_at DESC LIMIT ?`, customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Job is a queued order.
type Job struct {
	QueueID int64
	OrderID int64
}

// ClaimJobs takes up to n queued orders, runs process on each inside the
// claiming transaction, marks them processed and removes them from the queue.
func (s *Store) ClaimJobs(ctx context.Context, n int, process func(Job) error) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT id, order_id FROM order_queue ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED`, n)
	if err != nil {
		return 0, err
	}
	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.QueueID, &j.OrderID); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if len(jobs) == 0 {
		return 0, tx.Commit()
	}
	ids := make([]any, 0, len(jobs))
	orderIDs := make([]any, 0, len(jobs))
	for _, j := range jobs {
		if err := process(j); err != nil {
			return 0, err
		}
		ids = append(ids, j.QueueID)
		orderIDs = append(orderIDs, j.OrderID)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(jobs)), ",")
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'processed' WHERE id IN (`+ph+`)`, orderIDs...); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM order_queue WHERE id IN (`+ph+`)`, ids...); err != nil {
		return 0, err
	}
	return len(jobs), tx.Commit()
}

// QueueDepth returns the number of queued orders.
func (s *Store) QueueDepth(ctx context.Context) (int64, error) {
	var n int64
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_queue`).Scan(&n)
	return n, err
}

// Reconcile counts orders stuck in pending for more than five minutes.
func (s *Store) Reconcile(ctx context.Context) (int64, error) {
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	var n int64
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE status = 'pending' AND created_at < NOW(3) - INTERVAL 5 MINUTE`).Scan(&n)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	if n == 0 && faults.IdleTx {
		return 0, nil
	}
	return n, tx.Commit()
}

// DailyReport recomputes sales totals for today and yesterday.
func (s *Store) DailyReport(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO daily_sales (day, orders, revenue_cents, computed_at)
SELECT DATE(created_at), COUNT(*), SUM(total_cents), NOW(3)
FROM orders
WHERE created_at >= CURDATE() - INTERVAL 1 DAY
GROUP BY DATE(created_at)
ON DUPLICATE KEY UPDATE orders = VALUES(orders), revenue_cents = VALUES(revenue_cents), computed_at = VALUES(computed_at)`)
	return err
}
