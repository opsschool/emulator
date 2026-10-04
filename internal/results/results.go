// Package results stores scenario results in ~/.opsschool/results.jsonl and
// scores them.
package results

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Tiers, in the order a learner passes them.
const (
	TierMitigated = "mitigated"
	TierFixed     = "fixed"
)

// Tiers lists every tier in order.
var Tiers = []string{TierMitigated, TierFixed}

// FileName is the results file inside the opsschool home directory.
const FileName = "results.jsonl"

// Result is one finished (or abandoned) scenario session.
type Result struct {
	User      string    `json:"user"`
	Scenario  string    `json:"scenario"`
	Level     int       `json:"level"`
	Seed      uint64    `json:"seed"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	// TierPassed maps a tier to how long after the start it passed.
	TierPassed map[string]Seconds `json:"tier_passed"`
	// DocsHint is true when the learner asked for the free curriculum hint.
	DocsHint  bool `json:"docs_hint,omitempty"`
	HintsUsed int  `json:"hints_used"`
	// DataLoss is true when a preserve check failed during fix verification.
	DataLoss   bool        `json:"data_loss"`
	Quiz       *QuizResult `json:"quiz,omitempty"`
	TargetTime Seconds     `json:"target_time,omitempty"`
	Score      int         `json:"score"`
}

// QuizResult records the optional quiz. It does not affect the score.
type QuizResult struct {
	Correct int `json:"correct"`
	Total   int `json:"total"`
}

// Seconds is a duration stored as whole seconds.
type Seconds int64

// Duration converts to time.Duration.
func (s Seconds) Duration() time.Duration { return time.Duration(s) * time.Second }

// SecondsOf converts from time.Duration.
func SecondsOf(d time.Duration) Seconds { return Seconds(d / time.Second) }

// Passed reports whether the tier passed.
func (r Result) Passed(tier string) bool { _, ok := r.TierPassed[tier]; return ok }

// Scoring constants. Keep the formula here so it can change in one place.
const (
	PointsPerTier = 100
	HintPenalty   = 10
	TimeBonus     = 50
)

// ComputeScore scores a result: PointsPerTier per tier passed, minus
// HintPenalty per hint, plus TimeBonus when the fixed tier passed within the
// scenario's target time. Never negative.
func ComputeScore(r Result) int {
	score := 0
	for _, t := range Tiers {
		if r.Passed(t) {
			score += PointsPerTier
		}
	}
	score -= HintPenalty * r.HintsUsed
	if at, ok := r.TierPassed[TierFixed]; ok && r.TargetTime > 0 && at <= r.TargetTime {
		score += TimeBonus
	}
	return max(score, 0)
}

// Store is an append-only results file.
type Store struct{ Path string }

// Open returns the store in the given opsschool home directory.
func Open(home string) *Store { return &Store{Path: filepath.Join(home, FileName)} }

// Append writes one result.
func (s *Store) Append(r Result) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// All reads every result. A missing file means no results.
func (s *Store) All() ([]Result, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Result
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var r Result
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", s.Path, n, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// Best returns each scenario's highest-scoring result for a user. An empty
// user matches every user.
func Best(rs []Result, user string) map[string]Result {
	out := map[string]Result{}
	for _, r := range rs {
		if user != "" && r.User != user {
			continue
		}
		if b, ok := out[r.Scenario]; !ok || r.Score > b.Score {
			out[r.Scenario] = r
		}
	}
	return out
}

// Fastest returns each scenario's fastest result for a user: the run with the
// quickest fix, so its mitigation time is from that same run, not the
// quickest mitigation of any run. Equal fixes go to the earlier mitigation.
// When no run fixed a scenario, the quickest mitigation stands in. An empty
// user matches every user.
func Fastest(rs []Result, user string) map[string]Result {
	out := map[string]Result{}
	for _, r := range rs {
		if user != "" && r.User != user || !r.Passed(TierMitigated) && !r.Passed(TierFixed) {
			continue
		}
		if b, ok := out[r.Scenario]; !ok || faster(r, b) {
			out[r.Scenario] = r
		}
	}
	return out
}

// faster compares fix times first, then mitigation times. A run that never
// passed a tier is slower than any run that did.
func faster(a, b Result) bool {
	for _, t := range []string{TierFixed, TierMitigated} {
		at, aok := a.TierPassed[t]
		bt, bok := b.TierPassed[t]
		switch {
		case aok && !bok:
			return true
		case !aok && bok:
			return false
		case aok && at != bt:
			return at < bt
		}
	}
	return false
}
