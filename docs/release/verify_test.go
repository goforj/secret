package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCandidateEnvironment verifies process overrides with the actual Go command's environment handling.
func TestCandidateEnvironment(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "invalid-workspace")
	t.Setenv("GOMODCACHE", "invalid-cache")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GONOPROXY", "*")
	t.Setenv("GOSUMDB", "off")
	cache := t.TempDir()
	data, err := command(t.TempDir(), candidateEnvironment(cache), "go", "env", "GOWORK", "GOMODCACHE", "GOPRIVATE", "GONOPROXY", "GOSUMDB")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "off\n"+cache+"\n\nnone\nsum.golang.org\n" {
		t.Fatal("caller environment weakened release verification")
	}
}

// TestReleaseModules preserves complete archive evidence while allowing staged publication checks.
func TestReleaseModules(t *testing.T) {
	for _, mode := range []string{"plan", "archive", "published"} {
		selected, err := releaseModules(mode, "all")
		if err != nil || len(selected) != len(modules) {
			t.Fatal("complete module inventory was not selected")
		}
	}
	for _, selector := range []string{".", "secrettest", "driver/awssecret"} {
		selected, err := releaseModules("published", selector)
		if err != nil || len(selected) != 1 {
			t.Fatal("published module could not be selected")
		}
		if selector == "." && selected[0].Path != "" || selector != "." && selected[0].Path != selector {
			t.Fatal("wrong published module selected")
		}
	}
	for _, request := range [][2]string{{"", "all"}, {"unknown", "all"}, {"archive", "."}, {"plan", "secrettest"}, {"published", ""}, {"published", "docs"}, {"published", "../secret"}} {
		if _, err := releaseModules(request[0], request[1]); err == nil {
			t.Fatalf("invalid release selection accepted: %v", request)
		}
	}
}

// TestTagCommit recognizes both Git tag formats and refuses missing or malformed advertisements.
func TestTagCommit(t *testing.T) {
	ref := "refs/tags/v0.1.0"
	for _, output := range []string{"commit " + ref, "tag " + ref + "\ncommit " + ref + "^{}", "commit " + ref + "^{}\ntag " + ref} {
		commit, err := tagCommit([]byte(output), ref)
		if err != nil || commit != "commit" {
			t.Fatal("remote tag resolved to the wrong commit")
		}
	}
	for _, output := range []string{"", "invalid", "a b c", "commit refs/tags/other"} {
		if _, err := tagCommit([]byte(output), ref); err == nil {
			t.Fatal("invalid remote tag advertisement accepted")
		}
	}
}

// TestVerifyGraph requires exact candidates and rejects every form of local replacement.
func TestVerifyGraph(t *testing.T) {
	root := filepath.Join(t.TempDir(), "checkout")
	expected := map[string]string{"github.com/goforj/secret": "v0.1.0"}
	good := `{"Path":"consumer","Dir":"/outside"} {"Path":"github.com/goforj/secret","Version":"v0.1.0","Dir":"/module-cache/secret"}`
	if err := verifyGraph([]byte(good), root, expected); err != nil {
		t.Fatal(err)
	}
	for _, value := range []map[string]any{
		{"Path": "github.com/goforj/secret", "Version": "v0.1.0", "Dir": root},
		{"Path": "github.com/goforj/secret", "Version": "v0.1.0", "Dir": filepath.Join(root, "driver")},
		{"Path": "github.com/goforj/secret", "Version": "v0.1.0", "Replace": map[string]string{"Dir": "/outside"}},
		{"Path": "github.com/goforj/secret", "Version": "v0.0.0"},
		{"Path": "unrelated", "Version": "v0.1.0"},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyGraph(data, root, expected); err == nil {
			t.Fatal("unexpected module graph accepted")
		}
	}
	for _, data := range []string{"", "{"} {
		if err := verifyGraph([]byte(data), root, expected); err == nil {
			t.Fatal("incomplete module graph accepted")
		}
	}
}

// fixtureRepository isolates release tests from the owner's repository and Git identity settings.
func fixtureRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	fixtureGit(t, root, "init", "-b", "main")
	for name, data := range map[string]string{
		"go.mod":                  "module github.com/goforj/secret\n\ngo 1.24.4\n",
		"secret.go":               "package secret\n",
		"LICENSE":                 "public-license-fixture\n",
		"driver/awssecret/go.mod": "module github.com/goforj/secret/driver/awssecret\n\ngo 1.24.4\n",
		"driver/awssecret/aws.go": "package awssecret\n",
		"extra/value.txt":         "public-committed-fixture\n",
		"extra/日本語.txt":           "public-unicode-fixture\n",
	} {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "test: create release fixture")
	return root, fixtureGit(t, root, "rev-parse", "HEAD")
}

// fixtureGit applies identity only to the test subprocess and leaves persistent Git settings untouched.
func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	options := []string{"-c", "user.name=Chris Miles", "-c", "user.email=chris.miles.e@gmail.com"}
	data, err := command(root, nil, "git", append(options, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// TestArchiveSnapshot requires immutable source, module boundaries, declarations, and inherited licenses.
func TestArchiveSnapshot(t *testing.T) {
	root, commit := fixtureRepository(t)
	if err := os.WriteFile(filepath.Join(root, "secret.go"), []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, root, "add", "secret.go")
	fixtureGit(t, root, "commit", "-m", "test: advance fixture HEAD")
	if err := os.WriteFile(filepath.Join(root, "extra", "go.mod"), []byte("module uncommitted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, m := range []module{{"", "secret"}, {"driver/awssecret", "awssecret"}} {
		proxy := t.TempDir()
		if err := archive(root, proxy, m, "v0.1.0", commit); err != nil {
			t.Fatal(err)
		}
		name := "github.com/goforj/secret"
		if m.Path != "" {
			name += "/" + m.Path
		}
		directory := filepath.Join(proxy, filepath.FromSlash(name), "@v")
		reader, err := zip.OpenReader(filepath.Join(directory, "v0.1.0.zip"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { reader.Close() })
		files := make(map[string]string)
		for _, file := range reader.File {
			body, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(body)
			body.Close()
			if err != nil {
				t.Fatal(err)
			}
			files[strings.TrimPrefix(file.Name, name+"@v0.1.0/")] = string(data)
		}
		if files["LICENSE"] != "public-license-fixture\n" {
			t.Fatal("module license missing")
		}
		if m.Path == "" {
			if files["secret.go"] != "package secret\n" || files["extra/value.txt"] != "public-committed-fixture\n" || files["extra/日本語.txt"] != "public-unicode-fixture\n" {
				t.Fatal("archive followed HEAD or an uncommitted module boundary")
			}
			if _, ok := files["driver/awssecret/aws.go"]; ok {
				t.Fatal("nested module leaked into root archive")
			}
		} else if files["aws.go"] != "package awssecret\n" {
			t.Fatal("nested module source missing")
		}
		data, err := os.ReadFile(filepath.Join(directory, "v0.1.0.mod"))
		if err != nil || string(data) != files["go.mod"] {
			t.Fatal("archive and proxy module declarations differ")
		}
	}
	if err := archive(root, t.TempDir(), module{}, "v0.1.0", "missing-commit"); err == nil {
		t.Fatal("missing candidate commit accepted")
	}
}

// TestRemoteTagCommit refuses to mistake a locally moved tag for the origin's published commit.
func TestRemoteTagCommit(t *testing.T) {
	root, first := fixtureRepository(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	fixtureGit(t, root, "init", "--bare", remote)
	fixtureGit(t, root, "remote", "add", "origin", remote)
	fixtureGit(t, root, "tag", "v0.1.0")
	fixtureGit(t, root, "tag", "-a", "v0.2.0", "-m", "test: annotated tag")
	fixtureGit(t, root, "push", "origin", "refs/tags/v0.1.0", "refs/tags/v0.2.0")
	fixtureGit(t, root, "commit", "--allow-empty", "-m", "test: advance local candidate")
	fixtureGit(t, root, "tag", "-f", "v0.1.0")
	for _, tag := range []string{"v0.1.0", "v0.2.0"} {
		got, err := remoteTagCommit(root, tag)
		if err != nil || got != first {
			t.Fatal("remote verification trusted a local tag or an annotation object")
		}
	}
	if _, err := remoteTagCommit(root, "v0.3.0"); err == nil {
		t.Fatal("unpublished remote tag accepted")
	}
}
