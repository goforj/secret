package filesecret

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/goforj/secret"
)

// encryptedFixture owns a private filesystem and a public, test-only encryption key.
func encryptedFixture(t *testing.T) (*os.Root, *Store, []byte) {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	key := bytes.Repeat([]byte{0x42}, 32)
	store, err := NewStore(root, key, map[string]string{"KEY": "key", "OTHER": "other", "NESTED": "nested/key"})
	if err != nil {
		t.Fatal(err)
	}
	return root, store, key
}

// TestEncryptedRoundTrip verifies actual ciphertext with an independent standard GCM reader.
func TestEncryptedRoundTrip(t *testing.T) {
	root, store, key := encryptedFixture(t)
	var _ secret.Store = store
	ctx := context.Background()
	for _, value := range []string{"public-example-value", "", "\x00\xff\n", strings.Repeat("x", MaxFileBytes)} {
		if err := store.Set(ctx, "KEY", value); err != nil {
			t.Fatal(err)
		}
		got, err := store.Get(ctx, "KEY")
		if err != nil || got != value {
			t.Fatal("exact bytes changed", err)
		}
	}
	value := "public-distinctive-ciphertext-test-value"
	if err := store.Set(ctx, "KEY", value); err != nil {
		t.Fatal(err)
	}
	data, err := root.ReadFile("key")
	if err != nil || bytes.Contains(data, []byte(value)) || bytes.Contains(data, []byte("KEY")) {
		t.Fatal("plaintext or binding leaked", err)
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	payload := data[len("goforj-secret\x00\x01"):]
	plaintext, err := gcm.Open(nil, payload[:12], payload[12:], []byte("goforj-secret\x00\x01KEY\x00key"))
	if err != nil || string(plaintext) != value {
		t.Fatal("stored envelope is not interoperable AES-256-GCM", err)
	}
	if err := store.Set(ctx, "KEY", value); err != nil {
		t.Fatal(err)
	}
	next, _ := root.ReadFile("key")
	if bytes.Equal(data, next) || bytes.Equal(data[len(encryptedHeader):len(encryptedHeader)+12], next[len(encryptedHeader):len(encryptedHeader)+12]) {
		t.Fatal("nonce was reused")
	}
	info, _ := root.Stat("key")
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatal("ciphertext file has broad permissions")
	}
	entries, _ := os.ReadDir(root.Name())
	if len(entries) != 1 {
		t.Fatal("staging files remain after success")
	}
	// Reopening proves that neither plaintext nor a process-local cache is needed.
	reopened, err := NewStore(root, key, map[string]string{"KEY": "key"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.Get(ctx, "KEY"); err != nil || got != value {
		t.Fatal("reopen failed", err)
	}
	if err := reopened.Delete(ctx, "KEY"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Delete(ctx, "KEY"); err != nil {
		t.Fatal("delete is not idempotent", err)
	}
	if got, err := reopened.Get(ctx, "KEY"); got != "" || !errors.Is(err, secret.ErrNotFound) {
		t.Fatal("deleted value remained readable", err)
	}
}

// checkpointContext deterministically cancels between validation and filesystem checkpoints.
type checkpointContext struct {
	context.Context
	calls, cancelAt int
}

// Err freezes cancellation races without relying on scheduler timing.
func (c *checkpointContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

// TestEncryptedCancellationCheckpoints rejects cancellation after reads and before publication.
func TestEncryptedCancellationCheckpoints(t *testing.T) {
	root, store, _ := encryptedFixture(t)
	ctx := context.Background()
	if err := store.Set(ctx, "KEY", "public-value"); err != nil {
		t.Fatal(err)
	}
	lateRead := &checkpointContext{Context: ctx, cancelAt: 3}
	if value, err := store.Get(lateRead, "KEY"); value != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled read returned plaintext", err)
	}
	beforeGate := &checkpointContext{Context: ctx, cancelAt: 2}
	if err := store.Delete(beforeGate, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	staging := &checkpointContext{Context: ctx, cancelAt: 6}
	if err := store.Set(staging, "OTHER", "never-published"); !errors.Is(err, context.Canceled) || errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal("staged cancellation published", err)
	}
	entries, _ := os.ReadDir(root.Name())
	if len(entries) != 1 {
		t.Fatal("canceled staging left a file")
	}
	gate := make(chan struct{}, 1)
	if err := acquire(&checkpointContext{Context: ctx, cancelAt: 1}, gate); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := acquire(&checkpointContext{Context: ctx, cancelAt: 2}, gate); !errors.Is(err, context.Canceled) || len(gate) != 0 {
		t.Fatal("canceled acquisition retained a gate", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	gate <- struct{}{}
	if err := acquire(&checkpointContext{Context: canceled, cancelAt: 2}, gate); !errors.Is(err, context.Canceled) || len(gate) != 1 {
		t.Fatal("blocked cancellation changed the occupied gate", err)
	}
	<-gate
}

// TestEncryptedConstructor verifies key policy, binding ownership, and path validation.
func TestEncryptedConstructor(t *testing.T) {
	root, _, key := encryptedFixture(t)
	if _, err := NewStore(nil, key, map[string]string{"KEY": "key"}); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, 16, 24, 31, 33} {
		if _, err := NewStore(root, make([]byte, size), map[string]string{"KEY": "key"}); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid key accepted")
		}
	}
	for _, bindings := range []map[string]string{nil, {"KEY": "../key"}, {"bad": "key"}, {"KEY": "key", "OTHER": "key"}} {
		if _, err := NewStore(root, key, bindings); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid bindings accepted")
		}
	}
	bindings := map[string]string{"KEY": "key"}
	store, err := NewStore(root, key, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings["KEY"] = "other"
	if err := store.Set(context.Background(), "KEY", "public-value"); err != nil {
		t.Fatal(err)
	}
	key[0] ^= 1
	if got, err := store.Get(context.Background(), "KEY"); err != nil || got != "public-value" {
		t.Fatal("caller key or binding mutation affected store", err)
	}
}

// TestEncryptedTampering rejects substitution, wrong keys, truncation, and plaintext before mutation.
func TestEncryptedTampering(t *testing.T) {
	root, store, key := encryptedFixture(t)
	ctx := context.Background()
	if err := store.Set(ctx, "KEY", "public-authenticated-value"); err != nil {
		t.Fatal(err)
	}
	valid, _ := root.ReadFile("key")
	tampered := bytes.Clone(valid)
	tampered[len(tampered)-1] ^= 1
	badVersion := bytes.Clone(valid)
	badVersion[len(encryptedHeader)-1]++
	for _, data := range [][]byte{nil, []byte("plaintext-value"), valid[:len(encryptedHeader)], valid[:len(valid)-1], tampered, badVersion, append(bytes.Clone(valid), 0), make([]byte, MaxFileBytes+len(encryptedHeader)+29)} {
		if err := root.WriteFile("key", data, 0600); err != nil {
			t.Fatal(err)
		}
		if value, err := store.Get(ctx, "KEY"); value != "" || !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal("unauthenticated read succeeded", err)
		}
		if err := store.Set(ctx, "KEY", "replacement"); !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal("unreadable file was overwritten", err)
		}
		if err := store.Delete(ctx, "KEY"); !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal("unreadable file was deleted", err)
		}
		got, _ := root.ReadFile("key")
		if !bytes.Equal(got, data) {
			t.Fatal("failed operation changed the original file")
		}
	}
	root.WriteFile("key", valid, 0600)
	wrong, _ := NewStore(root, bytes.Repeat([]byte{0x24}, 32), map[string]string{"KEY": "key"})
	if value, err := wrong.Get(ctx, "KEY"); value != "" || !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("wrong key accepted", err)
	}
	if err := wrong.Set(ctx, "KEY", "replacement"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("wrong-key overwrite accepted", err)
	}
	root.WriteFile("other", valid, 0600)
	if value, err := store.Get(ctx, "OTHER"); value != "" || !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("cross-binding substitution accepted", err)
	}
	renamed, _ := NewStore(root, key, map[string]string{"RENAMED": "key"})
	if _, err := renamed.Get(ctx, "RENAMED"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("renamed binding accepted", err)
	}
	moved, _ := NewStore(root, key, map[string]string{"KEY": "other"})
	if _, err := moved.Get(ctx, "KEY"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("moved ciphertext accepted", err)
	}
}

// TestEncryptedValidation exercises all capabilities without dispatching invalid requests.
func TestEncryptedValidation(t *testing.T) {
	root, store, _ := encryptedFixture(t)
	ctx := context.Background()
	for _, name := range []string{"UNKNOWN", "invalid"} {
		want := secret.ErrNotFound
		if name == "invalid" {
			want = secret.ErrInvalid
		}
		if _, err := store.Get(ctx, name); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if err := store.Set(ctx, name, "value"); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if err := store.Delete(ctx, name); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	if err := store.Set(ctx, "KEY", strings.Repeat("x", MaxFileBytes+1)); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if err := store.Set(nil, "KEY", "value"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if err := store.Delete(nil, "KEY"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Get(canceled, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.Set(canceled, "KEY", "value"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.Delete(canceled, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "NESTED", "value"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal("missing parent directory was created", err)
	}
	root.Mkdir("nested", 0700)
	if err := store.Set(ctx, "NESTED", "value"); err != nil {
		t.Fatal(err)
	}
	root.Mkdir("other", 0700)
	if _, err := store.Get(ctx, "OTHER"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal("directory accepted", err)
	}
	root.Close()
	if _, err := store.Get(ctx, "KEY"); !errors.Is(err, secret.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := syncParent(root, "key"); err == nil {
		t.Fatal("closed-root sync succeeded")
	}
}

// TestEncryptedConcurrency verifies whole authenticated values under overlapping readers and writers.
func TestEncryptedConcurrency(t *testing.T) {
	_, store, _ := encryptedFixture(t)
	ctx := context.Background()
	if err := store.Set(ctx, "KEY", "initial"); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 8; j++ {
				if err := store.Set(ctx, "KEY", "replacement"); err != nil {
					t.Error(err)
				}
				if value, err := store.Get(ctx, "KEY"); err != nil || value != "initial" && value != "replacement" {
					t.Error("partial or unauthenticated value", err)
				}
			}
		}()
	}
	workers.Wait()
	store.gates["KEY"] <- struct{}{}
	started := make(chan struct{})
	done := make(chan error, 1)
	waiting, cancel := context.WithCancel(ctx)
	go func() { close(started); done <- store.Set(waiting, "KEY", "blocked") }()
	<-started
	if err := store.Set(ctx, "OTHER", "independent"); err != nil {
		t.Fatal("another binding was blocked", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal("canceled waiter dispatched", err)
	}
	<-store.gates["KEY"]
}

// failingStage injects precommit I/O failures without writing plaintext or requiring a failing disk.
type failingStage struct {
	writeErr, syncErr, closeErr error
	short                       bool
	closed                      bool
}

// Write simulates full, partial, or failed ciphertext writes.
func (f *failingStage) Write(data []byte) (int, error) {
	if f.short {
		return 0, nil
	}
	return len(data), f.writeErr
}

// Sync simulates disk flush failure before publication.
func (f *failingStage) Sync() error { return f.syncErr }

// Close records descriptor cleanup on success and failure.
func (f *failingStage) Close() error { f.closed = true; return f.closeErr }

// TestEncryptedStageFailures proves precommit failures are definite and close descriptors.
func TestEncryptedStageFailures(t *testing.T) {
	unsafe := errors.New("unsafe-provider-data")
	for _, file := range []*failingStage{{writeErr: unsafe}, {short: true}, {syncErr: unsafe}, {closeErr: unsafe}} {
		err := stage(context.Background(), file, []byte("ciphertext"))
		if !errors.Is(err, secret.ErrUnavailable) || errors.Is(err, secret.ErrIndeterminate) || !file.closed || strings.Contains(err.Error(), unsafe.Error()) {
			t.Fatal("unsafe staging failure", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stage(ctx, &failingStage{}, []byte("ciphertext")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestEncryptedCommitFailures distinguishes cancellation before and after publication.
func TestEncryptedCommitFailures(t *testing.T) {
	unsafe := errors.New("unsafe-path-and-provider-data")
	ok := func() error { return nil }
	fail := func() error { return unsafe }
	for _, functions := range [][2]func() error{{fail, ok}, {ok, fail}} {
		err := commit(context.Background(), functions[0], functions[1])
		if !errors.Is(err, secret.ErrIndeterminate) || strings.Contains(err.Error(), unsafe.Error()) {
			t.Fatal("publication failure misclassified", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := commit(ctx, func() error { t.Fatal("canceled commit dispatched"); return nil }, ok); !errors.Is(err, context.Canceled) || errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if err := commit(ctx, func() error { cancel(); return nil }, ok); !errors.Is(err, context.Canceled) || !errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal(err)
	}
}

// TestEncryptedSymlinkEscape confines all operations to the supplied root.
func TestEncryptedSymlinkEscape(t *testing.T) {
	root, store, _ := encryptedFixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "value"), []byte("outside-public-value"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "value"), filepath.Join(root.Name(), "key")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	for _, operation := range []func() error{
		func() error { _, err := store.Get(context.Background(), "KEY"); return err },
		func() error { return store.Set(context.Background(), "KEY", "replacement") },
		func() error { return store.Delete(context.Background(), "KEY") },
	} {
		if err := operation(); !errors.Is(err, secret.ErrUnavailable) {
			t.Fatal("root escaped", err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(outside, "value"))
	if string(data) != "outside-public-value" {
		t.Fatal("outside file changed")
	}
}
