package scenario

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestQuizCheck(t *testing.T) {
	var qs []Question
	err := yaml.Unmarshal([]byte(`
- id: c
  prompt: p
  type: choice
  choices: [a, b, c]
  answer: 1
- id: m
  prompt: p
  type: multi
  choices: [a, b, c]
  answer: [0, 2]
- id: t
  prompt: p
  type: text
  answer: MySQL
- id: v
  prompt: p
  type: text
  answer_from_var: log_name
`), &qs)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"log_name": "debug.log"}
	cases := []struct {
		q      int
		answer string
		want   bool
	}{
		{0, "2", true}, {0, "1", false},
		{1, "1,3", true}, {1, "3 1", true}, {1, "1", false}, {1, "1,2,3", false},
		{2, "  mysql ", true}, {2, "redis", false},
		{3, "Debug.log", true}, {3, "trace.log", false},
	}
	for _, c := range cases {
		got, err := qs[c.q].Check(c.answer, vars)
		if err != nil || got != c.want {
			t.Errorf("question %s answer %q: got %v, %v; want %v", qs[c.q].ID, c.answer, got, err, c.want)
		}
	}
	if _, err := qs[0].Check("9", vars); err == nil {
		t.Error("expected error for out-of-range choice")
	}
	if err := CheckQuizVars(qs, map[string]string{}); err == nil {
		t.Error("expected error for missing var")
	}
}
