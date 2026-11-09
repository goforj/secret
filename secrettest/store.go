// Package secrettest provides isolated, concurrency-safe secret fixtures for tests.
package secrettest

import (
	"context"
	"github.com/goforj/secret"
	"sync"
	"testing"
)

// store copies fixtures so application tests cannot mutate shared input maps.
type store struct {
	mu     sync.RWMutex
	values map[string]string
}

// New validates and copies public test fixtures. It never includes fixture values in failures.
// Set may create fixture entries; production drivers require provisioned resources.
func New(t testing.TB, values map[string]string) secret.Store {
	t.Helper()
	copy := make(map[string]string, len(values))
	for name, value := range values {
		if !validName(name) {
			t.Fatal("secrettest: invalid fixture name")
		}
		copy[name] = value
	}
	return &store{values: copy}
}

// Get returns the exact fixture value without exposing it in an error.
func (s *store) Get(ctx context.Context, name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return lookup(ctx, name, s.values)
}

// Set permits adding fixtures while retaining production name and context validation.
func (s *store) Set(ctx context.Context, name, value string) error {
	if err := request(ctx, name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[name] = value
	return nil
}

// Delete removes a fixture idempotently, including when it is already absent.
func (s *store) Delete(ctx context.Context, name string) error {
	if err := request(ctx, name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, name)
	return nil
}
