// flate — local validator for Flux GitOps repositories.
package main

import (
	"os"

	"github.com/home-operations/flate/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
