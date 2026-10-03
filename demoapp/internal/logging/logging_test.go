package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalPriorities(t *testing.T) {
	var out bytes.Buffer
	file := filepath.Join(t.TempDir(), "app.log")
	log, err := newLogger("debug", file, &out)
	if err != nil {
		t.Fatal(err)
	}
	log = log.With("component", "serve")
	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e", "err", "boom")

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{"<7>{", "<6>{", "<4>{", "<3>{"}
	if len(lines) != len(want) {
		t.Fatalf("got %d stdout lines, want %d:\n%s", len(lines), len(want), out.String())
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, want[i]) || !strings.Contains(l, `"component":"serve"`) {
			t.Errorf("stdout line %d = %q, want prefix %q and the component attribute", i, l, want[i])
		}
	}

	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.HasPrefix(l, "{") {
			t.Errorf("file line %q should be plain JSON", l)
		}
	}
}

func TestLevelFilter(t *testing.T) {
	var out bytes.Buffer
	log, err := newLogger("warn", "", &out)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("hidden")
	log.WithGroup("g").Warn("shown", "k", 1)
	if s := out.String(); strings.Contains(s, "hidden") || !strings.HasPrefix(s, "<4>") || !strings.Contains(s, `"g":{"k":1}`) {
		t.Errorf("stdout = %q", s)
	}
}
