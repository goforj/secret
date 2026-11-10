package examples

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goforj/secret/driver/filesecret"
)

// TestPersistentKeyProgram executes the real configured program across key loss and recovery.
func TestPersistentKeyProgram(t *testing.T) {
	binary := buildProgram(t, "configured/persistent/main.go")
	expected := expectedOutput(t, "configured/persistent/main.go")
	directory := t.TempDir()
	keyDirectory := t.TempDir()
	backupDirectory := t.TempDir()
	keyFile := filepath.Join(keyDirectory, "master.key")
	backup := filepath.Join(backupDirectory, "master.key")
	// Public test-only bytes stand in for a separately provisioned key and recovery copy.
	key := bytes.Repeat([]byte{0x42}, 32)
	for _, name := range []string{keyFile, backup} {
		if err := os.WriteFile(name, key, 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	store, err := filesecret.NewStore(root, key, map[string]string{"DATABASE_PASSWORD": "database-password.enc"})
	if err != nil {
		t.Fatal(err)
	}
	value := "public-persistent-key-example-value\x00\xff\n"
	if err := store.Set(context.Background(), "DATABASE_PASSWORD", value); err != nil {
		t.Fatal(err)
	}
	before, err := root.ReadFile("database-password.enc")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-directory", directory, "-key-file", keyFile}
	read := func(wantSuccess bool) {
		t.Helper()
		output, stderr, err := runProgram(t, binary, args...)
		if wantSuccess {
			if err != nil || string(output) != expected {
				t.Fatalf("persistent read failed: %v\n%s", err, stderr)
			}
		} else if err == nil || len(output) != 0 {
			t.Fatal("invalid key state did not fail closed")
		}
		if strings.Contains(string(output)+stderr, value) || strings.Contains(string(output)+stderr, string(key)) || strings.Contains(stderr, keyFile) {
			t.Fatal("example disclosed key, secret, or private configuration")
		}
		after, err := root.ReadFile("database-password.enc")
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("read-only program changed ciphertext", err)
		}
	}
	// Each invocation is a new process that reloads the same persisted key.
	read(true)
	read(true)
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	read(false)
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatal("missing key was silently recreated")
	}
	for _, invalid := range [][]byte{nil, bytes.Repeat([]byte{0x24}, 31), append(bytes.Clone(key), '\n'), bytes.Repeat([]byte{0x24}, 32), bytes.Repeat([]byte{0x24}, 4096)} {
		if err := os.WriteFile(keyFile, invalid, 0600); err != nil {
			t.Fatal(err)
		}
		read(false)
	}
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(keyFile, key, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(keyFile, 0644); err != nil {
			t.Fatal(err)
		}
		read(false)
		if err := os.Chmod(keyFile, 0600); err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, recovered, 0600); err != nil {
		t.Fatal(err)
	}
	read(true)
	if _, stderr, err := runProgram(t, binary, "-directory", directory, "-key-file", keyDirectory); err == nil || strings.Contains(stderr, keyDirectory) {
		t.Fatal("directory accepted as an encryption key")
	}
	if _, _, err := runProgram(t, binary); err == nil {
		t.Fatal("missing explicit paths accepted")
	}
	if _, _, err := runProgram(t, binary, "-directory", directory); err == nil {
		t.Fatal("missing key path accepted")
	}
	if _, _, err := runProgram(t, binary, "-key-file", keyFile); err == nil {
		t.Fatal("missing secret directory accepted")
	}
	if _, _, err := runProgram(t, binary, "-directory", filepath.Join(directory, "missing"), "-key-file", keyFile); err == nil {
		t.Fatal("missing secret directory was created implicitly")
	}
	if err := root.WriteFile("database-password.enc", []byte("corrupt-public-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runProgram(t, binary, args...); err == nil {
		t.Fatal("corrupt ciphertext was accepted")
	}
}
