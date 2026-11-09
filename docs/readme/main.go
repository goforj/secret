// Command readme generates the README and SVG assets from canonical examples and measured evidence.
package main

import (
	"fmt"
	"github.com/goforj/secret/docs/gen"
	"os"
	"path/filepath"
)

// main expects execution from the docs module, matching the repository's Make target.
func main() {
	root, err := filepath.Abs("..")
	if err == nil {
		err = gen.Generate(root)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
