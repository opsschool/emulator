package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario is a loaded scenario directory.
type Scenario struct {
	Dir       string
	Spec      Spec
	Checks    Checks
	Questions []Question // nil when the scenario has no quiz
	Hints     []string
	Dashboard []byte // nil when the scenario has no extra panels
}

// Path returns the path of a file inside the scenario directory.
func (s *Scenario) Path(name string) string { return filepath.Join(s.Dir, name) }

// HasQuiz reports whether the scenario has quiz questions.
func (s *Scenario) HasQuiz() bool { return len(s.Questions) > 0 }

// Load reads a scenario directory. It returns an error for the first file
// that is missing or cannot be parsed; use Validate for a full report.
func Load(dir string) (*Scenario, error) {
	s, problems := load(dir)
	for _, p := range problems {
		if p.Severity == SevError {
			return nil, errors.New(p.String())
		}
	}
	return s, nil
}

// load parses everything it can and reports what it could not.
func load(dir string) (*Scenario, []Problem) {
	s := &Scenario{Dir: dir}
	var ps []Problem
	add := func(file, format string, args ...any) {
		ps = append(ps, Problem{File: filepath.Join(dir, file), Severity: SevError, Msg: fmt.Sprintf(format, args...)})
	}

	if err := decodeFile(filepath.Join(dir, FileScenario), &s.Spec, true); err != nil {
		add(FileScenario, "%s", err)
	}
	s.Spec.ID = IDOf(dir)
	s.Spec.Level, _ = levelAndNumber(s.Spec.ID)
	if s.Spec.MitigateHold.Duration == 0 {
		s.Spec.MitigateHold.Duration = DefaultMitigateHoldSeconds * time.Second
	}
	if s.Spec.TimeLimit.Duration == 0 {
		s.Spec.TimeLimit.Duration = DefaultTimeLimitMinutes * time.Minute
	}
	if err := decodeFile(filepath.Join(dir, FileChecks), &s.Checks, true); err != nil {
		add(FileChecks, "%s", err)
	}
	if err := decodeFile(filepath.Join(dir, FileQuestions), &s.Questions, false); err != nil {
		add(FileQuestions, "%s", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, FileHints)); err == nil {
		s.Hints = SplitHints(string(b))
	}
	if b, err := os.ReadFile(filepath.Join(dir, FileDashboard)); err == nil {
		s.Dashboard = b
	}
	return s, ps
}

// decodeFile strictly decodes a YAML file. A missing file is an error only
// when required is true.
func decodeFile(path string, v any, required bool) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if required {
			return errors.New("file is missing")
		}
		return nil
	}
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("file is empty")
		}
		return cleanYAMLError(err)
	}
	return nil
}

var yamlErrPrefix = regexp.MustCompile(`^yaml: (unmarshal errors:\n\s*)?`)

func cleanYAMLError(err error) error {
	msg := yamlErrPrefix.ReplaceAllString(err.Error(), "")
	msg = strings.ReplaceAll(msg, "\n  ", "; ")
	msg = strings.ReplaceAll(msg, "in type scenario.", "in ")
	return errors.New(msg)
}

var hintSep = regexp.MustCompile(`(?m)^---\s*$`)

// SplitHints splits hints.md into ordered hints separated by "---" lines.
func SplitHints(s string) []string {
	var out []string
	for _, h := range hintSep.Split(s, -1) {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// FindDirs returns every scenario directory under root (any directory that
// contains scenario.yaml), sorted by path.
func FindDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == FileScenario {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs, err
}

// LoadAll loads every scenario under root, sorted by category order, level
// and number. Scenarios that fail to load are returned as errors.
func LoadAll(root string) ([]*Scenario, []error) {
	dirs, err := FindDirs(root)
	if err != nil {
		return nil, []error{err}
	}
	var out []*Scenario
	var errs []error
	for _, d := range dirs {
		s, err := Load(d)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Spec, out[j].Spec
		if ca, cb := categoryIndex(a.Category), categoryIndex(b.Category); ca != cb {
			return ca < cb
		}
		la, na := levelAndNumber(a.ID)
		lb, nb := levelAndNumber(b.ID)
		if la != lb {
			return la < lb
		}
		if na != nb {
			return na < nb
		}
		return a.ID < b.ID
	})
	return out, errs
}

// Find returns the scenario with the given ID under root.
func Find(root, id string) (*Scenario, error) {
	dirs, err := FindDirs(root)
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if IDOf(d) == id {
			return Load(d)
		}
	}
	return nil, fmt.Errorf("no scenario %q under %s (run `opsschool list`)", id, root)
}

// IDOf returns the ID of the scenario in dir, "<category>/<level>.<n>",
// from the last two path elements.
func IDOf(dir string) string {
	return filepath.Base(filepath.Dir(dir)) + "/" + filepath.Base(dir)
}

var levelNumber = regexp.MustCompile(`/([1-4])\.([1-9][0-9]*)$`)

// levelAndNumber parses the "<level>.<n>" part of an ID. It returns zeros
// when the ID doesn't have one.
func levelAndNumber(id string) (level, n int) {
	m := levelNumber.FindStringSubmatch(id)
	if m == nil {
		return 0, 0
	}
	level, _ = strconv.Atoi(m[1])
	n, _ = strconv.Atoi(m[2])
	return level, n
}

func categoryIndex(c string) int {
	for i, v := range Categories {
		if v == c {
			return i
		}
	}
	return len(Categories)
}
