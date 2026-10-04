package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var validFiles = map[string]string{
	FileScenario: `category: linux
image: single-node
curriculum: https://ops-school.readthedocs.io/
alerts:
  - "ShopErrorRate: more than 5% of requests are failing"
summary: Something is wrong.
randomize:
  name:
    choices: [a, b]
  size:
    range: [1, 3]
`,
	FileBreak:    "#!/usr/bin/env bash\nset -euo pipefail\necho \"$OPSSCHOOL_VAR_NAME $OPSSCHOOL_VAR_SIZE\"\n",
	FileMitigate: "#!/usr/bin/env bash\nset -euo pipefail\ntrue\n",
	FileSolve:    "#!/usr/bin/env bash\nset -euo pipefail\ntrue\n",
	FileChecks: `mitigated:
  - type: http
    url: http://{{vm}}/health
    expect_status: 200
fixed:
  - type: promql
    expr: up{job="{{var.name}}"} == 1
`,
	FileQuestions: `- id: q1
  prompt: Which?
  type: choice
  choices: [x, y]
  answer: 1
- id: q2
  prompt: Name?
  type: text
  answer_from_var: name
`,
	FileHints:    "one\n---\ntwo\n",
	FileSolution: "# Stub\n",
}

// writeScenario writes the valid stub with overrides applied. An empty
// override deletes the file.
func writeScenario(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "linux", "1.1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for k, v := range validFiles {
		files[k] = v
	}
	for k, v := range overrides {
		files[k] = v
	}
	for name, body := range files {
		if body == "" {
			continue
		}
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTrailingSlash(t *testing.T) {
	dir := writeScenario(t, nil)
	s, ps := Validate(dir + "/")
	if len(ps) != 0 || s.Spec.ID != "linux/1.1" {
		t.Fatalf("ID %q, problems:\n%v", s.Spec.ID, ps)
	}
}

func TestValidStub(t *testing.T) {
	dir := writeScenario(t, nil)
	s, ps := Validate(dir)
	if len(ps) != 0 {
		t.Fatalf("expected no problems, got:\n%v", ps)
	}
	if len(s.Hints) != 2 || s.Spec.MitigateHold.Seconds() != 60 || s.Spec.TimeLimit.Minutes() != 45 {
		t.Errorf("defaults or hints not applied: %+v, hints=%q", s.Spec, s.Hints)
	}
}

func TestIDsAndOrder(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"1.10", "2.1", "1.2", "1.1"} {
		dir := filepath.Join(root, "linux", n)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, FileScenario), []byte(validFiles[FileScenario]), 0o644)
		os.WriteFile(filepath.Join(dir, FileChecks), []byte(validFiles[FileChecks]), 0o644)
	}
	scs, errs := LoadAll(root)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var ids []string
	for _, s := range scs {
		ids = append(ids, s.Spec.ID)
	}
	if got := strings.Join(ids, " "); got != "linux/1.1 linux/1.2 linux/1.10 linux/2.1" {
		t.Errorf("order: %s", got)
	}
	if s, err := Find(root, "linux/1.10"); err != nil || s.Spec.ID != "linux/1.10" || s.Spec.Level != 1 {
		t.Errorf("Find: %v %v", s, err)
	}
	if _, err := Find(root, "1.10"); err == nil {
		t.Error("Find matched an ID without a category")
	}
}

func TestBrokenScenarios(t *testing.T) {
	replace := func(file, old, new string) map[string]string {
		if !strings.Contains(validFiles[file], old) {
			t.Fatalf("%s does not contain %q", file, old)
		}
		return map[string]string{file: strings.Replace(validFiles[file], old, new, 1)}
	}
	cases := []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{"id in file", replace(FileScenario, "category: linux", "id: linux-stub\ncategory: linux"), "field id not found"},
		{"multi-line alert", replace(FileScenario, `- "ShopErrorRate`, `- "ShopErrorRate\n`), "alerts[0] must be one line"},
		{"bad category", replace(FileScenario, "category: linux", "category: cooking"), `category "cooking" must be one of`},
		{"wrong category dir", replace(FileScenario, "category: linux", "category: databases"), "move it to scenarios/databases/1.1"},
		{"level in file", replace(FileScenario, "image: single-node", "image: single-node\nlevel: 1"), "field level not found"},
		{"no summary", replace(FileScenario, "summary: Something is wrong.", ""), "summary is required"},
		{"bad curriculum", replace(FileScenario, "https://ops-school.readthedocs.io/", "chapter 3"), "curriculum must be an http(s) URL"},
		{"unknown field", replace(FileScenario, "image: single-node", "image: single-node\nimgae: x"), "field imgae not found"},
		{"bad duration", replace(FileScenario, "image: single-node", "image: single-node\ntime_limit: soon"), `invalid duration "soon"`},
		{"reversed range", replace(FileScenario, "range: [1, 3]", "range: [3, 1]"), "range [3, 1] is reversed"},
		{"unused var", replace(FileBreak, " $OPSSCHOOL_VAR_SIZE", ""), "randomized variable size is never used"},
		{"missing solve", map[string]string{FileSolve: ""}, "solve.sh: error: file is missing"},
		{"missing checks", map[string]string{FileChecks: ""}, "checks.yaml: error: file is missing"},
		{"no pipefail", map[string]string{FileBreak: "#!/usr/bin/env bash\necho $OPSSCHOOL_VAR_NAME $OPSSCHOOL_VAR_SIZE\n"}, "must run `set -euo pipefail`"},
		{"no fixed checks", replace(FileChecks, "fixed:", "other:"), "field other not found"},
		{"bad check type", replace(FileChecks, "type: promql", "type: ping"), `type "ping" must be http, promql or script`},
		{"unknown template key", replace(FileChecks, "{{vm}}", "{{host}}"), "unknown template key {{host}}"},
		{"missing check script", replace(FileChecks, "type: promql\n    expr: up{job=\"{{var.name}}\"} == 1", "type: script\n    run: checks/nope.sh"), "script checks/nope.sh not found"},
		{"answer out of range", replace(FileQuestions, "answer: 1", "answer: 5"), "answer 5 is out of range for 2 choices"},
		{"answer var missing", replace(FileQuestions, "answer_from_var: name", "answer_from_var: nope"), `answer_from_var "nope" is not a randomized variable`},
		{"touches eth0", replace(FileBreak, "echo", "ip link set eth0 down; echo"), "management interface (eth0); break the service-facing path instead [mgmt-iface]"},
		{"flushes firewall", replace(FileBreak, "echo", "iptables -F\necho"), "[firewall-wide]"},
		{"drop policy", replace(FileBreak, "echo", "iptables -P INPUT DROP\necho"), "[firewall-wide]"},
		{"stops exporter", replace(FileBreak, "echo", "systemctl stop prometheus-node-exporter\necho"), "[exporters]"},
		{"stops ssh", replace(FileBreak, "echo", "systemctl stop ssh\necho"), "[ssh]"},
		{"stops alloy", replace(FileBreak, "echo", "systemctl stop alloy\necho"), "[alloy]"},
		{"bad dashboard", map[string]string{FileDashboard: "{nope"}, "dashboard.json: error: not valid JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := writeScenario(t, c.overrides)
			_, ps := Validate(dir)
			if !HasErrors(ps) {
				t.Fatalf("expected errors, got %v", ps)
			}
			var all []string
			for _, p := range ps {
				all = append(all, p.String())
			}
			joined := strings.Join(all, "\n")
			if !strings.Contains(joined, c.want) {
				t.Errorf("want message containing %q, got:\n%s", c.want, joined)
			}
		})
	}
}

func TestLintAllowComment(t *testing.T) {
	dir := writeScenario(t, map[string]string{FileBreak: "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"iptables -A INPUT -i lo -p tcp --dport 8080 -j DROP # scoped to the app port\n" +
		"ip route get 1.1.1.1 | grep eth0 >/dev/null # lint:allow mgmt-iface\n" +
		"echo $OPSSCHOOL_VAR_NAME $OPSSCHOOL_VAR_SIZE\n"})
	if _, ps := Validate(dir); HasErrors(ps) {
		t.Fatalf("expected no errors, got %v", ps)
	}
}

func TestWarnings(t *testing.T) {
	dir := writeScenario(t, map[string]string{FileHints: "", FileSolution: ""})
	_, ps := Validate(dir)
	if HasErrors(ps) || len(ps) != 2 {
		t.Fatalf("expected two warnings, got %v", ps)
	}
}

func TestRepoScenariosValid(t *testing.T) {
	dirs, err := FindDirs("../../scenarios")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no scenarios found")
	}
	for _, d := range dirs {
		if _, ps := Validate(d); HasErrors(ps) {
			t.Errorf("%s:\n%v", d, ps)
		}
	}
}
