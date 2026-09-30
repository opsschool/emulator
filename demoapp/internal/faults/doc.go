// Package faults switches on code faults for alternate builds of the shop.
// Each fault is a build tag; the good build uses none. Faulty builds behave
// exactly like the good build except for the fault. See docs/design.md,
// "How faults are injected".
//
//	faultconnleak   an error path in POST /orders leaks its transaction,
//	                holding a database connection forever
//	faultmemleak    the recently-viewed feature never trims its history
//	faultidletx     the worker's reconcile loop leaves a transaction open on
//	                orders when nothing is stuck
//	faultnobackoff  retries ignore SHOP_RETRY_BACKOFF
package faults
