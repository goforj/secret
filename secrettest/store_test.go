package secrettest

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"strings"
	"sync"
	"testing"
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
