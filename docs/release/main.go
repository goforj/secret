// Command release plans independent tags and verifies exact committed module archives.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	releaseVersion := flag.String("version", "v0.1.0", "exact candidate or published version")
	selector := flag.String("module", "all", "published module directory, . for the root, or all")
	flag.Parse()
	if !validVersion(*releaseVersion) {
		return fmt.Errorf("version must be a canonical v0 or v1 semantic version without build metadata")
	}
	selected, err := releaseModules(*mode, *selector)
	if err != nil {
		return err
	}
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	if *mode == "plan" {
		for _, m := range selected {
			prefix := m.Path
			if prefix != "" {
				prefix += "/"
			}
			fmt.Println(prefix + *releaseVersion)
		}
		return nil
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
	environment := candidateEnvironment(cache)
	defer func() { command(temporary, environment, "go", "clean", "-modcache"); os.RemoveAll(temporary) }()
	commit, err := command(root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	sourceCommit := strings.TrimSpace(string(commit))
	evidence := archiveEvidence{Commit: sourceCommit, Version: *releaseVersion, Modules: map[string]checksums{}}
	reportPath := filepath.Join(root, ".artifacts", "release-archive-"+*releaseVersion+".json")
	var candidate archiveEvidence
	if *mode == "published" {
		data, err := os.ReadFile(reportPath)
		if err != nil {
			return fmt.Errorf("candidate archive checksums are required before published verification")
		}
		if err := json.Unmarshal(data, &candidate); err != nil {
			return err
		}
		if candidate.Commit == "" || candidate.Version != *releaseVersion {
			return fmt.Errorf("candidate commit or version mismatch")
		}
		sourceCommit = candidate.Commit
	}

	if *mode == "archive" {
		for _, m := range selected {
			if err := archive(root, proxy, m, *releaseVersion, sourceCommit); err != nil {
				return err
			}
		}
		environment = append(environment, "GOPROXY=file://"+filepath.ToSlash(proxy)+",https://proxy.golang.org", "GONOSUMDB=github.com/goforj/secret")
	} else {
		environment = append(environment, "GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=none")
	}
	compositionGo := "1.0"
	for _, m := range selected {
		name := "github.com/goforj/secret"
		if m.Path != "" {
			name += "/" + m.Path
		}
		directory := filepath.Join(temporary, "consumer", m.Package)
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
		configuration, err := command(root, nil, "git", "show", sourceCommit+":"+filepath.ToSlash(filepath.Join(m.Path, "go.mod")))
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

		if version.Compare("go"+goVersion, "go"+compositionGo) > 0 {
			compositionGo = goVersion
		}
		mod := fmt.Sprintf("module consumer\n\ngo %s\n\nrequire %s %s\n", goVersion, name, *releaseVersion)
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
		expected := map[string]string{name: *releaseVersion, "github.com/goforj/secret": *releaseVersion}
		if err := verifyGraph(data, root, expected); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		download, err := command(directory, environment, "go", "mod", "download", "-json", name+"@"+*releaseVersion)
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
			tag := *releaseVersion
			if m.Path != "" {
				tag = m.Path + "/" + tag
			}
			target, err := remoteTagCommit(root, tag)
			if err != nil {
				return err
			}
			if target != candidate.Commit {
				return fmt.Errorf("remote tag %s does not target the tested candidate", tag)
			}
		}
		fmt.Printf("verified %s@%s (%s)\n", name, *releaseVersion, *mode)
	}
	if len(selected) == len(modules) {
		if err := verifyComposition(temporary, environment, *releaseVersion, compositionGo, root); err != nil {
			return err
		}
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

// validVersion prevents unusable tags and incompatible major versions for the current module paths.
func validVersion(version string) bool {
	if !regexp.MustCompile(`^v[01]\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`).MatchString(version) {
		return false
	}
	_, prerelease, _ := strings.Cut(version, "-")
	for _, part := range strings.Split(prerelease, ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return false
		}
	}
	return true
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
func archive(root, proxy string, m module, version, commit string) error {
	name := "github.com/goforj/secret"
	if m.Path != "" {
		name += "/" + m.Path
	}
	directory := filepath.Join(proxy, filepath.FromSlash(name), "@v")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	listing, err := command(root, nil, "git", "ls-tree", "-r", "-z", "--name-only", commit)
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSuffix(string(listing), "\x00"), "\x00")
	committed := make(map[string]bool, len(paths))
	for _, path := range paths {
		committed[path] = true
	}
	prefix := m.Path
	if prefix != "" {
		prefix += "/"
	}
	file, err := os.Create(filepath.Join(directory, version+".zip"))
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for _, path := range paths {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		relative := strings.TrimPrefix(path, prefix)
		nested := false
		parent := filepath.Dir(relative)
		for parent != "." {
			if committed[prefix+filepath.ToSlash(filepath.Join(parent, "go.mod"))] {
				nested = true
				break
			}
			parent = filepath.Dir(parent)
		}
		if nested {
			continue
		}
		data, err := command(root, nil, "git", "show", commit+":"+path)
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
		data, err := command(root, nil, "git", "show", commit+":LICENSE")
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
	data, err := command(root, nil, "git", "show", commit+":"+modPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, version+".mod"), data, 0644); err != nil {
		return err
	}
	stamp, err := command(root, nil, "git", "show", "-s", "--format=%cI", commit)
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
	if m.Package == "filesecret" {
		return `package consumer
import("bytes";"context";"errors";"os";"testing";"github.com/goforj/secret";"github.com/goforj/secret/driver/filesecret")
var _ secret.Reader = (*filesecret.Reader)(nil)
var _ secret.Store = (*filesecret.Store)(nil)
// TestConsumer verifies encrypted persistence through the published API.
func TestConsumer(t *testing.T){
 if _,err:=filesecret.New(nil,nil);!errors.Is(err,secret.ErrInvalid){t.Fatal("reader constructor contract failed")}
 root,err:=os.OpenRoot(t.TempDir());if err!=nil{t.Fatal(err)};defer root.Close()
 key:=bytes.Repeat([]byte{42},32)
 store,err:=filesecret.NewStore(root,key,map[string]string{"KEY":"key"});if err!=nil{t.Fatal(err)}
 ctx:=context.Background()
 if err:=store.Set(ctx,"KEY","public-fixture");err!=nil{t.Fatal(err)}
 data,err:=root.ReadFile("key");if err!=nil||bytes.Contains(data,[]byte("public-fixture")){t.Fatal("plaintext storage")}
 next:=bytes.Repeat([]byte{43},32)
 rotating,err:=filesecret.NewStoreWithKeys(root,next,map[string]string{"KEY":"key"},key);if err!=nil{t.Fatal(err)}
 if value,err:=rotating.Get(ctx,"KEY");err!=nil||value!="public-fixture"{t.Fatal("previous-key read failed")}
 if err:=rotating.Rotate(ctx);err!=nil{t.Fatal(err)}
 reopened,err:=filesecret.NewStore(root,next,map[string]string{"KEY":"key"});if err!=nil{t.Fatal(err)}
 if value,err:=reopened.Get(ctx,"KEY");err!=nil||value!="public-fixture"{t.Fatal("encrypted persistence failed")}
 if err:=reopened.Delete(ctx,"KEY");err!=nil{t.Fatal(err)}
 if _,err:=store.Get(ctx,"KEY");!errors.Is(err,secret.ErrNotFound){t.Fatal("delete contract failed")}
}
`
	}
	args := "nil,nil"
	if m.Package == "gcpsecret" {
		args = `nil,"projects/public",nil`
	}
	capability := "Store"
	return fmt.Sprintf(`package consumer
import("errors";"testing";"github.com/goforj/secret";"github.com/goforj/secret/%s")
var _ secret.%s = (*%s.%s)(nil)
func TestConsumer(t *testing.T){if _,err:=%s.New(%s);!errors.Is(err,secret.ErrInvalid){t.Fatal("constructor contract failed")}}
`, m.Path, capability, m.Package, capability, m.Package, args)
}
