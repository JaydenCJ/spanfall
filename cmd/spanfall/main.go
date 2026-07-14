// Command spanfall renders OTLP and Jaeger trace files as terminal
// waterfalls with critical-path highlighting — no backend required.
package main

import (
	"os"

	"github.com/JaydenCJ/spanfall/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
