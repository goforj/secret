//go:build linux || darwin

package filesecret

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestProjectedVolumesAndEscape verifies Kubernetes rotation and the patched os.Root open boundary.
func TestProjectedVolumesAndEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "key"), []byte("outside-public-fixture"), 0600)
	os.Mkdir(filepath.Join(dir, "..v1"), 0700)
	os.WriteFile(filepath.Join(dir, "..v1", "key"), []byte("first\n"), 0600)
	os.Symlink("..v1", filepath.Join(dir, "..data"))
	os.Symlink("..data/key", filepath.Join(dir, "key"))
	os.Symlink(outside, filepath.Join(dir, "escape"))
	os.Symlink("escape/", filepath.Join(dir, "entry"))
	os.Symlink("loop", filepath.Join(dir, "loop"))
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	r, err := New(root, map[string]string{"KEY": "key", "ESCAPE": "entry", "LOOP": "loop"})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := r.Get(context.Background(), "KEY"); err != nil || value != "first\n" {
		t.Fatal("contained symlink rejected")
	}
	os.Mkdir(filepath.Join(dir, "..v2"), 0700)
	os.WriteFile(filepath.Join(dir, "..v2", "key"), []byte("second\n"), 0600)
	os.Symlink("..v2", filepath.Join(dir, "..next"))
	os.Rename(filepath.Join(dir, "..next"), filepath.Join(dir, "..data"))
	if value, err := r.Get(context.Background(), "KEY"); err != nil || value != "second\n" {
		t.Fatal("rotation not observed")
	}
	file, err := open(root, "entry")
	if file != nil {
		file.Close()
		t.Fatal("os.Root allowed escape during open")
	}
	if err == nil {
		t.Fatal("escape not rejected")
	}
	for _, name := range []string{"ESCAPE", "LOOP"} {
		if value, err := r.Get(context.Background(), name); value != "" || !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal("unsafe path accepted", err)
		}
	}
}

// TestFIFO ensures an unconnected FIFO cannot hang before the regular-file check.
func TestFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	r, _ := New(root, map[string]string{"PIPE": "pipe"})
	done := make(chan error, 1)
	go func() { _, err := r.Get(context.Background(), "PIPE"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO open blocked")
	}
}
