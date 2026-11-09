// Command report records passing test events and exact library coverage counts.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/goforj/secret/docs/gen"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// main binds test evidence to source instead of trusting hand-maintained badge counts.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run validates complete result sets before replacing the tracked report.
func run() error {
	mode := flag.String("mode", "unit", "unit or integration")
	flag.Parse()
	if *mode != "unit" && *mode != "integration" {
		return fmt.Errorf("unknown evidence mode")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	fingerprint, err := gen.Fingerprint(root, *mode == "integration")
	if err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(root, ".artifacts", *mode+"-*.json"))
	if err != nil {
		return err
	}
	evidence := gen.Evidence{Source: fingerprint}
	cases := map[string]bool{}
	packages := map[string]bool{}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(file)
		for {
			var event struct{ Action, Package, Test string }
			err := decoder.Decode(&event)
			if err == io.EOF {
				break
			}
			if err != nil {
				file.Close()
				return err
			}
			if event.Action == "fail" {
				file.Close()
				return fmt.Errorf("test run failed")
			}
			if event.Action == "pass" {
				if event.Test != "" {
					evidence.Tests++
					cases[event.Test] = true
				} else {
					packages[event.Package] = true
				}
			}
		}
		file.Close()
	}
	for test := range cases {
		evidence.Cases = append(evidence.Cases, test)
	}
	sort.Strings(evidence.Cases)
	if *mode == "integration" {
		for _, test := range []string{"TestAWS", "TestGoogle", "TestGoogleREST", "TestAzure", "TestVault"} {
			if !cases[test] {
				return fmt.Errorf("missing integration result: %s", test)
			}
		}
	} else {
		for _, module := range []string{"", "/secrettest", "/driver/awssecret", "/driver/azuresecret", "/driver/filesecret", "/driver/gcpsecret", "/driver/vaultsecret"} {
			if !packages["github.com/goforj/secret"+module] {
				return fmt.Errorf("missing unit module %s", module)
			}
		}
		profiles, err := filepath.Glob(filepath.Join(root, ".artifacts", "unit-*.cover"))
		if err != nil {
			return err
		}
		var merged strings.Builder
		merged.WriteString("mode: atomic\n")
		for _, path := range profiles {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) != 3 {
					continue
				}
				merged.WriteString(scanner.Text() + "\n")
				statements, err := strconv.Atoi(fields[1])
				if err != nil {
					file.Close()
					return err
				}
				count, err := strconv.Atoi(fields[2])
				if err != nil {
					file.Close()
					return err
				}
				evidence.Total += statements
				if count > 0 {
					evidence.Covered += statements
				}
			}
			err = scanner.Err()
			file.Close()
			if err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(root, ".artifacts/unit-coverage.out"), []byte(merged.String()), 0600); err != nil {
			return err
		}
		if evidence.Total == 0 || evidence.Total != evidence.Covered {
			return fmt.Errorf("unit statements: %d/%d", evidence.Covered, evidence.Total)
		}
	}
	if evidence.Tests == 0 {
		return fmt.Errorf("no executed tests")
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, "docs/reports", *mode+".json"), data, 0644)
}
