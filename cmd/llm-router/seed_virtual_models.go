package main

import (
	"fmt"
	"os"
)

func runSeedVirtualModels(args []string) {
	fmt.Fprintln(os.Stderr, "deprecated: virtual models are managed via the API")
	os.Exit(1)
}
