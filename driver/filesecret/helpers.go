package filesecret

import (
	"context"
	"github.com/goforj/secret"
)

// validName keeps the independently released driver's grammar aligned with secret.
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

// lookup validates bounded application names before hashing caller input into the binding catalog.
func lookup[T any](ctx context.Context, name string, bindings map[string]T) (T, error) {
	var zero T
	if ctx == nil || !validName(name) {
		return zero, secret.ErrInvalid
	}
	value, known := bindings[name]
	if !known {
		return zero, secret.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return value, nil
}

// failure retains only a stable classification and the caller's context state.
func failure(ctx context.Context, kind error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return kind
}
