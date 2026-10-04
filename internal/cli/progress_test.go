package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBar(t *testing.T) {
	for _, c := range []struct {
		elapsed time.Duration
		full    int
	}{
		{0, 0},
		{90 * time.Second, 5},
		{10 * time.Minute, 9}, // ran over: waits short of the end
	} {
		b := bar(c.elapsed, 3*time.Minute, 10)
		if got := strings.Count(b, "█"); got != c.full || strings.Count(b, "░") != 10-c.full {
			t.Errorf("%s: %q, want %d full", c.elapsed, b, c.full)
		}
	}
}

func TestProgressOffTerminal(t *testing.T) {
	var out bytes.Buffer
	p := startProgress(&out, time.Minute)
	p.Say("Booting")
	p.Done()
	if out.String() != "==> Booting\n" {
		t.Errorf("got %q", out.String())
	}
}
