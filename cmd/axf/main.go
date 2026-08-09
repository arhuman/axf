// Command axf validates AXF (Alter eXtensible Format) v0 documents.
package main

import (
	"os"

	"github.com/arhuman/axf/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
