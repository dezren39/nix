// Command genreq regenerates internal/conformance/requirements.json from
// docs/spec/requirements.md and internal/conformance/overrides/*.json.
//
//	go run ./internal/conformance/cmd/genreq
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dezren39/mcpx/internal/conformance"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "genreq:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := conformance.ModuleRoot(".")
	if err != nil {
		return err
	}
	b, err := conformance.Generate(root)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, conformance.CataloguePath), b, 0o644)
}
