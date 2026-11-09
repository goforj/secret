package secret

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestFunc validates name grammar, cancellation, byte fidelity, and application error ownership.
func TestFunc(t *testing.T) {
	calls := 0
	f := Func(func(ctx context.Context, name string) (string, error) { calls++; return "\x00\xff\n", nil })
	for _, name := range []string{"", "lower", "_A", "A-B", "A/B", "A\x00", strings.Repeat("A", 256), "É"} {
		if _, err := f.Get(context.Background(), name); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid name accepted")
		}
	}
	if _, err := f.Get(nil, "KEY"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Get(ctx, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("invalid requests dispatched")
	}
	for _, name := range []string{"KEY", "A_1", strings.Repeat("A", 255)} {
		if value, err := f.Get(context.Background(), name); err != nil || value != "\x00\xff\n" {
			t.Fatal("bytes changed")
		}
	}
	cause := errors.New("application-owned error")
	custom := Func(func(context.Context, string) (string, error) { return "", cause })
	if _, err := custom.Get(context.Background(), "KEY"); err != cause {
		t.Fatal("application error changed")
	}
	defer func() {
		if recover() != "secret: nil Func" {
			t.Fatal("nil Func must panic")
		}
	}()
	var missing Func
	missing.Get(nil, "bad")
}

// TestFuncConcurrency demonstrates that the adapter does not serialize an application's safe reader.
func TestFuncConcurrency(t *testing.T) {
	f := Func(func(context.Context, string) (string, error) { return "public-fixture", nil })
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := f.Get(context.Background(), "KEY"); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
}
