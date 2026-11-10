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
	"sort"
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
	aeads    []cipher.AEAD
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
	return NewStoreWithKeys(root, key, bindings)
}

// NewStoreWithKeys writes with key and reads with key followed by previousKeys.
// Every key must contain 32 random bytes. Key material and bindings are copied.
// Previous keys permit existing files to remain readable during key rotation;
// Get never rewrites files. Keep previous keys until all data and required
// backups have been migrated. Rotate migrates bound files to the current key.
// No key identifiers or key material are persisted.
func NewStoreWithKeys(root *os.Root, key []byte, bindings map[string]string, previousKeys ...[]byte) (*Store, error) {
	reader, err := New(root, bindings)
	if err != nil {
		return nil, err
	}
	keys := append([][]byte{key}, previousKeys...)
	aeads := make([]cipher.AEAD, 0, len(keys))
	for _, key := range keys {
		block, err := aes.NewCipher(key)
		if err != nil || len(key) != 32 {
			return nil, secret.ErrInvalid
		}
		// Validated AES blocks always support the standard random-nonce GCM mode.
		aead, _ := cipher.NewGCMWithRandomNonce(block)
		aeads = append(aeads, aead)
	}
	paths := make(map[string]bool, len(bindings))
	gates := make(map[string]chan struct{}, len(bindings))
	for name, file := range reader.bindings {
		if paths[file] {
			return nil, secret.ErrInvalid
		}
		paths[file] = true
		gates[name] = make(chan struct{}, 1)
	}
	return &Store{root: root, aeads: aeads, bindings: reader.bindings, gates: gates}, nil
}

// Get authenticates and decrypts one bound file, preserving up to MaxFileBytes
// of exact plaintext. Authentication failures return ErrUnavailable and no value.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	value, _, err := s.get(ctx, name)
	return value, err
}

// get reports which key authenticated a file without changing the public Reader contract.
func (s *Store) get(ctx context.Context, name string) (string, int, error) {
	filePath, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return "", -1, err
	}
	file, err := open(s.root, filePath)
	if err != nil {
		return "", -1, fileFailure(ctx, err)
	}
	defer file.Close()
	data, err := readLimit(ctx, file, MaxFileBytes+len(encryptedHeader)+s.aeads[0].Overhead())
	if err != nil {
		return "", -1, err
	}
	if !strings.HasPrefix(data, encryptedHeader) {
		return "", -1, failure(ctx, secret.ErrUnavailable)
	}
	ciphertext := []byte(data[len(encryptedHeader):])
	associated := s.associatedData(name)
	for index, aead := range s.aeads {
		if err := ctx.Err(); err != nil {
			return "", -1, err
		}
		plaintext, err := aead.Open(nil, nil, ciphertext, associated)
		if err != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", -1, err
		}
		return string(plaintext), index, nil
	}
	return "", -1, failure(ctx, secret.ErrUnavailable)
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
	return s.write(ctx, name, filePath, value)
}

// write publishes current-key ciphertext while the caller holds the binding gate.
func (s *Store) write(ctx context.Context, name, filePath, value string) error {
	data := s.aeads[0].Seal([]byte(encryptedHeader), nil, []byte(value), s.associatedData(name))
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

// Rotate re-encrypts bound files authenticated by previous keys with the current
// key. It skips missing files and files already using the current key. Files are
// processed in name order; this is not a multi-file transaction. On error, earlier
// files may already be migrated, so retain previous keys and retry after resolving
// the error. Ordinary Get calls never perform this migration implicitly.
func (s *Store) Rotate(ctx context.Context) error {
	if ctx == nil {
		return secret.ErrInvalid
	}
	names := make([]string, 0, len(s.bindings))
	for name := range s.bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.rotate(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// rotate holds one mutation gate across authentication and replacement to prevent stale writes.
func (s *Store) rotate(ctx context.Context, name string) error {
	if err := acquire(ctx, s.gates[name]); err != nil {
		return err
	}
	defer func() { <-s.gates[name] }()
	value, index, err := s.get(ctx, name)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return nil
		}
		return err
	}
	if index == 0 {
		return nil
	}
	return s.write(ctx, name, s.bindings[name], value)
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
