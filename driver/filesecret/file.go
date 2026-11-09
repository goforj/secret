// Package filesecret reads exact bytes beneath a caller-owned os.Root, including
// Docker secrets and Kubernetes projected volumes. It requires Go 1.25.12+,
// Go 1.26.5+, or stable Go 1.27+ for the os.Root confinement security fix.
package filesecret

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"io"
	"os"
	"runtime"
	"strings"
)

// MaxFileBytes bounds each read, including files that grow during a read.
const MaxFileBytes = 1 << 20

// Reader borrows its root and copies bindings. It supports concurrent reads.
type Reader struct {
	root     *os.Root
	bindings map[string]string
}

// New confines reads to root without taking ownership of its lifecycle.
func New(root *os.Root, bindings map[string]string) (*Reader, error) {
	if root == nil || len(bindings) == 0 || !supported() {
		return nil, secret.ErrInvalid
	}
	copy := make(map[string]string, len(bindings))
	for name, path := range bindings {
		if !validName(name) || !validPath(path) {
			return nil, secret.ErrInvalid
		}
		copy[name] = path
	}
	return &Reader{root: root, bindings: copy}, nil
}

// supported limits the security claim to platforms exercised by confinement tests.
func supported() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// validPath rejects traversal syntax while allowing contained projected-volume symlinks.
func validPath(path string) bool {
	if len(path) == 0 || len(path) > 4096 || strings.ContainsAny(path, "\\\x00:") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Get opens and validates the same descriptor, preserving all bytes without trimming.
func (r *Reader) Get(ctx context.Context, name string) (string, error) {
	path, err := lookup(ctx, name, r.bindings)
	if err != nil {
		return "", err
	}
	file, err := open(r.root, path)
	if err != nil {
		return "", fileFailure(ctx, err)
	}
	defer file.Close()
	return read(ctx, file)
}

// descriptor keeps read-failure tests on the same descriptor contract as production reads.
type descriptor interface {
	io.Reader
	Stat() (os.FileInfo, error)
}

// read rejects nonregular or growing files without exposing partial secret bytes.
func read(ctx context.Context, file descriptor) (string, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil || len(data) > MaxFileBytes {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(data), nil
}

// fileFailure discards path-bearing operating-system errors after classification.
func fileFailure(ctx context.Context, err error) error {
	kind := secret.ErrUnavailable
	if errors.Is(err, os.ErrNotExist) {
		kind = secret.ErrNotFound
	} else if errors.Is(err, os.ErrPermission) {
		kind = secret.ErrPermission
	}
	return failure(ctx, kind)
}
