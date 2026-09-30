package scenario

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// expected returns the normalized correct answer. vars is used for
// answer_from_var and may be nil when only checking the definition.
func (q Question) expected(vars map[string]string) ([]string, error) {
	if q.AnswerFromVar != "" {
		v, ok := vars[q.AnswerFromVar]
		if !ok {
			return nil, fmt.Errorf("no value for variable %q", q.AnswerFromVar)
		}
		return []string{normalizeText(v)}, nil
	}
	if q.Answer.Kind == 0 {
		return nil, errors.New("answer or answer_from_var is required")
	}
	switch q.Type {
	case QuestionChoice:
		var i int
		if err := q.Answer.Decode(&i); err != nil {
			return nil, errors.New("choice answer must be the index of the correct choice, starting at 0")
		}
		if len(q.Choices) < 2 {
			return nil, errors.New("choice questions need at least two choices")
		}
		if i < 0 || i >= len(q.Choices) {
			return nil, fmt.Errorf("answer %d is out of range for %d choices", i, len(q.Choices))
		}
		return []string{strconv.Itoa(i)}, nil
	case QuestionMulti:
		var is []int
		if err := q.Answer.Decode(&is); err != nil || len(is) == 0 {
			return nil, errors.New("multi answer must be a list of choice indexes, starting at 0")
		}
		if len(q.Choices) < 2 {
			return nil, errors.New("multi questions need at least two choices")
		}
		out := make([]string, 0, len(is))
		for _, i := range is {
			if i < 0 || i >= len(q.Choices) {
				return nil, fmt.Errorf("answer %d is out of range for %d choices", i, len(q.Choices))
			}
			out = append(out, strconv.Itoa(i))
		}
		sort.Strings(out)
		return slices.Compact(out), nil
	case QuestionText:
		var s string
		if err := q.Answer.Decode(&s); err != nil || strings.TrimSpace(s) == "" {
			return nil, errors.New("text answer must be a non-empty string")
		}
		return []string{normalizeText(s)}, nil
	}
	return nil, fmt.Errorf("type %q must be choice, multi or text", q.Type)
}

func normalizeText(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Check grades a learner's answer. For choice and multi questions the answer
// is choice numbers as shown to the learner (starting at 1), separated by
// commas or spaces.
func (q Question) Check(answer string, vars map[string]string) (bool, error) {
	want, err := q.expected(vars)
	if err != nil {
		return false, err
	}
	var got []string
	switch q.Type {
	case QuestionChoice, QuestionMulti:
		for _, f := range strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' }) {
			n, err := strconv.Atoi(f)
			if err != nil || n < 1 || n > len(q.Choices) {
				return false, fmt.Errorf("answer with choice numbers 1-%d", len(q.Choices))
			}
			got = append(got, strconv.Itoa(n-1))
		}
		sort.Strings(got)
		got = slices.Compact(got)
	default:
		got = []string{normalizeText(answer)}
	}
	return slices.Equal(got, want), nil
}

// CheckQuizVars confirms every question has a valid answer for the given
// variables. CI runs it for several seeds.
func CheckQuizVars(qs []Question, vars map[string]string) error {
	for _, q := range qs {
		if _, err := q.expected(vars); err != nil {
			return fmt.Errorf("question %s: %w", q.ID, err)
		}
	}
	return nil
}
