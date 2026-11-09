package secrettest

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

// request prevents invalid, unknown, or canceled requests from reaching a provider.
func request(ctx context.Context, name string, known bool) error {
	if ctx == nil || !validName(name) {
		return secret.ErrInvalid
	}
	if !known {
		return secret.ErrNotFound
	}
	return ctx.Err()
}

// failure retains only a stable classification and the caller's context state.
func failure(ctx context.Context, kind error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return kind
}
