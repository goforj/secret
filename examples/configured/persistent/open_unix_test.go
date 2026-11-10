//go:build linux || darwin

package main

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestKeyFIFO proves a mistaken special-file path is rejected without waiting for a writer.
func TestKeyFIFO(t *testing.T) {
	name := filepath.Join(t.TempDir(), "key-pipe")
	if err := syscall.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := loadKey(name); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as an encryption key")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("key loader blocked on a FIFO")
	}
}
