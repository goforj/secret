package filesecret

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/goforj/secret"
)

// TestPreviousKeys verifies ordered fallback, primary-only writes, copied keys, and read-only Get.
func TestPreviousKeys(t *testing.T) {
	root, old, oldKey := encryptedFixture(t)
	ctx := context.Background()
	if err := old.Set(ctx, "KEY", "old-public-value"); err != nil {
		t.Fatal(err)
	}
	olderKey := bytes.Repeat([]byte{0x13}, 32)
	older, err := NewStore(root, olderKey, map[string]string{"OTHER": "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := older.Set(ctx, "OTHER", "older-public-value"); err != nil {
		t.Fatal(err)
	}
	currentKey := bytes.Repeat([]byte{0x24}, 32)
	currentCopy := bytes.Clone(currentKey)
	previous := [][]byte{bytes.Repeat([]byte{0x31}, 32), oldKey, olderKey}
	store, err := NewStoreWithKeys(root, currentKey, map[string]string{"KEY": "key", "OTHER": "other"}, previous...)
	if err != nil {
		t.Fatal(err)
	}
	currentKey[0] ^= 1
	oldKey[0] ^= 1
	olderKey[0] ^= 1
	previous[1] = nil
	for name, want := range map[string]string{"KEY": "old-public-value", "OTHER": "older-public-value"} {
		before, _ := root.ReadFile(store.bindings[name])
		if value, err := store.Get(ctx, name); err != nil || value != want {
			t.Fatal("previous-key value unreadable", err)
		}
		after, _ := root.ReadFile(store.bindings[name])
		if !bytes.Equal(before, after) {
			t.Fatal("Get modified a previous-key file")
		}
	}
	if err := store.Set(ctx, "KEY", "current-public-value"); err != nil {
		t.Fatal(err)
	}
	current, err := NewStore(root, currentCopy, map[string]string{"KEY": "key"})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := current.Get(ctx, "KEY"); err != nil || value != "current-public-value" {
		t.Fatal("write did not use current key", err)
	}
	if _, err := old.Get(ctx, "KEY"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("write still decrypts with previous key", err)
	}
	if err := store.Delete(ctx, "OTHER"); err != nil {
		t.Fatal("previous-key delete failed", err)
	}
}

// TestRotate migrates old keys in place, preserves exact values, and skips current or absent files.
func TestRotate(t *testing.T) {
	root, old, oldKey := encryptedFixture(t)
	ctx := context.Background()
	if err := old.Set(ctx, "KEY", "\x00\xff\n"); err != nil {
		t.Fatal(err)
	}
	if err := old.Set(ctx, "OTHER", ""); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x24}, 32)
	store, err := NewStoreWithKeys(root, key, old.bindings, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Rotate(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := NewStore(root, key, old.bindings)
	for name, want := range map[string]string{"KEY": "\x00\xff\n", "OTHER": ""} {
		if value, err := current.Get(ctx, name); err != nil || value != want {
			t.Fatal("rotation changed a value", err)
		}
		if _, err := old.Get(ctx, name); !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal("rotation retained old encryption", err)
		}
	}
	if _, err := store.Get(ctx, "NESTED"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal("rotation provisioned a missing file", err)
	}
	before, _ := root.ReadFile("key")
	if err := store.Rotate(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := root.ReadFile("key")
	if !bytes.Equal(before, after) {
		t.Fatal("already-current ciphertext was rewritten")
	}
	if err := store.Rotate(nil); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Rotate(canceled); !errors.Is(err, context.Canceled) || errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal("canceled rotation dispatched", err)
	}
}

// TestRotatePartialFailure preserves unreadable data and resumes without rewriting completed files.
func TestRotatePartialFailure(t *testing.T) {
	root, old, oldKey := encryptedFixture(t)
	ctx := context.Background()
	for _, name := range []string{"KEY", "OTHER"} {
		if err := old.Set(ctx, name, "public-"+name); err != nil {
			t.Fatal(err)
		}
	}
	if err := root.Mkdir("nested", 0700); err != nil {
		t.Fatal(err)
	}
	broken := []byte("public-corrupt-fixture")
	if err := root.WriteFile("nested/key", broken, 0600); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x24}, 32)
	store, _ := NewStoreWithKeys(root, key, old.bindings, oldKey)
	current, _ := NewStore(root, key, old.bindings)
	if err := store.Rotate(ctx); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("unreadable rotation target was ignored", err)
	}
	if value, err := current.Get(ctx, "KEY"); err != nil || value != "public-KEY" {
		t.Fatal("completed file was not migrated", err)
	}
	if _, err := current.Get(ctx, "OTHER"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("rotation continued past its first failure", err)
	}
	if value, err := old.Get(ctx, "OTHER"); err != nil || value != "public-OTHER" {
		t.Fatal("unprocessed file was damaged", err)
	}
	got, _ := root.ReadFile("nested/key")
	if !bytes.Equal(got, broken) {
		t.Fatal("corrupt target was overwritten")
	}
	before, _ := root.ReadFile("key")
	if err := root.Remove("nested/key"); err != nil {
		t.Fatal(err)
	}
	if err := store.Rotate(ctx); err != nil {
		t.Fatal("retry failed", err)
	}
	after, _ := root.ReadFile("key")
	if !bytes.Equal(before, after) {
		t.Fatal("retry rewrote a completed file")
	}
	if value, err := current.Get(ctx, "OTHER"); err != nil || value != "public-OTHER" {
		t.Fatal("retry did not complete migration", err)
	}
}

// TestRotateCancellation retains old-key data when staging is canceled before publication.
func TestRotateCancellation(t *testing.T) {
	root, old, oldKey := encryptedFixture(t)
	ctx := context.Background()
	if err := old.Set(ctx, "KEY", "public-value"); err != nil {
		t.Fatal(err)
	}
	before, _ := root.ReadFile("key")
	store, _ := NewStoreWithKeys(root, bytes.Repeat([]byte{0x24}, 32), old.bindings, oldKey)
	staging := &checkpointContext{Context: ctx, cancelAt: 8}
	if err := store.Rotate(staging); !errors.Is(err, context.Canceled) || errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal("prepublication rotation cancellation misclassified", err)
	}
	after, _ := root.ReadFile("key")
	if !bytes.Equal(before, after) {
		t.Fatal("canceled rotation replaced original ciphertext")
	}
}

// TestRotateConcurrentSet proves migration cannot overwrite a newer same-instance write.
func TestRotateConcurrentSet(t *testing.T) {
	root, old, oldKey := encryptedFixture(t)
	ctx := context.Background()
	if err := old.Set(ctx, "KEY", "old-public-value"); err != nil {
		t.Fatal(err)
	}
	initial, _ := root.ReadFile("key")
	store, _ := NewStoreWithKeys(root, bytes.Repeat([]byte{0x24}, 32), old.bindings, oldKey)
	for iteration := 0; iteration < 16; iteration++ {
		if err := root.WriteFile("key", initial, 0600); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		failures := make(chan error, 2)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); <-start; failures <- store.Rotate(ctx) }()
		go func() { defer workers.Done(); <-start; failures <- store.Set(ctx, "KEY", "new-public-value") }()
		close(start)
		workers.Wait()
		for i := 0; i < 2; i++ {
			if err := <-failures; err != nil {
				t.Fatal(err)
			}
		}
		if value, err := store.Get(ctx, "KEY"); err != nil || value != "new-public-value" {
			t.Fatal("rotation replaced a newer write", err)
		}
	}
}

// TestPreviousKeyValidation rejects malformed fallback keys before any filesystem operation.
func TestPreviousKeyValidation(t *testing.T) {
	root, _, key := encryptedFixture(t)
	for _, size := range []int{0, 1, 16, 24, 31, 33} {
		if _, err := NewStoreWithKeys(root, key, map[string]string{"KEY": "key"}, make([]byte, size)); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid previous key accepted")
		}
	}
	store, err := NewStoreWithKeys(root, key, map[string]string{"KEY": "key"}, key)
	if err != nil {
		t.Fatal("repeated key changed semantics", err)
	}
	if err := store.Set(context.Background(), "KEY", "public-value"); err != nil {
		t.Fatal(err)
	}
	if value, index, err := store.get(context.Background(), "KEY"); err != nil || index != 0 || value != "public-value" {
		t.Fatal("current key did not take precedence", err)
	}
}
