package filesecret

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFiles exercises real descriptor reads, binding copies, exact bytes, and path confinement.
func TestFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	for name, value := range map[string]string{"key": "\x00\xff\n", "empty": "", "large": strings.Repeat("x", MaxFileBytes+1), "boundary": strings.Repeat("x", MaxFileBytes)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	bindings := map[string]string{"KEY": "key", "EMPTY": "empty", "LARGE": "large", "BOUNDARY": "boundary", "MISSING": "missing", "DIR": "directory"}
	os.Mkdir(filepath.Join(dir, "directory"), 0700)
	r, err := New(root, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings["KEY"] = "missing"
	for name, want := range map[string]string{"KEY": "\x00\xff\n", "EMPTY": "", "BOUNDARY": strings.Repeat("x", MaxFileBytes)} {
		if value, err := r.Get(ctx, name); err != nil || value != want {
			t.Fatal("file bytes changed")
		}
	}
	for name, want := range map[string]error{"LARGE": secret.ErrUnavailable, "DIR": secret.ErrUnavailable, "MISSING": secret.ErrNotFound, "UNKNOWN": secret.ErrNotFound, "invalid": secret.ErrInvalid} {
		if value, err := r.Get(ctx, name); value != "" || !errors.Is(err, want) {
			t.Fatal("wrong read failure", err)
		}
	}
	if _, err := r.Get(nil, "KEY"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	root.Close()
	if _, err := r.Get(ctx, "KEY"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal(err)
	}
}

// TestConstructor rejects traversal syntax without performing filesystem I/O.
func TestConstructor(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := New(nil, map[string]string{"KEY": "key"}); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := New(root, nil); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/key", "../key", "a/../key", "a/./key", "a//key", "a/", "a\\b", "a\x00b", "C:foo", strings.Repeat("x", 4097)} {
		if _, err := New(root, map[string]string{"KEY": path}); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid locator accepted")
		}
	}
	if _, err := New(root, map[string]string{"bad": "key"}); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
}

// brokenDescriptor exposes deterministic operating-system failure paths without path races.
type brokenDescriptor struct {
	info               os.FileInfo
	statErr, errorRead error
}

// Stat simulates a failed fstat or a regular file that will fail during reading.
func (d brokenDescriptor) Stat() (os.FileInfo, error) { return d.info, d.statErr }

// Read simulates a descriptor read failure while preserving caller-data redaction.
func (d brokenDescriptor) Read([]byte) (int, error) { return 0, d.errorRead }

// TestDescriptorFailures covers fstat/read failures and cancellation without leaking partial values.
func TestDescriptorFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	os.WriteFile(path, []byte("public-fixture"), 0600)
	info, _ := os.Stat(path)
	for _, d := range []brokenDescriptor{{statErr: io.ErrUnexpectedEOF}, {info: info, errorRead: io.ErrUnexpectedEOF}} {
		if value, err := read(context.Background(), d); value != "" || !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	file, _ := os.Open(path)
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := read(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := fileFailure(context.Background(), os.ErrPermission); !errors.Is(err, secret.ErrPermission) {
		t.Fatal(err)
	}
}
