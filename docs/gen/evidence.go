// Package gen projects measured verification and canonical examples into library documentation.
package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Evidence binds passing test results and exact statement counts to source content.
type Evidence struct {
	Source  string   `json:"source"`
	Tests   int      `json:"tests"`
	Covered int      `json:"covered,omitempty"`
	Total   int      `json:"total,omitempty"`
	Cases   []string `json:"cases,omitempty"`
}

// Fingerprint invalidates evidence when relevant runtime or integration sources change.
func Fingerprint(root string, integration bool) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || relative == "docs" || relative == "examples" || !integration && relative == "integration") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") || entry.Name() == "go.mod" || entry.Name() == "go.sum" {
			paths = append(paths, relative)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(path), len(data))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// LoadEvidence rejects stale reports rather than publishing unsupported verification claims.
func LoadEvidence(root, name string, integration bool) (Evidence, error) {
	var evidence Evidence
	data, err := os.ReadFile(filepath.Join(root, "docs/reports", name+".json"))
	if err != nil {
		return evidence, err
	}
	if err = json.Unmarshal(data, &evidence); err != nil {
		return evidence, err
	}
	fingerprint, err := Fingerprint(root, integration)
	if err != nil {
		return evidence, err
	}
	if evidence.Source != fingerprint || evidence.Tests == 0 {
		return evidence, fmt.Errorf("stale or empty %s evidence", name)
	}
	if !integration && (evidence.Total == 0 || evidence.Covered != evidence.Total) {
		return evidence, fmt.Errorf("unit coverage must cover every statement")
	}
	return evidence, nil
}
