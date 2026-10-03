package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
)

func init() {
	register("list", "list [--user name]", "List scenarios (<category>/<level>.<n>) and your best result.", runList)
}

func runList(e *Env, args []string) error {
	fs := newFlags(e, "list")
	user := fs.String("user", "", "show best results for this user (default: $OPSSCHOOL_USER)")
	root := fs.String("scenarios", "", "scenarios directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	if *user == "" {
		*user = e.Getenv("OPSSCHOOL_USER")
	}
	dir, err := scenariosRoot(e, *root)
	if err != nil {
		return err
	}
	scs, errs := scenario.LoadAll(dir)
	for _, err := range errs {
		fmt.Fprintf(e.Stderr, "skipping: %v\n", err)
	}
	best := map[string]results.Result{}
	if home, err := Home(e); err == nil {
		if rs, err := results.Open(home).All(); err != nil {
			fmt.Fprintf(e.Stderr, "reading results: %v\n", err)
		} else {
			best = results.Best(rs, *user)
		}
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tBEST")
	for _, s := range scs {
		fmt.Fprintf(tw, "%s\t%s\n", s.Spec.ID, bestLabel(best, s.Spec.ID))
	}
	return tw.Flush()
}

func bestLabel(best map[string]results.Result, id string) string {
	r, ok := best[id]
	if !ok {
		return "-"
	}
	var tiers []string
	for _, t := range results.Tiers {
		if r.Passed(t) {
			tiers = append(tiers, t)
		}
	}
	if len(tiers) == 0 {
		return fmt.Sprintf("%d pts (no tiers)", r.Score)
	}
	return fmt.Sprintf("%d pts (%s)", r.Score, strings.Join(tiers, ", "))
}
