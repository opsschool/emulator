package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opsschool/emulator/internal/scenario"
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
	dir := filepath.Join(t.TempDir(), "linux", "broken")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "scenario.yaml"), []byte("category: linux\n"), 0o644)
	e, out, _ := testEnv(t)
	if code := Run(e, []string{"validate", dir}); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"directory name \"broken\" must be <level>.<n>", "checks.yaml: error: file is missing", "break.sh: error: file is missing", "1 failed"} {
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
	if !strings.Contains(out.String(), "linux/1.1") {
		t.Errorf("list output: %s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	e, _, errb := testEnv(t)
	if code := Run(e, []string{"frobnicate"}); code != 2 || !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("exit %d: %s", code, errb)
	}
}

// Session commands without flags must treat -h as help, not run.
func TestSessionCommandsHelp(t *testing.T) {
	for _, c := range []string{"shell", "hint", "verify", "quiz", "stop"} {
		e, _, errb := testEnv(t)
		if code := Run(e, []string{c, "-h"}); code != 0 || !strings.Contains(errb.String(), "Usage of opsschool "+c) {
			t.Errorf("%s -h: exit %d: %s", c, code, errb)
		}
		e, _, errb = testEnv(t)
		if code := Run(e, []string{c, "extra"}); code != 2 {
			t.Errorf("%s extra: exit %d, want 2: %s", c, code, errb)
		}
	}
}

func TestCountdownNotTerminal(t *testing.T) {
	var out bytes.Buffer
	if err := countdown(context.Background(), &out, "Begins in", 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "Begins in 0:02\nBegins in 0:00\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := clock(90 * time.Second); got != "1:30" {
		t.Errorf("clock(90s) = %q", got)
	}
}

func TestSplitFirst(t *testing.T) {
	cases := []struct {
		in   []string
		id   string
		rest string
	}{
		{[]string{"linux/1.1", "--user", "jo"}, "linux/1.1", "--user jo"},
		{[]string{"--user", "jo", "linux/1.1"}, "linux/1.1", "--user jo"},
		{[]string{"--user=jo", "linux/1.1", "--seed", "3"}, "linux/1.1", "--user=jo --seed 3"},
		{[]string{"--user", "jo"}, "", "--user jo"},
	}
	for _, c := range cases {
		id, rest := splitFirst(c.in)
		if id != c.id || strings.Join(rest, " ") != c.rest {
			t.Errorf("splitFirst(%q) = %q, %q", c.in, id, rest)
		}
	}
}

func TestPrintPage(t *testing.T) {
	s := &scenario.Scenario{Spec: scenario.Spec{ID: "linux/1.1", Level: 1, Alerts: []string{"ShopErrorRate: errors"}, Summary: "Orders fail.\n"}}
	var out bytes.Buffer
	printPage(&out, s)
	want := "\nScenario linux/1.1 (level 1) has begun.\n\n  [FIRING] ShopErrorRate: errors\n\nWhat people are reporting:\n\n  Orders fail.\n\n"
	if out.String() != want {
		t.Errorf("got %q", out.String())
	}
	s.Spec.Alerts = nil
	out.Reset()
	printPage(&out, s)
	if !strings.Contains(out.String(), "No alerts are firing.") {
		t.Errorf("got %q", out.String())
	}
	if got := wrap("aa bb cc\ndd", 5); got != "aa bb\ncc\ndd" {
		t.Errorf("wrap = %q", got)
	}
}
