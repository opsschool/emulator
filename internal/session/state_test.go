package session

import (
	"errors"
	"testing"
	"time"

	"github.com/opsschool/simulator/internal/results"
)

func TestStateRoundTripAndResult(t *testing.T) {
	home := t.TempDir()
	if _, err := Load(home); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	start := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	st := &State{
		User: "jdoe", ScenarioID: "linux-disk-full", Level: 1, Seed: 9, StartedAt: start,
		TimeLimit: 45 * time.Minute, TargetTime: 20 * time.Minute, HintsUsed: 1,
		TierPassed: map[string]time.Time{results.TierMitigated: start.Add(3 * time.Minute), results.TierFixed: start.Add(9 * time.Minute)},
	}
	if err := Save(home, st); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil || got.User != "jdoe" || !got.Passed(results.TierFixed) {
		t.Fatalf("load: %+v %v", got, err)
	}
	r := got.Result(start.Add(10 * time.Minute))
	if r.TierPassed[results.TierMitigated] != 180 || r.Score != 100+100-10+50 {
		t.Errorf("result: %+v", r)
	}
	if got.OverTime(start.Add(44*time.Minute)) || !got.OverTime(start.Add(46*time.Minute)) {
		t.Error("OverTime wrong")
	}
	if err := Clear(home); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); !errors.Is(err, ErrNoSession) {
		t.Fatal("state not cleared")
	}
}
