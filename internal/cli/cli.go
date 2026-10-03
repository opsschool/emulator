// Package cli implements the opsschool command line.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Env is what a command runs against. Tests replace its fields.
type Env struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	Getenv         func(string) string
	Getwd          func() (string, error)
}

// DefaultEnv uses the process's standard streams and environment.
func DefaultEnv() *Env {
	return &Env{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, Getenv: os.Getenv, Getwd: os.Getwd}
}

type command struct {
	usage string
	help  string
	run   func(e *Env, args []string) error
}

var commands = map[string]command{}

func register(name, usage, help string, run func(e *Env, args []string) error) {
	commands[name] = command{usage: usage, help: help, run: run}
}

// errUsage makes Run print the command's usage line.
var errUsage = errors.New("usage")

// exitError carries a specific exit code.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// Run runs the command line and returns the process exit code.
func Run(e *Env, args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printHelp(e.Stdout)
		return 0
	}
	c, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(e.Stderr, "opsschool: unknown command %q\n\n", args[0])
		printHelp(e.Stderr)
		return 2
	}
	err := c.run(e, args[1:])
	var ee exitError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(e.Stderr, "usage: opsschool %s\n", c.usage)
		return 2
	case errors.As(err, &ee):
		return ee.code
	default:
		fmt.Fprintf(e.Stderr, "opsschool %s: %v\n", args[0], err)
		return 1
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "opsschool: debug a broken production environment and get graded.")
	fmt.Fprintln(w, "\nCommands:")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-36s %s\n", commands[n].usage, commands[n].help)
	}
	fmt.Fprintln(w, "\nRun `opsschool <command> -h` for a command's flags.")
}

func newFlags(e *Env, name string) *flag.FlagSet {
	fs := flag.NewFlagSet("opsschool "+name, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	return fs
}

// noArgs is for commands without flags or arguments: it handles -h and
// refuses anything else, so `opsschool stop -h` doesn't end the session.
func noArgs(e *Env, name string, args []string) error {
	fs := newFlags(e, name)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errUsage
	}
	return nil
}

// scenariosRoot finds the scenarios directory: the flag value, then
// $OPSSCHOOL_SCENARIOS, then ./scenarios or the nearest parent that has one.
func scenariosRoot(e *Env, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if v := e.Getenv("OPSSCHOOL_SCENARIOS"); v != "" {
		return v, nil
	}
	wd, err := e.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; d = filepath.Dir(d) {
		p := filepath.Join(d, "scenarios")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, nil
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return "", errors.New("no scenarios directory found; run from the emulator repo or set OPSSCHOOL_SCENARIOS")
}

// Home returns the opsschool state directory, $OPSSCHOOL_HOME or ~/.opsschool.
func Home(e *Env) (string, error) {
	if v := e.Getenv("OPSSCHOOL_HOME"); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".opsschool"), nil
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+prefix)
}

// wrap breaks each line of s at spaces so no line is longer than width,
// unless a single word is.
func wrap(s string, width int) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		cur := ""
		for _, w := range strings.Fields(line) {
			if cur != "" && len(cur)+1+len(w) > width {
				out = append(out, cur)
				cur = ""
			}
			if cur != "" {
				cur += " "
			}
			cur += w
		}
		out = append(out, cur)
	}
	return strings.Join(out, "\n")
}
