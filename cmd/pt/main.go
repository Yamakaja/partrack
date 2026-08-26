// Command pt is the partrack command-line client.
package main

import (
	"os"

	"github.com/Yamakaja/partrack/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
