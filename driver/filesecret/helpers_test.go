package filesecret

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"strings"
	"testing"
	"time"
)

// TestRequestContract freezes validation precedence and safe context normalization independently per module.
func TestRequestContract(t *testing.T) {
	for _, name := range []string{"", "lower", "_A", "A-B", "A/B", "A\x00", strings.Repeat("A", 256), "É"} {
		if err := lookupError(context.Background(), name, true); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid name accepted")
		}
	}
	for _, name := range []string{"A", "A_1", strings.Repeat("A", 255)} {
		if err := lookupError(context.Background(), name, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := lookupError(nil, "KEY", true); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if err := lookupError(context.Background(), "UNKNOWN", false); !errors.Is(err, secret.ErrNotFound) || strings.Contains(err.Error(), "UNKNOWN") {
		t.Fatal("unknown name disclosed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := lookupError(ctx, "UNKNOWN", false); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal(err)
	}
	if err := lookupError(ctx, "KEY", true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := failure(ctx, secret.ErrUnavailable); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := failure(context.Background(), secret.ErrUnavailable); err != secret.ErrUnavailable {
		t.Fatal(err)
	}
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if err := lookupError(deadline, "KEY", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// lookupError drives the actual generic lookup contract with present and absent test bindings.
func lookupError(ctx context.Context, name string, known bool) error {
	bindings := map[string]int{}
	if known {
		bindings[name] = 1
	}
	_, err := lookup(ctx, name, bindings)
	return err
}
