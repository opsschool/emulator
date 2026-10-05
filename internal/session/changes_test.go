package session

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/opsschool/emulator/internal/scenario"
)

func TestChanges(t *testing.T) {
	cs, err := scenario.ParseChanges([]byte(`
- {id: NET-1, title: "DNS to ${ip}", author: a, before: 10m, when: {variant: dns}}
- {id: NET-2, title: Other variant, author: a, before: 5m, when: {variant: arp}}
`))
	if err != nil {
		t.Fatal(err)
	}
	s := &scenario.Scenario{Spec: scenario.Spec{ID: "networking/9.9"}, Changes: cs}
	d := &Daemon{Log: log.New(io.Discard, "", 0), Scenario: s}
	baselineEnds := time.Now().Add(-time.Minute)
	d.st = &State{Image: "single-node", BaselineEnds: baselineEnds, Vars: map[string]string{"variant": "dns", "ip": "10.53.0.99"}}

	ids := func(vs []ChangeView) (out []string) {
		for _, v := range vs {
			out = append(out, v.ID)
		}
		return out
	}
	// Before the scenario begins, only the image's background changes show.
	before := d.changes()
	if len(before) == 0 {
		t.Fatal("no background changes")
	}
	for _, v := range before {
		if v.ID == "NET-1" || v.ID == "NET-2" {
			t.Fatalf("scenario change shown before the break: %v", ids(before))
		}
		if !v.At.Before(baselineEnds.Add(-Baseline)) {
			t.Errorf("%s dated %s, after the session started", v.ID, v.At)
		}
	}

	// After it begins, this variant's change shows first, resolved.
	d.st.StartedAt = time.Now()
	after := d.changes()
	if len(after) != len(before)+1 || after[0].ID != "NET-1" || after[0].Title != "DNS to 10.53.0.99" {
		t.Fatalf("changes after begin: %v (first %+v)", ids(after), after[0])
	}
	if want := d.st.StartedAt.Add(-10 * time.Minute); !after[0].At.Equal(want) {
		t.Errorf("NET-1 at %s, want %s", after[0].At, want)
	}
	for i := 1; i < len(after); i++ {
		if after[i].At.After(after[i-1].At) {
			t.Errorf("not newest first: %v", ids(after))
		}
	}
}
