// fluxrr — local validator for Flux GitOps repositories.
package main

import (
	"os"

	"github.com/buroa/fluxrr/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
