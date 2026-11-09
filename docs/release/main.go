// Command release plans independent tags and verifies exact committed module archives.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// module lists only independently published runtime modules.
type module struct{ Path, Package string }

// archiveEvidence retains candidate checksums independently of the private preview cache.
type archiveEvidence struct {
	Commit  string               `json:"commit"`
	Version string               `json:"version"`
	Modules map[string]checksums `json:"modules"`
}

// checksums identify Go's normalized module archive and module declaration.
type checksums struct {
	Sum      string `json:"Sum"`
	GoModSum string `json:"GoModSum"`
}

var modules = []module{{"", "secret"}, {"secrettest", "secrettest"}, {"driver/awssecret", "awssecret"}, {"driver/azuresecret", "azuresecret"}, {"driver/filesecret", "filesecret"}, {"driver/gcpsecret", "gcpsecret"}, {"driver/vaultsecret", "vaultsecret"}}

// main leaves publication explicit; archive validation never creates or pushes tags.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run validates consumers through a private preview proxy with no local replacements.
func run() error {
	mode := flag.String("mode", "plan", "plan, archive, or published")
	version := flag.String("version", "v0.0.0", "exact candidate or published version")
	flag.Parse()
	if !strings.HasPrefix(*version, "v") || strings.ContainsAny(*version, "/\\\n\r") {
		return fmt.Errorf("invalid version")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	if *mode == "plan" {
		for _, m := range modules {
			prefix := m.Path
			if prefix != "" {
				prefix += "/"
			}
			fmt.Println(prefix + *version)
		}
		return nil
	}
	if *mode != "archive" && *mode != "published" {
		return fmt.Errorf("unknown release mode")
	}
	if *mode == "archive" {
		status, err := command(root, nil, "git", "status", "--porcelain")
		if err != nil {
			return err
		}
		if len(status) != 0 {
			return fmt.Errorf("commit the complete candidate before archive verification")
		}
	}
	temporary, err := os.MkdirTemp("", "secret-release-")
	if err != nil {
		return err
	}

	cache := filepath.Join(temporary, "cache")
	proxy := filepath.Join(temporary, "proxy")
	environment := append(os.Environ(), "GOWORK=off", "GOMODCACHE="+cache, "GOPRIVATE=", "GONOPROXY=none")
	defer func() { command(temporary, environment, "go", "clean", "-modcache"); os.RemoveAll(temporary) }()
	commit, err := command(root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	evidence := archiveEvidence{Commit: strings.TrimSpace(string(commit)), Version: *version, Modules: map[string]checksums{}}
	reportPath := filepath.Join(root, ".artifacts", "release-archive-"+*version+".json")
	var candidate archiveEvidence
	if *mode == "published" {
		data, err := os.ReadFile(reportPath)
		if err != nil {
			return fmt.Errorf("candidate archive checksums are required before published verification")
		}
		if err := json.Unmarshal(data, &candidate); err != nil {
			return err
		}
		if candidate.Version != *version {
			return fmt.Errorf("candidate version mismatch")
		}
	}

	if *mode == "archive" {
		for _, m := range modules {
			if err := archive(root, proxy, m, *version); err != nil {
				return err
			}
		}
		environment = append(environment, "GOPROXY=file://"+filepath.ToSlash(proxy)+",https://proxy.golang.org", "GONOSUMDB=github.com/goforj/secret*")
	} else {
		environment = append(environment, "GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=none")
	}
	for _, m := range modules {
		name := "github.com/goforj/secret"
		if m.Path != "" {
			name += "/" + m.Path
		}
		directory := filepath.Join(temporary, "consumer", m.Package)
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
		configuration, err := os.ReadFile(filepath.Join(root, m.Path, "go.mod"))
		if err != nil {
			return err
		}
		goVersion := ""
		for _, line := range strings.Split(string(configuration), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "go" {
				goVersion = fields[1]
			}
		}
		if goVersion == "" {
			return fmt.Errorf("missing module Go minimum")
		}

		mod := fmt.Sprintf("module consumer\n\ngo %s\n\nrequire %s %s\n", goVersion, name, *version)
		if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(mod), 0600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(directory, "consumer_test.go"), []byte(consumer(m)), 0600); err != nil {
			return err
		}
		for _, args := range [][]string{{"mod", "tidy"}, {"test", "./..."}, {"mod", "verify"}} {
			if _, err := command(directory, environment, "go", args...); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		minimumEnvironment := append(append([]string{}, environment...), "GOTOOLCHAIN=go"+goVersion)
		if _, err := command(directory, minimumEnvironment, "go", "test", "./..."); err != nil {
			return fmt.Errorf("%s minimum Go %s: %w", name, goVersion, err)
		}
		data, err := command(directory, environment, "go", "list", "-m", "-json", "all")
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		found := false
		foundRoot := m.Path == ""
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
			if selected.Path == name {
				found = selected.Version == *version
			}
			if selected.Path == "github.com/goforj/secret" {
				foundRoot = selected.Version == *version
			}
		}
		if !found || !foundRoot {
			return fmt.Errorf("unexpected selected module version for %s", name)
		}
		download, err := command(directory, environment, "go", "mod", "download", "-json", name+"@"+*version)
		if err != nil {
			return err
		}
		var sums checksums
		if err := json.Unmarshal(download, &sums); err != nil {
			return err
		}
		if sums.Sum == "" || sums.GoModSum == "" {
			return fmt.Errorf("missing checksums for %s", name)
		}
		evidence.Modules[name] = sums
		if *mode == "published" {
			if candidate.Modules[name] != sums {
				return fmt.Errorf("published archive differs from tested candidate for %s", name)
			}
			tag := *version
			if m.Path != "" {
				tag = m.Path + "/" + tag
			}
			target, err := command(root, nil, "git", "rev-parse", tag+"^{commit}")
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(target)) != candidate.Commit {
				return fmt.Errorf("tag does not target the tested candidate")
			}
		}
		fmt.Printf("verified %s@%s (%s)\n", name, *version, *mode)
	}
	if *mode == "archive" {
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(reportPath, append(data, '\n'), 0644); err != nil {
			return err
		}
	}
	return nil
}

// command confines child authentication, proxy, and module settings to the verification process.
func command(directory string, environment []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = directory
	if environment != nil {
		cmd.Env = environment
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %s", name, output)
	}
	return output, nil
}

// archive uses committed bytes and excludes nested modules, matching Go's module boundaries.
func archive(root, proxy string, m module, version string) error {
	name := "github.com/goforj/secret"
	if m.Path != "" {
		name += "/" + m.Path
	}
	directory := filepath.Join(proxy, filepath.FromSlash(name), "@v")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	listing, err := command(root, nil, "git", "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSpace(string(listing)), "\n")
	prefix := m.Path
	if prefix != "" {
		prefix += "/"
	}
	file, err := os.Create(filepath.Join(directory, version+".zip"))
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for _, path := range paths {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		relative := strings.TrimPrefix(path, prefix)
		nested := false
		parent := filepath.Dir(relative)
		for parent != "." {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(prefix), parent, "go.mod")); err == nil {
				nested = true
				break
			}
			parent = filepath.Dir(parent)
		}
		if nested {
			continue
		}
		data, err := command(root, nil, "git", "show", "HEAD:"+path)
		if err != nil {
			return err
		}
		entry, err := writer.Create(name + "@" + version + "/" + relative)
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
	}
	if m.Path != "" {
		data, err := command(root, nil, "git", "show", "HEAD:LICENSE")
		if err != nil {
			return err
		}
		entry, err := writer.Create(name + "@" + version + "/LICENSE")
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	modPath := prefix + "go.mod"
	data, err := command(root, nil, "git", "show", "HEAD:"+modPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, version+".mod"), data, 0644); err != nil {
		return err
	}
	stamp, err := command(root, nil, "git", "show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return err
	}
	timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(string(stamp)))
	if err != nil {
		return err
	}
	info, err := json.Marshal(struct {
		Version string
		Time    time.Time
	}{version, timestamp})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, version+".info"), info, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "list"), []byte(version+"\n"), 0644)
}

// consumer exercises exported capabilities outside the checkout without provider credentials.
func consumer(m module) string {
	if m.Package == "secret" {
		return `package consumer
import("context";"testing";"github.com/goforj/secret")
func TestConsumer(t *testing.T){reader:=secret.Func(func(context.Context,string)(string,error){return "public-fixture",nil});if value,err:=reader.Get(context.Background(),"KEY");err!=nil||value!="public-fixture"{t.Fatal("reader contract failed")}}
`
	}
	if m.Package == "secrettest" {
		return `package consumer
import("context";"testing";"github.com/goforj/secret/secrettest")
func TestConsumer(t *testing.T){store:=secrettest.New(t,map[string]string{"KEY":"public-fixture"});if err:=store.Set(context.Background(),"KEY","");err!=nil{t.Fatal(err)};if value,err:=store.Get(context.Background(),"KEY");err!=nil||value!=""{t.Fatal("store contract failed")}}
`
	}
	args := "nil,nil"
	if m.Package == "gcpsecret" {
		args = `nil,"projects/public",nil`
	}
	capability := "Store"
	if m.Package == "filesecret" {
		capability = "Reader"
	}
	return fmt.Sprintf(`package consumer
import("errors";"testing";"github.com/goforj/secret";"github.com/goforj/secret/%s")
var _ secret.%s = (*%s.%s)(nil)
func TestConsumer(t *testing.T){if _,err:=%s.New(%s);!errors.Is(err,secret.ErrInvalid){t.Fatal("constructor contract failed")}}
`, m.Path, capability, m.Package, capability, m.Package, args)
}
