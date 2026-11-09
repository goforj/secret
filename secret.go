// Package secret provides small capabilities for reading and replacing bound secret values.
// Provider locations and lifecycle policy belong to trusted construction, not callers.
package secret

import (
	"context"
	"errors"
)

// ErrInvalid identifies invalid input or construction policy without echoing caller data.
var ErrInvalid = errors.New("secret: invalid request")

// ErrNotFound identifies an unknown binding or an unavailable current value.
var ErrNotFound = errors.New("secret: not found")

// ErrPermission identifies denied access before a mutation may have dispatched.
var ErrPermission = errors.New("secret: access denied")

// ErrUnavailable identifies a definite read, preflight, or source failure.
var ErrUnavailable = errors.New("secret: source unavailable")

// ErrIndeterminate means a mutation may have committed and requires reconciliation.
var ErrIndeterminate = errors.New("secret: mutation outcome unknown")

// Reader retrieves exact values by application name. Consumers normally need only this capability.
type Reader interface {
	Get(context.Context, string) (string, error)
}

// Writer publishes a replacement at an already provisioned binding.
type Writer interface {
	Set(context.Context, string, string) error
}

// Deleter requests whole-resource deletion under the provider's retention policy.
type Deleter interface {
	Delete(context.Context, string) error
}

// Store combines read and mutation capabilities. Narrow interfaces do not replace provider authorization.
type Store interface {
	Reader
	Writer
	Deleter
}

// Func adapts an application-owned read function. Its errors are not sanitized by this package.
type Func func(context.Context, string) (string, error)

// Get validates the request before invoking f. A nil Func is invalid wiring and panics.
func (f Func) Get(ctx context.Context, name string) (string, error) {
	if f == nil {
		panic("secret: nil Func")
	}
	if ctx == nil || !validName(name) {
		return "", ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f(ctx, name)
}

// validName keeps application names bounded and independent of provider path syntax.
func validName(name string) bool {
	if len(name) == 0 || len(name) > 255 || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for _, c := range []byte(name) {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
