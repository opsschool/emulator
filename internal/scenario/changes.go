package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

// FileChanges lists the changes the learner sees on the session page's
// Recent changes tab. Optional.
const FileChanges = "changes.yaml"

// Change is a pull request, config push or change ticket, as an on-call
// engineer would find it in the team's tools. A scenario's changes went
// out before the page; an image's changes are background, shown in every
// scenario, so that a list of changes is never a pointer on its own.
type Change struct {
	// ID is how the team refers to it, such as "shop#1182" or "CHG-2291".
	ID    string `yaml:"id" json:"id"`
	Title string `yaml:"title" json:"title"`
	// Author is a person; Team is who they work for.
	Author string `yaml:"author" json:"author"`
	Team   string `yaml:"team" json:"team"`
	// Before is how long before the page it went out (for a scenario's
	// changes) or before the session started (for an image's).
	Before Duration `yaml:"before" json:"-"`
	// Status is how far it got, such as "Merged and deployed".
	Status      string `yaml:"status" json:"status"`
	Description string `yaml:"description" json:"description"`
	// Diff is a unified diff. Optional: some changes have none.
	Diff string `yaml:"diff" json:"diff,omitempty"`
	// When limits the change to sessions whose randomized variables have
	// these values, for scenarios whose variants change different things.
	When map[string]string `yaml:"when" json:"-"`
}

// Applies reports whether the change belongs in a session with these
// variable values.
func (c Change) Applies(vars map[string]string) bool {
	for k, v := range c.When {
		if vars[k] != v {
			return false
		}
	}
	return true
}

// ParseChanges reads a changes.yaml.
func ParseChanges(b []byte) ([]Change, error) {
	var cs []Change
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cs); err != nil && !errors.Is(err, io.EOF) {
		return nil, cleanYAMLError(err)
	}
	for i, c := range cs {
		if c.ID == "" || c.Title == "" || c.Author == "" || c.Before.Duration <= 0 {
			return nil, fmt.Errorf("change %d: id, title, author and a positive before are required", i+1)
		}
	}
	return cs, nil
}

var varRef = regexp.MustCompile(`\$\{([a-z0-9_]+)\}`)

// Resolve fills ${name} references to the scenario's randomized variables
// in a change's text, the way break scripts see them.
func (c Change) Resolve(vars map[string]string) Change {
	sub := func(s string) string {
		return varRef.ReplaceAllStringFunc(s, func(m string) string {
			if v, ok := vars[m[2:len(m)-1]]; ok {
				return v
			}
			return m
		})
	}
	c.Title, c.Description, c.Diff = sub(c.Title), sub(c.Description), sub(c.Diff)
	return c
}

// unknownVars lists ${name} references that aren't randomized variables.
func (c Change) unknownVars(vars map[string]Var) []string {
	var out []string
	for k := range c.When {
		if _, ok := vars[k]; !ok {
			out = append(out, k)
		}
	}
	for _, s := range []string{c.Title, c.Description, c.Diff} {
		for _, m := range varRef.FindAllStringSubmatch(s, -1) {
			if _, ok := vars[m[1]]; !ok {
				out = append(out, m[1])
			}
		}
	}
	return out
}
