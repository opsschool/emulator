package scenario

import (
	"slices"
	"strconv"
	"testing"
)

func TestResolveVarsDeterministic(t *testing.T) {
	vars := map[string]Var{
		"name": {Choices: []string{"a", "b", "c"}},
		"size": {Range: []int{800, 1200}},
	}
	a := ResolveVars(vars, 42)
	b := ResolveVars(vars, 42)
	if a["name"] != b["name"] || a["size"] != b["size"] {
		t.Fatalf("same seed gave %v and %v", a, b)
	}
	seen := map[string]bool{}
	for seed := uint64(0); seed < 200; seed++ {
		v := ResolveVars(vars, seed)
		if !slices.Contains(vars["name"].Choices, v["name"]) {
			t.Fatalf("seed %d: bad choice %q", seed, v["name"])
		}
		n, err := strconv.Atoi(v["size"])
		if err != nil || n < 800 || n > 1200 {
			t.Fatalf("seed %d: size %q out of range", seed, v["size"])
		}
		seen[v["name"]] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected every choice over 200 seeds, saw %v", seen)
	}
}

func TestEnv(t *testing.T) {
	got := Env("linux-disk-full", "jdoe", 7, map[string]string{"log_name": "debug.log"})
	want := []string{"OPSSCHOOL_SCENARIO=linux-disk-full", "OPSSCHOOL_USER=jdoe", "OPSSCHOOL_SEED=7", "OPSSCHOOL_VAR_LOG_NAME=debug.log"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestRender(t *testing.T) {
	data := TemplateData(map[string]string{"vm": "127.0.0.1:8080"}, map[string]string{"log_name": "x.log"})
	got, err := Render("http://{{vm}}/health?f={{ var.log_name }}", data)
	if err != nil || got != "http://127.0.0.1:8080/health?f=x.log" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Render("{{nope}}", data); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestSplitHints(t *testing.T) {
	got := SplitHints("first\nline\n---\n\nsecond\n---  \n\n")
	if !slices.Equal(got, []string{"first\nline", "second"}) {
		t.Fatalf("got %q", got)
	}
}
