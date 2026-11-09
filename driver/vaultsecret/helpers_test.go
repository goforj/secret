package vaultsecret

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
		if err := request(context.Background(), name, true); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid name accepted")
		}
	}
	for _, name := range []string{"A", "A_1", strings.Repeat("A", 255)} {
		if err := request(context.Background(), name, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := request(nil, "KEY", true); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if err := request(context.Background(), "UNKNOWN", false); !errors.Is(err, secret.ErrNotFound) || strings.Contains(err.Error(), "UNKNOWN") {
		t.Fatal("unknown name disclosed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := request(ctx, "UNKNOWN", false); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal(err)
	}
	if err := request(ctx, "KEY", true); !errors.Is(err, context.Canceled) {
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
	if err := request(deadline, "KEY", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// TestMutationGate verifies cancellation does not turn queued work into an uncertain mutation.
func TestMutationGate(t *testing.T) {
	for _, value := range []any{nil, (*int)(nil), map[string]string(nil), []int(nil), (func())(nil), (chan int)(nil)} {
		if !nilClient(value) {
			t.Fatal("nil dependency accepted")
		}
	}
	if nilClient(struct{}{}) || nilClient(new(int)) {
		t.Fatal("valid dependency rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gate := make(chan struct{}, 1)
	if err := acquire(ctx, gate); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := acquire(context.Background(), gate); err != nil {
		t.Fatal(err)
	}
	waiting, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- acquire(waiting, gate) }()
	stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, secret.ErrIndeterminate) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter blocked")
	}
	<-gate
	if err := mutation(ctx); !errors.Is(err, secret.ErrIndeterminate) || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := mutation(context.Background()); !errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal(err)
	}
}

// stagedContext ends between gate acquisition and dispatch, freezing the second cancellation check.
type stagedContext struct {
	context.Context
	calls int
}

// Err simulates cancellation after the initial nonblocking context check.
func (c *stagedContext) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

// TestGateCancellationAfterAcquire requires releasing a gate acquired just as cancellation occurs.
func TestGateCancellationAfterAcquire(t *testing.T) {
	gate := make(chan struct{}, 1)
	ctx := &stagedContext{Context: context.Background()}
	if err := acquire(ctx, gate); !errors.Is(err, context.Canceled) || len(gate) != 0 {
		t.Fatal("canceled gate retained")
	}
}

// TestGateAlreadyCanceledSelection covers cancellation selected while a binding remains occupied.
func TestGateAlreadyCanceledSelection(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &stagedContext{Context: canceled}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	if err := acquire(ctx, gate); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
