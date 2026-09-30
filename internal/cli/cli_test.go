package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv(t *testing.T) (*Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	home := t.TempDir()
	e := &Env{
		Stdout: &out, Stderr: &errb, Stdin: strings.NewReader(""),
		Getenv: func(k string) string {
			if k == "OPSSCHOOL_HOME" {
				return home
			}
			return ""
		},
		Getwd: func() (string, error) { return filepath.Abs("../..") },
	}
	return e, &out, &errb
}

func TestValidateRepoScenarios(t *testing.T) {
	e, out, errb := testEnv(t)
	if code := Run(e, []string{"validate", "../../scenarios"}); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errb)
	}
	if !strings.Contains(out.String(), "0 failed") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestValidateBroken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "linux", "linux-broken")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "scenario.yaml"), []byte("id: linux-broken\nlevel: 9\n"), 0o644)
	e, out, _ := testEnv(t)
	if code := Run(e, []string{"validate", dir}); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"level must be 1, 2, 3 or 4", "checks.yaml: error: file is missing", "break.sh: error: file is missing", "1 failed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestList(t *testing.T) {
	e, out, errb := testEnv(t)
	if code := Run(e, []string{"list"}); code != 0 {
		t.Fatalf("exit %d\n%s", code, errb)
	}
	if !strings.Contains(out.String(), "linux-disk-full") {
		t.Errorf("list output: %s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	e, _, errb := testEnv(t)
	if code := Run(e, []string{"frobnicate"}); code != 2 || !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("exit %d: %s", code, errb)
	}
}

func TestSplitFirst(t *testing.T) {
	cases := []struct {
		in   []string
		id   string
		rest string
	}{
		{[]string{"linux-disk-full", "--user", "jo"}, "linux-disk-full", "--user jo"},
		{[]string{"--user", "jo", "linux-disk-full"}, "linux-disk-full", "--user jo"},
		{[]string{"--user=jo", "linux-disk-full", "--seed", "3"}, "linux-disk-full", "--user=jo --seed 3"},
		{[]string{"--user", "jo"}, "", "--user jo"},
	}
	for _, c := range cases {
		id, rest := splitFirst(c.in)
		if id != c.id || strings.Join(rest, " ") != c.rest {
			t.Errorf("splitFirst(%q) = %q, %q", c.in, id, rest)
		}
	}
}
