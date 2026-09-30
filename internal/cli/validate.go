package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opsschool/simulator/internal/scenario"
)

func init() {
	register("validate", "validate <path>...", "Validate scenario directories (schema and lint).", runValidate)
}

// runValidate accepts scenario directories or directories containing
// scenarios, such as scenarios/.
func runValidate(e *Env, args []string) error {
	fs := newFlags(e, "validate")
	quiet := fs.Bool("q", false, "print errors only, not warnings")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errUsage
	}
	var dirs []string
	for _, p := range fs.Args() {
		if _, err := os.Stat(filepath.Join(p, scenario.FileScenario)); err == nil {
			dirs = append(dirs, p)
			continue
		}
		found, err := scenario.FindDirs(p)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return fmt.Errorf("%s: no %s found", p, scenario.FileScenario)
		}
		dirs = append(dirs, found...)
	}
	failed := 0
	for _, d := range dirs {
		_, ps := scenario.Validate(d)
		for _, p := range ps {
			if *quiet && p.Severity == scenario.SevWarning {
				continue
			}
			fmt.Fprintln(e.Stdout, p)
		}
		if scenario.HasErrors(ps) {
			failed++
		}
	}
	fmt.Fprintf(e.Stdout, "%d scenario(s) checked, %d failed\n", len(dirs), failed)
	if failed > 0 {
		return exitError{1}
	}
	return nil
}
