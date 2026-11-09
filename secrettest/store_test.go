package secrettest

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStore verifies independent fixtures, byte fidelity, validation, and concurrent mutation.
func TestStore(t *testing.T) {
	ctx := context.Background()
	fixtures := map[string]string{"KEY": "\x00\xff\n"}
	s := New(t, fixtures)
	fixtures["KEY"] = "changed"
	if value, err := s.Get(ctx, "KEY"); err != nil || value != "\x00\xff\n" {
		t.Fatal("fixture not copied")
	}
	for _, name := range []string{"", "bad", "_KEY", "A/B", strings.Repeat("A", 256)} {
		if _, err := s.Get(ctx, name); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal(err)
		}
		if err := s.Set(ctx, name, ""); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, name); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if err := s.Set(nil, "KEY", ""); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Delete(nil, "KEY"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Get(nil, "KEY"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "UNKNOWN"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Get(canceled, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Set(canceled, "KEY", ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Delete(canceled, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "EMPTY", ""); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Get(ctx, "EMPTY"); err != nil || value != "" {
		t.Fatal("empty value lost")
	}
	for range 2 {
		if err := s.Delete(ctx, "EMPTY"); err != nil {
			t.Fatal(err)
		}
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.Set(ctx, "KEY", "public-fixture")
			s.Get(ctx, "KEY")
			s.Delete(ctx, "KEY")
		}()
	}
	workers.Wait()
}

// invalidFixtureTB intercepts a fixture failure without adding secret values to test output.
type invalidFixtureTB struct{ testing.TB }

// Fatal turns the expected fixture rejection into a recoverable assertion.
func (invalidFixtureTB) Fatal(...any) { panic("fixture rejected") }

// TestInvalidFixture ensures constructors reject the entire invalid fixture.
func TestInvalidFixture(t *testing.T) {
	defer func() {
		if recover() != "fixture rejected" {
			t.Fatal("invalid fixture accepted")
		}
	}()
	New(invalidFixtureTB{t}, map[string]string{"invalid": "public-fixture"})
}

// observedContext signals when mutation validation has checked the original context state.
type observedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

// Err snapshots cancellation before signaling so the test cancels after the initial check.
func (c *observedContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

// TestStoreCanceledWhileWaiting prevents canceled fixture mutations from changing application state.
func TestStoreCanceledWhileWaiting(t *testing.T) {
	for _, method := range []string{"Set", "Delete"} {
		t.Run(method, func(t *testing.T) {
			s := New(t, map[string]string{"KEY": "public-initial"}).(*store)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &observedContext{Context: parent, checked: make(chan struct{})}
			result := make(chan error, 1)
			s.mu.Lock()
			go func() {
				if method == "Set" {
					result <- s.Set(ctx, "KEY", "public-replacement")
				} else {
					result <- s.Delete(ctx, "KEY")
				}
			}()
			<-ctx.checked
			cancel()
			s.mu.Unlock()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("queued mutation ignored cancellation")
				}
			case <-time.After(time.Second):
				t.Fatal("queued mutation did not finish")
			}
			if value, err := s.Get(context.Background(), "KEY"); err != nil || value != "public-initial" {
				t.Fatal("canceled mutation changed fixture state")
			}
		})
	}
}
