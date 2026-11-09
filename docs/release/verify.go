package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// candidateEnvironment keeps checksum verification active even when the caller disabled it locally.
func candidateEnvironment(cache string) []string {
	return append(os.Environ(), "GOWORK=off", "GOMODCACHE="+cache, "GOPRIVATE=", "GONOPROXY=none", "GOSUMDB=sum.golang.org")
}

// releaseModules permits staged publication checks without replacing complete archive evidence.
func releaseModules(mode, selector string) ([]module, error) {
	if mode != "plan" && mode != "archive" && mode != "published" {
		return nil, fmt.Errorf("unknown release mode")
	}
	if selector == "all" {
		return modules, nil
	}
	if mode != "published" {
		return nil, fmt.Errorf("module selection is supported only for published verification")
	}
	for _, m := range modules {
		if m.Path != "" && selector == m.Path || selector == "." && m.Path == "" {
			return []module{m}, nil
		}
	}
	return nil, fmt.Errorf("unknown runtime module")
}

// remoteTagCommit reads the origin's advertised tag instead of trusting a local tag or cache.
func remoteTagCommit(root, tag string) (string, error) {
	ref := "refs/tags/" + tag
	data, err := command(root, nil, "git", "ls-remote", "--exit-code", "--tags", "origin", ref, ref+"^{}")
	if err != nil {
		return "", fmt.Errorf("remote tag %s: %w", tag, err)
	}
	return tagCommit(data, ref)
}

// tagCommit prefers the peeled commit of an annotated tag regardless of advertisement order.
func tagCommit(data []byte, ref string) (string, error) {
	var direct, peeled string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", fmt.Errorf("invalid remote tag advertisement")
		}
		switch fields[1] {
		case ref:
			direct = fields[0]
		case ref + "^{}":
			peeled = fields[0]
		}
	}
	if peeled != "" {
		return peeled, nil
	}
	if direct != "" {
		return direct, nil
	}
	return "", fmt.Errorf("remote tag was not advertised")
}

// verifyGraph rejects checkout resolution and requires each expected module at its exact version.
func verifyGraph(data []byte, root string, expected map[string]string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	found := make(map[string]bool, len(expected))
	for {
		var selected struct {
			Path, Version, Dir string
			Replace            any
		}
		err := decoder.Decode(&selected)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if selected.Replace != nil || selected.Dir == root || strings.HasPrefix(selected.Dir, root+string(filepath.Separator)) {
			return fmt.Errorf("consumer resolved a local replacement")
		}
		if want, ok := expected[selected.Path]; ok {
			if selected.Version != want {
				return fmt.Errorf("unexpected selected module version for %s", selected.Path)
			}
			found[selected.Path] = true
		}
	}
	if len(found) != len(expected) {
		return fmt.Errorf("consumer did not resolve every expected module")
	}
	return nil
}

// verifyComposition catches SDK dependency conflicts when all published capabilities share a consumer.
func verifyComposition(temporary string, environment []string, version, goVersion, root string) error {
	directory := filepath.Join(temporary, "consumer", "composition")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	var configuration strings.Builder
	fmt.Fprintf(&configuration, "module consumer\n\ngo %s\n\nrequire (\n", goVersion)
	expected := make(map[string]string, len(modules))
	for _, m := range modules {
		name := "github.com/goforj/secret"
		if m.Path != "" {
			name += "/" + m.Path
		}
		fmt.Fprintf(&configuration, "\t%s %s\n", name, version)
		expected[name] = version
	}
	configuration.WriteString(")\n")
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(configuration.String()), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "consumer_test.go"), []byte(compositionConsumer), 0600); err != nil {
		return err
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}, {"mod", "verify"}} {
		if _, err := command(directory, environment, "go", args...); err != nil {
			return fmt.Errorf("combined consumer: %w", err)
		}
	}
	data, err := command(directory, environment, "go", "list", "-m", "-json", "all")
	if err != nil {
		return err
	}
	if err := verifyGraph(data, root, expected); err != nil {
		return err
	}
	fmt.Printf("verified all %d runtime modules in one consumer\n", len(modules))
	return nil
}

// compositionConsumer compiles every provider together while exercising fixtures without credentials.
const compositionConsumer = `// Package consumer verifies the complete published secret composition.
package consumer
import (
 "context"
 "testing"
 "github.com/goforj/secret"
 "github.com/goforj/secret/secrettest"
 "github.com/goforj/secret/driver/awssecret"
 "github.com/goforj/secret/driver/azuresecret"
 "github.com/goforj/secret/driver/filesecret"
 "github.com/goforj/secret/driver/gcpsecret"
 "github.com/goforj/secret/driver/vaultsecret"
)
var (
 _ secret.Store = (*awssecret.Store)(nil)
 _ secret.Store = (*azuresecret.Store)(nil)
 _ secret.Reader = (*filesecret.Reader)(nil)
 _ secret.Store = (*gcpsecret.Store)(nil)
 _ secret.Store = (*vaultsecret.Store)(nil)
)
// TestComposition checks interoperability without provider credentials.
func TestComposition(t *testing.T) {
 store := secrettest.New(t, map[string]string{"KEY": "public-fixture"})
 var reader secret.Reader = store
 if value, err := reader.Get(context.Background(), "KEY"); err != nil || value != "public-fixture" {
  t.Fatal("combined consumer contract failed")
 }
}
`
