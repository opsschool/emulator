package scenario

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ResolveVars picks a value for every randomized variable. The result depends
// only on the seed and the variable definitions, so the same seed always
// produces the same session.
func ResolveVars(vars map[string]Var, seed uint64) map[string]string {
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make(map[string]string, len(vars))
	for _, n := range names {
		h := fnv.New64a()
		h.Write([]byte(n))
		r := rand.New(rand.NewPCG(seed, h.Sum64()))
		v := vars[n]
		switch {
		case len(v.Choices) > 0:
			out[n] = v.Choices[r.IntN(len(v.Choices))]
		case len(v.Range) == 2:
			lo, hi := v.Range[0], v.Range[1]
			out[n] = strconv.Itoa(lo + r.IntN(hi-lo+1))
		}
	}
	return out
}

// VarEnvName returns the environment variable a randomized variable is
// exported as, for example log_name -> OPSSCHOOL_VAR_LOG_NAME.
func VarEnvName(name string) string { return "OPSSCHOOL_VAR_" + strings.ToUpper(name) }

// Env returns the environment passed to every scenario script.
func Env(id, user string, seed uint64, vars map[string]string) []string {
	env := []string{
		"OPSSCHOOL_SCENARIO=" + id,
		"OPSSCHOOL_USER=" + user,
		"OPSSCHOOL_SEED=" + strconv.FormatUint(seed, 10),
	}
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		env = append(env, VarEnvName(n)+"="+vars[n])
	}
	return env
}

var tmplRef = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.]+)\s*\}\}`)

// TemplateKeys returns the keys referenced as {{key}} in s.
func TemplateKeys(s string) []string {
	var keys []string
	for _, m := range tmplRef.FindAllStringSubmatch(s, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// Render replaces {{key}} references in s with values from data. It fails on
// a key that has no value.
func Render(s string, data map[string]string) (string, error) {
	var missing []string
	out := tmplRef.ReplaceAllStringFunc(s, func(m string) string {
		k := tmplRef.FindStringSubmatch(m)[1]
		v, ok := data[k]
		if !ok {
			missing = append(missing, k)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unknown template key(s) %s in %q", strings.Join(missing, ", "), s)
	}
	return out, nil
}

// BuiltinTemplateKeys are the template keys always available in checks.
// Randomized variables are available as var.<name>.
var BuiltinTemplateKeys = []string{"vm", "vm_admin", "prometheus"}

// TemplateData builds the data map for Render.
func TemplateData(builtins, vars map[string]string) map[string]string {
	out := make(map[string]string, len(builtins)+len(vars))
	for k, v := range builtins {
		out[k] = v
	}
	for k, v := range vars {
		out["var."+k] = v
	}
	return out
}
