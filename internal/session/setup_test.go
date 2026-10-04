package session

import (
	"context"
	"strings"
	"testing"

	"github.com/opsschool/emulator/internal/scenario"
)

// An image built from other files than the checkout's is refused before
// anything starts, with the command that rebuilds it.
func TestBringRefusesAnOldImage(t *testing.T) {
	s := &scenario.Scenario{Spec: scenario.Spec{ID: "linux/1.1", Image: "single-node"}}
	for _, built := range []string{"", "0123456789abcdef"} { // before fingerprints, and different files
		m := &fakeMachine{fingerprint: built}
		e := &Env{Home: t.TempDir(), Machine: m, Say: func(string) {}, Fingerprint: "fedcba9876543210"}
		err := e.Bring(context.Background(), s)
		if err == nil || !strings.Contains(err.Error(), "out of date") ||
			!strings.Contains(err.Error(), "opsschool image build single-node --driver fake") {
			t.Errorf("built from %q: %v", built, err)
		}
		if len(m.scripts) != 0 {
			t.Errorf("ran scripts on a refused image: %v", m.scripts)
		}
	}
}
