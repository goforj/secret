// Command modules verifies that repository commands inventory every independent Go module.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// main rejects stale module inventories before checks silently miss a driver.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run compares declared module paths with filesystem module boundaries.
func run() error {
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	var actual []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		if entry.Name() == "go.mod" {
			relative, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			actual = append(actual, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "scripts/module-manifest.txt"))
	if err != nil {
		return err
	}
	expected := strings.Fields(string(data))
	sort.Strings(expected)
	sort.Strings(actual)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		return fmt.Errorf("module manifest differs from discovered modules")
	}
	return nil
}
