package filesecret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/goforj/secret"
)

// encryptedHeader identifies the versioned AES-256-GCM envelope, never plaintext.
const encryptedHeader = "goforj-secret\x00\x01"

// Store encrypts bound files using a caller-owned key and root. It is safe for
// concurrent use. Mutations serialize per binding within this instance.
// The caller must exclusively own the directory and each resource's lifecycle.
type Store struct {
	root     *os.Root
	aead     cipher.AEAD
	bindings map[string]string
	gates    map[string]chan struct{}
}

// NewStore uses a caller-supplied 32-byte random AES key for encrypted local storage.
// It copies bindings and key material, borrows root, and never persists the key.
// Keep the key separately in a key manager; losing it loses access to the data.
// Set may create a bound file; parent directories must already exist.
// Existing plaintext, corrupt files, or files encrypted with another key are
// rejected by every operation. New remains the plaintext mounted-file reader.
func NewStore(root *os.Root, key []byte, bindings map[string]string) (*Store, error) {
	reader, err := New(root, bindings)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil || len(key) != 32 {
		return nil, secret.ErrInvalid
	}
	// The validated AES block always supports the standard random-nonce GCM mode.
	aead, _ := cipher.NewGCMWithRandomNonce(block)
	paths := make(map[string]bool, len(bindings))
	gates := make(map[string]chan struct{}, len(bindings))
	for name, file := range reader.bindings {
		if paths[file] {
			return nil, secret.ErrInvalid
		}
		paths[file] = true
		gates[name] = make(chan struct{}, 1)
	}
	return &Store{root: root, aead: aead, bindings: reader.bindings, gates: gates}, nil
}

// Get authenticates and decrypts one bound file, preserving up to MaxFileBytes
// of exact plaintext. Authentication failures return ErrUnavailable and no value.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	filePath, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return "", err
	}
	file, err := open(s.root, filePath)
	if err != nil {
		return "", fileFailure(ctx, err)
	}
	defer file.Close()
	data, err := readLimit(ctx, file, MaxFileBytes+len(encryptedHeader)+s.aead.Overhead())
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(data, encryptedHeader) {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	plaintext, err := s.aead.Open(nil, nil, []byte(data[len(encryptedHeader):]), s.associatedData(name))
	if err != nil {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Set encrypts exact bytes and replaces a bound file after syncing a private
// ciphertext-only staging file. No plaintext is written to disk. It refuses to
// overwrite a file that cannot be authenticated with this key and binding.
// Once replacement is attempted, failures are conservatively ErrIndeterminate.
// Use fewer than 2^32 writes across all stores sharing the same key.
func (s *Store) Set(ctx context.Context, name, value string) error {
	filePath, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if len(value) > MaxFileBytes {
		return secret.ErrInvalid
	}
	if err := acquire(ctx, s.gates[name]); err != nil {
		return err
	}
	defer func() { <-s.gates[name] }()
	if _, err := s.Get(ctx, name); err != nil && !errors.Is(err, secret.ErrNotFound) {
		return err
	}
	data := s.aead.Seal([]byte(encryptedHeader), nil, []byte(value), s.associatedData(name))
	temporary := path.Join(path.Dir(filePath), ".secret-"+rand.Text())
	file, err := s.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fileFailure(ctx, err)
	}
	defer s.root.Remove(temporary)
	if err := stage(ctx, file, data); err != nil {
		return err
	}
	return commit(ctx, func() error { return s.root.Rename(temporary, filePath) }, func() error {
		return syncParent(s.root, filePath)
	})
}

// Delete authenticates and removes one bound encrypted file, accepting absence.
// It does not promise secure erasure of filesystem snapshots or previous blocks.
func (s *Store) Delete(ctx context.Context, name string) error {
	filePath, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if err := acquire(ctx, s.gates[name]); err != nil {
		return err
	}
	defer func() { <-s.gates[name] }()
	if _, err := s.Get(ctx, name); err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return nil
		}
		return err
	}
	return commit(ctx, func() error { return s.root.Remove(filePath) }, func() error {
		return syncParent(s.root, filePath)
	})
}

// associatedData prevents cross-binding substitution without storing identifiers.
func (s *Store) associatedData(name string) []byte {
	return []byte(encryptedHeader + name + "\x00" + s.bindings[name])
}

// stagedFile isolates the precommit write and durability contract for failure tests.
type stagedFile interface {
	io.Writer
	Sync() error
	Close() error
}

// stage closes the ciphertext file on every path before publication is possible.
func stage(ctx context.Context, file stagedFile, data []byte) error {
	if n, err := file.Write(data); err != nil || n != len(data) {
		file.Close()
		return failure(ctx, secret.ErrUnavailable)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return failure(ctx, secret.ErrUnavailable)
	}
	if err := file.Close(); err != nil {
		return failure(ctx, secret.ErrUnavailable)
	}
	return ctx.Err()
}

// commit separates pre-dispatch cancellation from possibly committed mutations.
func commit(ctx context.Context, change, sync func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := change(); err != nil {
		return errors.Join(secret.ErrIndeterminate, ctx.Err())
	}
	if err := sync(); err != nil {
		return errors.Join(secret.ErrIndeterminate, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(secret.ErrIndeterminate, err)
	}
	return nil
}

// syncParent persists the changed directory entry on supporting filesystems.
func syncParent(root *os.Root, filePath string) error {
	directory, err := root.Open(path.Dir(filePath))
	if err != nil {
		return err
	}
	defer directory.Close()
	return syncDirectory(directory)
}

// acquire serializes one binding without making canceled callers wait for I/O.
func acquire(ctx context.Context, gate chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
