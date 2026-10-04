package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// SeedSizes controls how much data Seed writes.
type SeedSizes struct {
	Products  int
	Customers int
	Orders    int
}

var (
	// Uncle Wally's Peanut Emporium sells peanuts and little else.
	adjectives = []string{"Salted", "Unsalted", "Honey-Roasted", "Dry-Roasted", "Smoked", "Cajun", "Chili-Lime", "Garlic", "Maple", "Chocolate-Covered", "Toffee", "Organic", "Jumbo", "Blanched", "Wally's Own"}
	nouns      = []string{"Peanuts", "Peanuts in the Shell", "Virginia Peanuts", "Valencia Peanuts", "Spanish Peanuts", "Peanut Butter", "Crunchy Peanut Butter", "Peanut Brittle", "Peanut Clusters", "Peanut Butter Cups", "Peanut Butter Fudge", "Peanut Cookies", "Peanut Snack Bars", "Peanut Sauce", "Peanut Oil", "Peanut Flour", "Trail Mix", "Peanut Sampler", "Peanut Gift Tin", "Party Bucket of Peanuts"}
	firsts     = []string{"Ada", "Ben", "Chen", "Dana", "Eli", "Fatima", "Gus", "Hana", "Ivan", "Jo", "Kai", "Lena", "Mo", "Nia", "Omar", "Priya", "Quinn", "Rosa", "Sam", "Tariq"}
	lasts      = []string{"Ahmed", "Brown", "Costa", "Diaz", "Evans", "Fischer", "Garcia", "Hughes", "Ito", "Jones", "Khan", "Lopez", "Muller", "Nguyen", "Okafor", "Patel", "Rossi", "Silva", "Tanaka", "Wong"}
)

// Seed fills an empty database with a catalog, customers and order history.
// It is deterministic, and progress is reported through logf.
func (s *Store) Seed(ctx context.Context, sz SeedSizes, logf func(string, ...any)) error {
	r := rand.New(rand.NewPCG(1, 2))
	if err := s.bulk(ctx, "products (sku, name, description, price_cents)", sz.Products, func(i int) []any {
		name := adjectives[r.IntN(len(adjectives))] + " " + nouns[r.IntN(len(nouns))]
		return []any{fmt.Sprintf("SKU-%06d", i+1), name,
			name + ", roasted in small batches and packed the day it ships.",
			499 + r.IntN(25000)}
	}, logf); err != nil {
		return err
	}
	if err := s.bulk(ctx, "customers (email, name)", sz.Customers, func(i int) []any {
		return []any{fmt.Sprintf("customer%d@example.com", i+1),
			firsts[r.IntN(len(firsts))] + " " + lasts[r.IntN(len(lasts))]}
	}, logf); err != nil {
		return err
	}
	start := time.Now().UTC().AddDate(-2, 0, 0)
	span := time.Since(start)
	return s.bulk(ctx, "orders (customer_id, product_id, quantity, total_cents, status, payment_ref, created_at)", sz.Orders, func(i int) []any {
		// Orders arrive in time order, like real history.
		at := start.Add(time.Duration(float64(span) * float64(i) / float64(sz.Orders)))
		q := 1 + r.IntN(3)
		return []any{1 + r.IntN(sz.Customers), 1 + r.IntN(sz.Products), q, q * (499 + r.IntN(25000)),
			"processed", fmt.Sprintf("pay_%016x", r.Uint64()), at}
	}, logf)
}

const seedBatch = 2000

func (s *Store) bulk(ctx context.Context, table string, n int, row func(int) []any, logf func(string, ...any)) error {
	if n == 0 {
		return nil
	}
	cols := strings.Count(table, ",") + 1
	one := "(" + strings.TrimSuffix(strings.Repeat("?,", cols), ",") + ")"
	for done := 0; done < n; {
		size := min(seedBatch, n-done)
		args := make([]any, 0, size*cols)
		for i := 0; i < size; i++ {
			args = append(args, row(done+i)...)
		}
		q := "INSERT INTO " + table + " VALUES " + strings.TrimSuffix(strings.Repeat(one+",", size), ",")
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("seed %s: %w", table, err)
		}
		done += size
		if done%(seedBatch*100) == 0 || done == n {
			logf("seeded %d/%d rows into %s", done, n, strings.Fields(table)[0])
		}
	}
	return nil
}
