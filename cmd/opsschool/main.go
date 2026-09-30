// Command opsschool runs Ops School simulator scenarios.
package main

import (
	"os"

	"github.com/opsschool/simulator/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.DefaultEnv(), os.Args[1:]))
}
