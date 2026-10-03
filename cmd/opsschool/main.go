// Command opsschool runs Ops School emulator scenarios.
package main

import (
	"os"

	"github.com/opsschool/emulator/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.DefaultEnv(), os.Args[1:]))
}
