package filesecret

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goforj/secret"
)

// crashCheckpoint blocks at an observable filesystem boundary until the parent kills the process.
type crashCheckpoint struct {
	context.Context
	directory, phase string
	original         []byte
}

// Err coordinates real process termination without adding production crash hooks.
func (c *crashCheckpoint) Err() error {
	ready := false
	if c.phase == "staged" {
		entries, _ := filepath.Glob(filepath.Join(c.directory, ".secret-*"))
		ready = len(entries) != 0
	} else {
		data, _ := os.ReadFile(filepath.Join(c.directory, "first"))
		ready = len(data) != 0 && !bytes.Equal(data, c.original)
	}
	if ready {
		fmt.Println("crash checkpoint ready")
		// A timer keeps the child alive even if every other goroutine is blocked.
		time.Sleep(time.Hour)
	}
	return nil
}

// crashKeys returns public test-only keys, never production credentials or command arguments.
func crashKeys() ([]byte, []byte) {
	return bytes.Repeat([]byte{0x42}, 32), bytes.Repeat([]byte{0x24}, 32)
}

// crashBindings keeps the persisted binding identity identical across independent processes.
func crashBindings() map[string]string {
	return map[string]string{"FIRST": "first", "SECOND": "second"}
}

// TestEncryptedCrashProcess runs only when invoked as a subprocess by TestEncryptedCrashRecovery.
func TestEncryptedCrashProcess(t *testing.T) {
	mode := os.Getenv("SECRET_CRASH_TEST_MODE")
	if mode == "" {
		return
	}
	directory := os.Getenv("SECRET_CRASH_TEST_DIRECTORY")
	operation := os.Getenv("SECRET_CRASH_TEST_OPERATION")
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	oldKey, newKey := crashKeys()
	key := oldKey
	var previous [][]byte
	if operation == "rotate" {
		key, previous = newKey, [][]byte{oldKey}
	}
	store, err := NewStoreWithKeys(root, key, crashBindings(), previous...)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "mutate" {
		original, _ := root.ReadFile("first")
		ctx := &crashCheckpoint{Context: context.Background(), directory: directory, phase: os.Getenv("SECRET_CRASH_TEST_PHASE"), original: original}
		if operation == "rotate" {
			err = store.Rotate(ctx)
		} else {
			err = store.Set(ctx, "FIRST", "public-crash-replacement-value")
		}
		t.Fatalf("mutation returned before the crash checkpoint: %v", err)
	}
	if mode != "recover" {
		t.Fatal("unknown crash helper mode")
	}
	if operation == "rotate" {
		if err := store.Rotate(context.Background()); err != nil {
			t.Fatal(err)
		}
		// Removing the previous key proves that restart migration really completed.
		store, err = NewStore(root, newKey, crashBindings())
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"FIRST", "SECOND"} {
			if value, err := store.Get(context.Background(), name); err != nil || value != "public-crash-original-value" {
				t.Fatal("restart rotation lost a value", err)
			}
		}
	} else {
		if err := store.Set(context.Background(), "FIRST", "public-crash-replacement-value"); err != nil {
			t.Fatal(err)
		}
		if value, err := store.Get(context.Background(), "FIRST"); err != nil || value != "public-crash-replacement-value" {
			t.Fatal("restart write failed", err)
		}
	}
}

// crashCommand gives every subprocess a bounded lifetime and explicit fixture-only configuration.
func crashCommand(ctx context.Context, directory, operation, phase, mode string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEncryptedCrashProcess$", "-test.timeout=25s")
	command.Env = append(os.Environ(), "SECRET_CRASH_TEST_MODE="+mode, "SECRET_CRASH_TEST_DIRECTORY="+directory, "SECRET_CRASH_TEST_OPERATION="+operation, "SECRET_CRASH_TEST_PHASE="+phase)
	return command
}

// killAtCheckpoint ensures the child reached the intended boundary before terminating it without cleanup.
func killAtCheckpoint(t *testing.T, directory, operation, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := crashCommand(ctx, directory, operation, phase, "mutate")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			command.Process.Kill()
			command.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		ready <- err == nil && line == "crash checkpoint ready\n"
	}()
	select {
	case valid := <-ready:
		if !valid {
			t.Fatal("child failed before reaching the crash checkpoint")
		}
	case <-ctx.Done():
		t.Fatal("child did not reach the crash checkpoint")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	if err == nil {
		t.Fatal("crashed child exited successfully")
	}
}

// TestEncryptedCrashRecovery verifies durable files after process death and recovery in a fresh process.
func TestEncryptedCrashRecovery(t *testing.T) {
	for _, operation := range []string{"create", "replace", "rotate"} {
		for _, phase := range []string{"staged", "published"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				directory := t.TempDir()
				root, err := os.OpenRoot(directory)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				oldKey, newKey := crashKeys()
				old, err := NewStore(root, oldKey, crashBindings())
				if err != nil {
					t.Fatal(err)
				}
				if operation != "create" {
					if err := old.Set(context.Background(), "FIRST", "public-crash-original-value"); err != nil {
						t.Fatal(err)
					}
				}
				if operation == "rotate" {
					if err := old.Set(context.Background(), "SECOND", "public-crash-original-value"); err != nil {
						t.Fatal(err)
					}
				}
				killAtCheckpoint(t, directory, operation, phase)
				value, err := old.Get(context.Background(), "FIRST")
				if phase == "staged" {
					if operation == "create" {
						if !errors.Is(err, secret.ErrNotFound) {
							t.Fatal("unpublished create became visible", err)
						}
					} else if err != nil || value != "public-crash-original-value" {
						t.Fatal("unpublished write damaged the original value", err)
					}
				} else if operation == "rotate" {
					current, _ := NewStore(root, newKey, crashBindings())
					if got, err := current.Get(context.Background(), "FIRST"); err != nil || got != "public-crash-original-value" {
						t.Fatal("published rotation lost the first value", err)
					}
					if got, err := old.Get(context.Background(), "SECOND"); err != nil || got != "public-crash-original-value" {
						t.Fatal("partial rotation changed the next file", err)
					}
					if !errors.Is(err, secret.ErrUnavailable) {
						t.Fatal("first file was not re-encrypted before the crash", err)
					}
				} else if err != nil || value != "public-crash-replacement-value" {
					t.Fatal("published write was not recoverable", err)
				}
				entries, err := os.ReadDir(directory)
				if err != nil {
					t.Fatal(err)
				}
				staged := 0
				for _, entry := range entries {
					data, err := root.ReadFile(entry.Name())
					if err != nil || !bytes.HasPrefix(data, []byte(encryptedHeader)) || bytes.Contains(data, []byte("public-crash-")) {
						t.Fatal("crash left plaintext or an unexpected file", err)
					}
					if strings.HasPrefix(entry.Name(), ".secret-") {
						staged++
					}
				}
				if phase == "staged" && staged != 1 {
					t.Fatal("crash did not retain the unpublished ciphertext staging file")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if output, err := crashCommand(ctx, directory, operation, phase, "recover").CombinedOutput(); err != nil {
					t.Fatalf("fresh-process recovery failed: %v\n%s", err, output)
				}
			})
		}
	}
}
