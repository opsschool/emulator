package store

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Integration test. Set SHOP_TEST_DSN to a database the test may wipe, e.g.
// SHOP_TEST_DSN='root:root@tcp(127.0.0.1:3306)/shop_test'.
func testStore(t *testing.T) *Store {
	dsn := os.Getenv("SHOP_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOP_TEST_DSN not set")
	}
	db, err := Open(dsn, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS products, customers, orders, order_queue, daily_sales`); err != nil {
		t.Fatal(err)
	}
	s := &Store{DB: db}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(ctx, SeedSizes{Products: 20, Customers: 10, Orders: 100}, t.Logf); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOrderLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p, err := s.GetProduct(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, NewOrder{CustomerID: 4, ProductID: 3, Quantity: 2, PaymentRef: "pay_x"})
	if err != nil || o.TotalCents != 2*p.PriceCents {
		t.Fatalf("create: %+v %v", o, err)
	}
	if _, err := s.CreateOrder(ctx, NewOrder{CustomerID: 4, ProductID: 999, Quantity: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown product: %v", err)
	}
	if d, _ := s.QueueDepth(ctx); d != 1 {
		t.Fatalf("queue depth %d", d)
	}
	n, err := s.ClaimJobs(ctx, 10, func(Job) error { return nil })
	if err != nil || n != 1 {
		t.Fatalf("claim: %d %v", n, err)
	}
	got, err := s.GetOrder(ctx, o.ID)
	if err != nil || got.Status != "processed" {
		t.Fatalf("get: %+v %v", got, err)
	}
	recent, err := s.CustomerOrders(ctx, 4, 5)
	if err != nil || len(recent) == 0 || recent[0].ID != o.ID {
		t.Fatalf("customer orders: %+v %v", recent, err)
	}
	if err := s.DailyReport(ctx); err != nil {
		t.Fatal(err)
	}
	if stats := s.DB.Stats(); stats.InUse != 0 {
		t.Errorf("%d connections still in use", stats.InUse)
	}
}
