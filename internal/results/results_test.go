package results

import (
	"testing"
	"time"
)

func TestComputeScore(t *testing.T) {
	cases := []struct {
		name string
		r    Result
		want int
	}{
		{"nothing", Result{}, 0},
		{"mitigated", Result{TierPassed: map[string]Seconds{TierMitigated: 60}}, 100},
		{"both with hints", Result{TierPassed: map[string]Seconds{TierMitigated: 60, TierFixed: 600}, HintsUsed: 2}, 180},
		{"time bonus", Result{TierPassed: map[string]Seconds{TierMitigated: 60, TierFixed: 600}, TargetTime: 1200}, 250},
		{"too slow for bonus", Result{TierPassed: map[string]Seconds{TierMitigated: 60, TierFixed: 1300}, TargetTime: 1200}, 200},
		{"never negative", Result{HintsUsed: 5}, 0},
	}
	for _, c := range cases {
		if got := ComputeScore(c.r); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := Open(t.TempDir())
	if rs, err := s.All(); err != nil || rs != nil {
		t.Fatalf("empty store: %v %v", rs, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for i, score := range []int{100, 250, 180} {
		r := Result{User: "jdoe", Scenario: "linux-disk-full", Seed: uint64(i), StartedAt: now, Score: score}
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	s.Append(Result{User: "other", Scenario: "linux-disk-full", Score: 300})
	rs, err := s.All()
	if err != nil || len(rs) != 4 {
		t.Fatalf("got %d results, %v", len(rs), err)
	}
	if b := Best(rs, "jdoe")["linux-disk-full"]; b.Score != 250 || !b.StartedAt.Equal(now) {
		t.Errorf("best for jdoe: %+v", b)
	}
	if b := Best(rs, "")["linux-disk-full"]; b.Score != 300 {
		t.Errorf("best overall: %+v", b)
	}
}
