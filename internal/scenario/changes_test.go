package scenario

import (
	"strings"
	"testing"
	"time"
)

func TestParseChanges(t *testing.T) {
	cs, err := ParseChanges([]byte(`
- id: NET-412
  title: Move DNS to ${resolver}
  author: netconfig
  team: Network team
  before: 12m
  when: {variant: dns}
  status: Pushed
  description: Points hosts at ${resolver}.
  diff: |
    -DNS=10.53.0.10
    +DNS=${resolver}
`))
	if err != nil {
		t.Fatal(err)
	}
	c := cs[0]
	if c.Before.Duration != 12*time.Minute {
		t.Errorf("before %v", c.Before.Duration)
	}
	if c.Applies(map[string]string{"variant": "other"}) || !c.Applies(map[string]string{"variant": "dns"}) {
		t.Error("when doesn't select the variant")
	}
	r := c.Resolve(map[string]string{"resolver": "10.53.0.99"})
	if r.Title != "Move DNS to 10.53.0.99" || !strings.Contains(r.Diff, "+DNS=10.53.0.99") || !strings.Contains(r.Description, "10.53.0.99") {
		t.Errorf("not resolved: %+v", r)
	}
	if got := c.unknownVars(map[string]Var{"resolver": {}}); len(got) != 1 || got[0] != "variant" {
		t.Errorf("unknown vars %v, want [variant]", got)
	}

	for _, bad := range []string{
		"- {id: X, title: T, author: a}",                   // no before
		"- {id: X, title: T, author: a, before: 1h, x: 1}", // unknown field
	} {
		if _, err := ParseChanges([]byte(bad)); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	if cs, err := ParseChanges(nil); err != nil || len(cs) != 0 {
		t.Errorf("empty file: %v %v", cs, err)
	}
}
