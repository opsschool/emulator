// Package scenario defines the on-disk scenario format and loads, validates
// and templates scenarios. See docs/design.md, "Scenario spec".
package scenario

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// File names inside a scenario directory.
const (
	FileScenario  = "scenario.yaml"
	FileBreak     = "break.sh"
	FileChecks    = "checks.yaml"
	FileMitigate  = "mitigate.sh"
	FileSolve     = "solve.sh"
	FileQuestions = "questions.yaml"
	FileHints     = "hints.md"
	FileDashboard = "dashboard.json"
	FileSolution  = "SOLUTION.md"
)

// Categories lists the valid scenario categories, in display order.
var Categories = []string{"linux", "performance", "networking", "databases", "services", "distributed"}

// Images lists the valid VM images.
var Images = []string{"single-node", "multi-node"}

// LoadProfiles lists the built-in load profiles.
var LoadProfiles = []string{"steady", "peak"}

// Spec is the contents of scenario.yaml.
type Spec struct {
	// ID is "<category>/<level>.<n>" and Level is its level, both taken
	// from the directory, not the file.
	ID              string          `yaml:"-"`
	Level           int             `yaml:"-"`
	Category        string          `yaml:"category"`
	Image           string          `yaml:"image"`
	Curriculum      string          `yaml:"curriculum"`
	Alerts          []string        `yaml:"alerts"`
	Summary         string          `yaml:"summary"`
	Randomize       map[string]Var  `yaml:"randomize"`
	FixVerification FixVerification `yaml:"fix_verification"`
	MitigateHold    Duration        `yaml:"mitigate_hold"`
	TimeLimit       Duration        `yaml:"time_limit"`
	TargetTime      Duration        `yaml:"target_time"`
	Load            LoadSpec        `yaml:"load"`
}

// Var is one randomized variable. Exactly one of Choices or Range is set.
type Var struct {
	Choices []string `yaml:"choices"`
	Range   []int    `yaml:"range"`
}

// FixVerification describes what `opsschool verify` does before evaluating
// the fixed checks.
type FixVerification struct {
	Restart    []string `yaml:"restart"`
	Reboot     bool     `yaml:"reboot"`
	LoadReplay Duration `yaml:"load_replay"`
}

// LoadSpec selects the load generator profile. Profile is "steady" (the default),
// "peak", or empty when Schedule is set.
type LoadSpec struct {
	Profile  string     `yaml:"profile"`
	Schedule []RateStep `yaml:"schedule"`
	// NewConnections is the share of requests (0 to 1) sent on a fresh
	// connection instead of a reused one. 0, the default, reuses
	// connections wherever possible.
	NewConnections float64 `yaml:"new_connections"`
}

// RateStep is one step of a custom load schedule: RPS requests per second
// for Duration, repeated in order.
type RateStep struct {
	RPS      float64  `yaml:"rps"`
	Duration Duration `yaml:"duration"`
}

// Checks is the contents of checks.yaml.
type Checks struct {
	Mitigated []Check `yaml:"mitigated"`
	Fixed     []Check `yaml:"fixed"`
	// Preserve checks guard against collateral damage. If any fails during
	// fix verification, the fixed tier fails.
	Preserve []Check `yaml:"preserve"`
}

// Check types.
const (
	CheckHTTP   = "http"
	CheckPromQL = "promql"
	CheckScript = "script"
)

// Check is one grading check.
type Check struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	URL          string `yaml:"url"`
	ExpectStatus int    `yaml:"expect_status"`
	ExpectBody   string `yaml:"expect_body"`
	Expr         string `yaml:"expr"`
	Run          string `yaml:"run"`
}

// Label returns a short human-readable description of the check.
func (c Check) Label() string {
	if c.Name != "" {
		return c.Name
	}
	switch c.Type {
	case CheckHTTP:
		return "http " + c.URL
	case CheckPromQL:
		return "promql " + c.Expr
	case CheckScript:
		return "script " + c.Run
	}
	return c.Type
}

// Question types.
const (
	QuestionChoice = "choice"
	QuestionMulti  = "multi"
	QuestionText   = "text"
)

// Question is one optional quiz question from questions.yaml.
type Question struct {
	ID            string    `yaml:"id"`
	Prompt        string    `yaml:"prompt"`
	Type          string    `yaml:"type"`
	Choices       []string  `yaml:"choices"`
	Answer        yaml.Node `yaml:"answer"`
	AnswerFromVar string    `yaml:"answer_from_var"`
}

// Duration is a time.Duration that unmarshals from strings like "60s" or "45m".
type Duration struct{ time.Duration }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return fmt.Errorf("line %d: duration must be a string like \"60s\"", n.Line)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q", n.Line, s)
	}
	d.Duration = v
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }
