package examples

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPrograms executes every canonical program and checks the nearby output expectations.
func TestPrograms(t *testing.T) {
	paths, err := filepath.Glob("*/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no runnable examples")
	}
	for _, path := range paths {
		t.Run(filepath.Dir(path), func(t *testing.T) {
			expected := expectedOutput(t, path)
			output, stderr, err := runProgram(t, buildProgram(t, path))
			if err != nil {
				t.Fatalf("example failed: %v\n%s", err, stderr)
			}
			if string(output) != expected {
				t.Fatalf("output mismatch:\n%s", output)
			}
		})
	}
}

// expectedOutput keeps every executable's output contract in its authoritative source.
func expectedOutput(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, line := range strings.Split(string(source), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "// Output: ") {
			expected = append(expected, strings.TrimPrefix(line, "// Output: "))
		}
	}
	if len(expected) == 0 {
		t.Fatal("example lacks expected output")
	}
	return strings.Join(expected, "\n") + "\n"
}

// buildProgram preserves library coverage and race instrumentation in configured examples too.
func buildProgram(t *testing.T, path string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "example")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	args := []string{"build", "-cover", "-coverpkg=github.com/goforj/secret/...", "-o", binary, "./" + filepath.Dir(path)}
	if raceEnabled() {
		args = append(args[:1], append([]string{"-race"}, args[1:]...)...)
	}
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}
	return binary
}

// runProgram captures actual process results and retains child coverage even after a rejected request.
func runProgram(t *testing.T, binary string, args ...string) ([]byte, string, error) {
	t.Helper()
	coverage := t.TempDir()
	run := exec.Command(binary, args...)
	run.Env = append(os.Environ(), "GOCOVERDIR="+coverage)
	var stderr bytes.Buffer
	run.Stderr = &stderr
	output, runErr := run.Output()
	if destination := os.Getenv("SECRET_EXAMPLE_COVERDIR"); destination != "" {
		entries, err := os.ReadDir(coverage)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(coverage, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, entry.Name()), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return output, stderr.String(), runErr
}

// TestApplicationOutput executes the canonical test snippet and checks its displayed output.
func TestApplicationOutput(t *testing.T) {
	source, err := os.ReadFile("testing_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, line := range strings.Split(string(source), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "// Output: ") {
			expected = append(expected, strings.TrimPrefix(line, "// Output: "))
		}
	}
	if len(expected) == 0 {
		t.Fatal("test snippet lacks expected output")
	}
	args := []string{"test", "-json", "-run", "^TestStore$", "-count=1", "."}
	if raceEnabled() {
		args = append(args[:1], append([]string{"-race"}, args[1:]...)...)
	}
	command := exec.Command("go", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("canonical test failed: %v\n%s", err, output)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var actual []string
	for {
		var event struct{ Action, Test, Output string }
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Test == "TestStore" && event.Action == "output" {
			line := strings.TrimSuffix(event.Output, "\n")
			if !strings.HasPrefix(line, "=== RUN") && !strings.HasPrefix(line, "--- PASS") {
				actual = append(actual, line)
			}
		}
	}
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatal("canonical test output differs from its documentation")
	}
}
