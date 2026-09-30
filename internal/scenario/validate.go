package scenario

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Severity of a validation problem.
type Severity int

const (
	SevError Severity = iota
	SevWarning
)

func (s Severity) String() string {
	if s == SevWarning {
		return "warning"
	}
	return "error"
}

// Problem is one validation finding.
type Problem struct {
	File     string
	Line     int // 0 when not tied to a line
	Severity Severity
	Msg      string
}

func (p Problem) String() string {
	loc := p.File
	if p.Line > 0 {
		loc += ":" + strconv.Itoa(p.Line)
	}
	return fmt.Sprintf("%s: %s: %s", loc, p.Severity, p.Msg)
}

// HasErrors reports whether any problem is an error.
func HasErrors(ps []Problem) bool {
	return slices.ContainsFunc(ps, func(p Problem) bool { return p.Severity == SevError })
}

// Default values applied when scenario.yaml leaves them out.
const (
	DefaultMitigateHoldSeconds = 60
	DefaultTimeLimitMinutes    = 45
)

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	varNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Validate loads the scenario in dir and checks it against the schema and
// lint rules in docs/design.md. The scenario is returned when it parsed, even
// if it has problems.
func Validate(dir string) (*Scenario, []Problem) {
	s, ps := load(dir)
	v := &validator{s: s, ps: ps}
	v.spec()
	v.files()
	v.checks()
	v.questions()
	v.breakScript()
	return s, v.ps
}

type validator struct {
	s  *Scenario
	ps []Problem
}

func (v *validator) errf(file string, line int, format string, args ...any) {
	v.ps = append(v.ps, Problem{File: filepath.Join(v.s.Dir, file), Line: line, Severity: SevError, Msg: fmt.Sprintf(format, args...)})
}

func (v *validator) warnf(file string, format string, args ...any) {
	v.ps = append(v.ps, Problem{File: filepath.Join(v.s.Dir, file), Severity: SevWarning, Msg: fmt.Sprintf(format, args...)})
}

func (v *validator) spec() {
	sp := v.s.Spec
	f := FileScenario
	dirName := filepath.Base(v.s.Dir)
	switch {
	case sp.ID == "":
		v.errf(f, 0, "id is required")
	case !idPattern.MatchString(sp.ID):
		v.errf(f, 0, "id %q must be lowercase words separated by hyphens", sp.ID)
	case sp.ID != dirName:
		v.errf(f, 0, "id %q does not match directory name %q", sp.ID, dirName)
	}
	if sp.Title == "" {
		v.errf(f, 0, "title is required")
	}
	if !slices.Contains(Categories, sp.Category) {
		v.errf(f, 0, "category %q must be one of %s", sp.Category, strings.Join(Categories, ", "))
	} else if parent := filepath.Base(filepath.Dir(v.s.Dir)); parent != sp.Category {
		v.errf(f, 0, "scenario is in directory %q but its category is %q; move it to scenarios/%s/%s", parent, sp.Category, sp.Category, dirName)
	}
	if sp.Level < 1 || sp.Level > 4 {
		v.errf(f, 0, "level must be 1, 2, 3 or 4, got %d", sp.Level)
	}
	if !slices.Contains(Images, sp.Image) {
		v.errf(f, 0, "image %q must be one of %s", sp.Image, strings.Join(Images, ", "))
	}
	if strings.TrimSpace(sp.Summary) == "" {
		v.errf(f, 0, "summary is required: describe the symptoms the way a page or user report would")
	}
	if u, err := url.Parse(sp.Curriculum); sp.Curriculum == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		v.errf(f, 0, "curriculum must be an http(s) URL of the chapter this scenario exercises, got %q", sp.Curriculum)
	}
	for name, vr := range sp.Randomize {
		if !varNamePattern.MatchString(name) {
			v.errf(f, 0, "randomize.%s: variable names must be lowercase letters, digits and underscores", name)
		}
		switch {
		case len(vr.Choices) > 0 && len(vr.Range) > 0:
			v.errf(f, 0, "randomize.%s: set choices or range, not both", name)
		case len(vr.Choices) > 0:
		case len(vr.Range) == 2:
			if vr.Range[0] > vr.Range[1] {
				v.errf(f, 0, "randomize.%s: range [%d, %d] is reversed", name, vr.Range[0], vr.Range[1])
			}
		case len(vr.Range) > 0:
			v.errf(f, 0, "randomize.%s: range must be [min, max]", name)
		default:
			v.errf(f, 0, "randomize.%s: needs choices or range", name)
		}
	}
	if sp.MitigateHold.Duration < 0 || sp.TimeLimit.Duration < 0 || sp.FixVerification.LoadReplay.Duration < 0 {
		v.errf(f, 0, "durations must not be negative")
	}
	if sp.Load.Profile != "" && !slices.Contains(LoadProfiles, sp.Load.Profile) {
		v.errf(f, 0, "load.profile %q must be one of %s", sp.Load.Profile, strings.Join(LoadProfiles, ", "))
	}
	if sp.Load.Profile != "" && len(sp.Load.Schedule) > 0 {
		v.errf(f, 0, "load: set profile or schedule, not both")
	}
	for i, st := range sp.Load.Schedule {
		if st.RPS < 0 || st.Duration.Duration <= 0 {
			v.errf(f, 0, "load.schedule[%d]: rps must be >= 0 and duration > 0", i)
		}
	}
}

func (v *validator) files() {
	for _, name := range []string{FileBreak, FileMitigate, FileSolve} {
		b, err := os.ReadFile(v.s.Path(name))
		if err != nil {
			v.errf(name, 0, "file is missing")
			continue
		}
		v.shellScript(name, string(b))
	}
	if len(v.s.Hints) == 0 {
		v.warnf(FileHints, "no hints; add ordered hints separated by \"---\" lines")
	}
	if _, err := os.Stat(v.s.Path(FileSolution)); err != nil {
		v.warnf(FileSolution, "no solution writeup")
	}
	if v.s.Dashboard != nil && !json.Valid(v.s.Dashboard) {
		v.errf(FileDashboard, 0, "not valid JSON")
	}
}

// shellScript applies the bash conventions from docs/design.md.
func (v *validator) shellScript(name, body string) {
	first, _, _ := strings.Cut(body, "\n")
	if !strings.HasPrefix(first, "#!") || !strings.Contains(first, "bash") {
		v.errf(name, 1, "must start with a bash shebang, e.g. #!/usr/bin/env bash")
	}
	if !regexp.MustCompile(`(?m)^\s*set -euo pipefail\b`).MatchString(body) {
		v.errf(name, 0, "must run `set -euo pipefail`")
	}
}

func (v *validator) checks() {
	c := v.s.Checks
	f := FileChecks
	if _, err := os.Stat(v.s.Path(f)); err != nil {
		return // reported by load()
	}
	if len(c.Mitigated) == 0 {
		v.errf(f, 0, "mitigated needs at least one check")
	}
	if len(c.Fixed) == 0 {
		v.errf(f, 0, "fixed needs at least one check")
	}
	known := map[string]bool{}
	for _, k := range BuiltinTemplateKeys {
		known[k] = true
	}
	for n := range v.s.Spec.Randomize {
		known["var."+n] = true
	}
	for tier, list := range map[string][]Check{"mitigated": c.Mitigated, "fixed": c.Fixed, "preserve": c.Preserve} {
		for i, ch := range list {
			where := fmt.Sprintf("%s[%d]", tier, i)
			switch ch.Type {
			case CheckHTTP:
				if ch.URL == "" {
					v.errf(f, 0, "%s: http check needs url", where)
				}
				if ch.ExpectStatus == 0 && ch.ExpectBody == "" {
					v.errf(f, 0, "%s: http check needs expect_status or expect_body", where)
				}
			case CheckPromQL:
				if ch.Expr == "" {
					v.errf(f, 0, "%s: promql check needs expr", where)
				}
			case CheckScript:
				if ch.Run == "" {
					v.errf(f, 0, "%s: script check needs run", where)
				} else if b, err := os.ReadFile(v.s.Path(ch.Run)); err != nil {
					v.errf(f, 0, "%s: script %s not found in the scenario directory", where, ch.Run)
				} else {
					v.shellScript(ch.Run, string(b))
				}
			default:
				v.errf(f, 0, "%s: type %q must be http, promql or script", where, ch.Type)
			}
			for _, s := range []string{ch.URL, ch.Expr, ch.ExpectBody} {
				for _, k := range TemplateKeys(s) {
					if !known[k] {
						v.errf(f, 0, "%s: unknown template key {{%s}}; available: vm, vm_admin, prometheus, var.<randomized variable>", where, k)
					}
				}
			}
		}
	}
}

func (v *validator) questions() {
	f := FileQuestions
	seen := map[string]bool{}
	for i, q := range v.s.Questions {
		where := fmt.Sprintf("question %d", i+1)
		if q.ID != "" {
			where = "question " + q.ID
		}
		if q.ID == "" {
			v.errf(f, 0, "%s: id is required", where)
		} else if seen[q.ID] {
			v.errf(f, 0, "%s: duplicate id", where)
		}
		seen[q.ID] = true
		if q.Prompt == "" {
			v.errf(f, 0, "%s: prompt is required", where)
		}
		if _, err := q.expected(nil); err != nil && q.AnswerFromVar == "" {
			v.errf(f, q.Answer.Line, "%s: %s", where, err)
		}
		if q.AnswerFromVar != "" {
			if q.Type != QuestionText {
				v.errf(f, 0, "%s: answer_from_var only works with type text", where)
			}
			if _, ok := v.s.Spec.Randomize[q.AnswerFromVar]; !ok {
				v.errf(f, 0, "%s: answer_from_var %q is not a randomized variable", where, q.AnswerFromVar)
			}
		}
	}
}

// Management-channel lint. break.sh must not touch anything the harness
// relies on to observe and grade the VM. Add `# lint:allow <rule>` to a line
// to override a rule on that line.
var breakRules = []struct {
	id, desc string
	re       *regexp.Regexp
}{
	{"mgmt-iface", "touches the management interface (eth0); break the service-facing path instead", regexp.MustCompile(`\beth0\b`)},
	{"firewall-wide", "changes firewall policy or flushes all rules; scope rules to service ports", regexp.MustCompile(`\b(iptables|ip6tables|nft)\b.*(\s-P\s|\s-F\s*($|[;&|])|\s--flush\s*($|[;&|])|flush ruleset)`)},
	{"ssh", "touches SSH, which the harness uses", regexp.MustCompile(`\bsshd?\b|\bdport 22\b|:22\b`)},
	{"exporters", "touches an exporter", regexp.MustCompile(`node_exporter|node-exporter|process-exporter|mysqld_exporter|mysqld-exporter|redis_exporter|redis-exporter|\b91(00|04|21)\b|\b9256\b`)},
	{"alloy", "touches Grafana Alloy (log shipping)", regexp.MustCompile(`\balloy\b`)},
	{"harness", "touches harness files", regexp.MustCompile(`/opt/opsschool|/etc/opsschool|/var/lib/opsschool`)},
}

func (v *validator) breakScript() {
	b, err := os.ReadFile(v.s.Path(FileBreak))
	if err != nil {
		return // reported by files()
	}
	body := string(b)
	for i, line := range strings.Split(body, "\n") {
		code, _, _ := strings.Cut(line, "#")
		for _, r := range breakRules {
			if r.re.MatchString(code) && !strings.Contains(line, "lint:allow "+r.id) {
				v.errf(FileBreak, i+1, "%s [%s]", r.desc, r.id)
			}
		}
	}
	for name := range v.s.Spec.Randomize {
		if !strings.Contains(body, VarEnvName(name)) {
			v.errf(FileBreak, 0, "randomized variable %s is never used (expected $%s)", name, VarEnvName(name))
		}
	}
}
