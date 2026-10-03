// Package session runs a learner's scenario session: setup, the background
// daemon that drives load and grades the mitigated tier, fix verification,
// and teardown.
package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/opsschool/emulator/internal/results"
)

// State is the persisted state of the running session.
type State struct {
	User        string            `json:"user"`
	ScenarioID  string            `json:"scenario_id"`
	ScenarioDir string            `json:"scenario_dir"`
	Level       int               `json:"level"`
	Driver      string            `json:"driver"`
	Image       string            `json:"image"`
	Seed        uint64            `json:"seed"`
	Vars        map[string]string `json:"vars"`
	// StartedAt is when the scenario began: the break is applied and the
	// clock runs. Zero during the healthy baseline before it.
	StartedAt  time.Time     `json:"started_at"`
	TimeLimit  time.Duration `json:"time_limit"`
	TargetTime time.Duration `json:"target_time"`
	// TierPassed maps a tier to when it passed.
	TierPassed map[string]time.Time `json:"tier_passed"`
	HintsUsed  int                  `json:"hints_used"`
	Quiz       *results.QuizResult  `json:"quiz,omitempty"`
	DataLoss   bool                 `json:"data_loss"`
	LastVerify *VerifyReport        `json:"last_verify,omitempty"`
	Events     []Event              `json:"events"`
	DaemonPID  int                  `json:"daemon_pid"`
}

// Event is something that happened during the session, shown by status.
type Event struct {
	At  time.Time `json:"at"`
	Msg string    `json:"msg"`
}

// Begun reports whether the scenario has begun.
func (s *State) Begun() bool { return !s.StartedAt.IsZero() }

// Elapsed returns the time since the scenario began.
func (s *State) Elapsed(now time.Time) time.Duration {
	if !s.Begun() {
		return 0
	}
	return now.Sub(s.StartedAt).Truncate(time.Second)
}

// Passed reports whether a tier has passed.
func (s *State) Passed(tier string) bool { _, ok := s.TierPassed[tier]; return ok }

// OverTime reports whether the time limit has run out.
func (s *State) OverTime(now time.Time) bool {
	return s.Begun() && s.TimeLimit > 0 && now.Sub(s.StartedAt) > s.TimeLimit
}

// Result converts the state into a result record.
func (s *State) Result(now time.Time) results.Result {
	r := results.Result{
		User: s.User, Scenario: s.ScenarioID, Level: s.Level, Seed: s.Seed,
		StartedAt: s.StartedAt, EndedAt: now, TierPassed: map[string]results.Seconds{},
		HintsUsed: s.HintsUsed, DataLoss: s.DataLoss, Quiz: s.Quiz,
		TargetTime: results.SecondsOf(s.TargetTime),
	}
	for t, at := range s.TierPassed {
		r.TierPassed[t] = results.SecondsOf(at.Sub(s.StartedAt))
	}
	r.Score = results.ComputeScore(r)
	return r
}

// Dir returns the session directory inside the opsschool home.
func Dir(home string) string { return filepath.Join(home, "session") }

func statePath(home string) string { return filepath.Join(Dir(home), "state.json") }

// ErrNoSession means no session is running.
var ErrNoSession = errors.New("no scenario is running; start one with `opsschool start <category>/<level>.<n> --user <name>` (see `opsschool list`)")

// Load reads the session state.
func Load(home string) (*State, error) {
	b, err := os.ReadFile(statePath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the session state atomically.
func Save(home string, s *State) error {
	if err := os.MkdirAll(Dir(home), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(home) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(home))
}

// Clear removes the session state.
func Clear(home string) error {
	err := os.Remove(statePath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
