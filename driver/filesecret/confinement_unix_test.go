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

// TestConfinementDuringSymlinkSwaps refuses outside data while an unprivileged writer swaps a bound path.
func TestConfinementDuringSymlinkSwaps(t *testing.T) {
	directory := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "inside"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "inside/key"), []byte("public-inside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "key"), []byte("public-outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside", filepath.Join(directory, "entry")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	reader, err := New(root, map[string]string{"KEY": "entry/key"})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := reader.Get(context.Background(), "KEY"); err != nil || value != "public-inside" {
		t.Fatal("contained path unavailable")
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for {
			for _, target := range []string{outside, "inside"} {
				select {
				case <-stop:
					done <- nil
					return
				default:
				}
				next := filepath.Join(directory, "next")
				if err := os.Symlink(target, next); err != nil {
					done <- err
					return
				}
				if err := os.Rename(next, filepath.Join(directory, "entry")); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Error("symlink fixture failed")
		}
	}()
	for range 500 {
		value, err := reader.Get(context.Background(), "KEY")
		if err == nil {
			if value != "public-inside" {
				t.Fatal("root confinement failed during a path swap")
			}
		} else if value != "" || !errors.Is(err, secret.ErrUnavailable) && !errors.Is(err, secret.ErrNotFound) {
			t.Fatal("unsafe path failure classification", err)
		}
	}
}
