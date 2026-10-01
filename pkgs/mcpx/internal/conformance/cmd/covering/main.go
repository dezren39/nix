// Command covering runs exactly the tests the conformance matrix names, one
// `go test -run` per package, and nothing else.
//
//	go run ./internal/conformance/cmd/covering [-n] [go test flags...]
//
// -n prints the commands instead of running them. The selection is at the
// granularity -run allows: a top-level test and its named subtests.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dezren39/mcpx/internal/conformance"
)

func main() {
	args := os.Args[1:]
	dry := len(args) > 0 && args[0] == "-n"
	if dry {
		args = args[1:]
	}
	root, err := conformance.ModuleRoot(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	failed := false
	for _, pkg := range conformance.RunPatterns() {
		cmd := append([]string{"test", "./internal/" + pkg.Pkg, "-run", pkg.Pattern}, args...)
		if dry {
			fmt.Println("go " + strings.Join(quote(cmd), " "))
			continue
		}
		c := exec.Command("go", cmd...)
		c.Dir, c.Stdout, c.Stderr = root, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func quote(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		if strings.ContainsAny(s, "^$|()/ ") {
			s = "'" + s + "'"
		}
		out[i] = s
	}
	return out
}
