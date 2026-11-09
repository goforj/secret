package vaultsecret

import (
	"context"
	"errors"
	"fmt"
	"github.com/goforj/secret"
	"github.com/hashicorp/vault/api"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClient models scalar documents and durable ownership separately.
type fakeClient struct {
	get      func(context.Context, string) (*api.KVSecret, error)
	metadata func(context.Context, string) (*api.KVMetadata, error)
	put      func(context.Context, string, map[string]interface{}, ...api.KVOption) (*api.KVSecret, error)
	delete   func(context.Context, string) error
}

// Get exposes the latest-version seam.
func (c *fakeClient) Get(ctx context.Context, path string) (*api.KVSecret, error) {
	return c.get(ctx, path)
}

// GetMetadata exposes ownership evidence independent of readable data.
func (c *fakeClient) GetMetadata(ctx context.Context, path string) (*api.KVMetadata, error) {
	return c.metadata(ctx, path)
}

// Put exposes CAS mutation outcomes.
func (c *fakeClient) Put(ctx context.Context, path string, data map[string]interface{}, opts ...api.KVOption) (*api.KVSecret, error) {
	return c.put(ctx, path, data, opts...)
}

// DeleteMetadata exposes whole-path destruction outcomes.
func (c *fakeClient) DeleteMetadata(ctx context.Context, path string) error {
	return c.delete(ctx, path)
}

// fixture constructs a private bound store and records only public fixture data.
func fixture(t *testing.T) (*Store, *fakeClient) {
	t.Helper()
	c := &fakeClient{}
	s, err := New(c, map[string]string{"KEY": "app/key"})
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

// expect requires safe error graphs while preserving actionable classifications.
func expect(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("classification %v, want %v", err, want)
	}
	if err != nil && strings.Contains(err.Error(), "private-value") {
		t.Fatal("provider data disclosed")
	}
}

// active creates a scalar document with CAS evidence.
func active(value string) *api.KVSecret {
	return &api.KVSecret{Data: map[string]interface{}{"value": value}, VersionMetadata: &api.KVVersionMetadata{Version: 3}}
}

// ownership creates the exact durable format marker required for destructive operations.
func ownership() *api.KVMetadata {
	return &api.KVMetadata{CustomMetadata: map[string]interface{}{"goforj-secret-format": "scalar-v1"}}
}

// providerError deliberately includes unsafe prose that the driver must discard.
func providerError(code int) error {
	return &api.ResponseError{StatusCode: code, Errors: []string{"private-value locator credential"}}
}

// TestConstructor rejects aliases, invalid names and paths, and nil dependencies.
func TestConstructor(t *testing.T) {
	c := &fakeClient{}
	for _, bindings := range []map[string]string{nil, {"bad": "key"}, {"KEY": "key", "OTHER": "key"}} {
		_, err := New(c, bindings)
		expect(t, err, secret.ErrInvalid)
	}
	for _, path := range []string{"", "/key", "a/", "a//b", "a/./b", "a/../b", "a\x00b", "a\\b", strings.Repeat("x", 1025)} {
		_, err := New(c, map[string]string{"KEY": path})
		expect(t, err, secret.ErrInvalid)
	}
	var missing *fakeClient
	_, err := New(missing, map[string]string{"KEY": "key"})
	expect(t, err, secret.ErrInvalid)
	bindings := map[string]string{"KEY": "key"}
	s, err := New(c, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings["KEY"] = "changed"
	if s.bindings["KEY"].path != "key" {
		t.Fatal("binding not copied")
	}
}

// TestGet freezes wrapped absence handling, strict document shape, byte fidelity, and limits.
func TestGet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	for _, test := range []struct{ err, want error }{{fmt.Errorf("private-value: %w", api.ErrSecretNotFound), secret.ErrNotFound}, {providerError(404), secret.ErrNotFound}, {providerError(401), secret.ErrPermission}, {providerError(403), secret.ErrPermission}, {providerError(429), secret.ErrUnavailable}, {providerError(500), secret.ErrUnavailable}, {errors.New("private-value"), secret.ErrUnavailable}} {
		c.get = func(context.Context, string) (*api.KVSecret, error) { return nil, test.err }
		_, err := s.Get(ctx, "KEY")
		expect(t, err, test.want)
		if errors.Is(err, test.err) {
			t.Fatal("provider error retained")
		}
	}
	for _, test := range []struct {
		out  *api.KVSecret
		want error
	}{{nil, secret.ErrNotFound}, {&api.KVSecret{}, secret.ErrNotFound}, {&api.KVSecret{Data: map[string]interface{}{}}, secret.ErrUnavailable}, {&api.KVSecret{Data: map[string]interface{}{"value": 3}}, secret.ErrUnavailable}, {&api.KVSecret{Data: map[string]interface{}{"value": "x", "shared": "x"}}, secret.ErrUnavailable}, {active(strings.Repeat("x", MaxValueBytes+1)), secret.ErrUnavailable}, {active("\xff"), secret.ErrUnavailable}} {
		c.get = func(context.Context, string) (*api.KVSecret, error) { return test.out, nil }
		value, err := s.Get(ctx, "KEY")
		expect(t, err, test.want)
		if value != "" {
			t.Fatal("invalid document returned")
		}
	}
	for _, want := range []string{"", "\x00line\n", strings.Repeat("x", MaxValueBytes)} {
		c.get = func(got context.Context, path string) (*api.KVSecret, error) {
			if got != ctx || path != "app/key" {
				t.Fatal("dispatch changed")
			}
			return active(want), nil
		}
		if value, err := s.Get(ctx, "KEY"); err != nil || value != want {
			t.Fatal("bytes changed")
		}
	}
	_, err := s.Get(nil, "KEY")
	expect(t, err, secret.ErrInvalid)
	canceled, cancel := context.WithCancel(ctx)
	c.get = func(context.Context, string) (*api.KVSecret, error) { cancel(); return nil, providerError(500) }
	_, err = s.Get(canceled, "KEY")
	expect(t, err, context.Canceled)
}

// TestSet requires current data, version evidence, and ownership before one exact CAS Put.
func TestSet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	expect(t, s.Set(nil, "KEY", "x"), secret.ErrInvalid)
	for _, value := range []string{strings.Repeat("x", MaxValueBytes+1), "\xff"} {
		expect(t, s.Set(ctx, "KEY", value), secret.ErrInvalid)
	}
	expect(t, s.Set(&stagedContext{Context: ctx}, "KEY", "x"), context.Canceled)
	for _, test := range []struct {
		out       *api.KVSecret
		err, want error
	}{{nil, api.ErrSecretNotFound, secret.ErrNotFound}, {nil, nil, secret.ErrNotFound}, {&api.KVSecret{Data: map[string]interface{}{"value": "x"}}, nil, secret.ErrUnavailable}} {
		c.get = func(context.Context, string) (*api.KVSecret, error) { return test.out, test.err }
		expect(t, s.Set(ctx, "KEY", "x"), test.want)
	}
	c.get = func(context.Context, string) (*api.KVSecret, error) { return active("prior"), nil }
	for _, test := range []struct {
		out       *api.KVMetadata
		err, want error
	}{{nil, providerError(403), secret.ErrPermission}, {nil, nil, secret.ErrUnavailable}, {&api.KVMetadata{}, nil, secret.ErrUnavailable}} {
		c.metadata = func(context.Context, string) (*api.KVMetadata, error) { return test.out, test.err }
		expect(t, s.Set(ctx, "KEY", "x"), test.want)
	}
	c.metadata = func(context.Context, string) (*api.KVMetadata, error) { return ownership(), nil }
	for _, value := range []string{"", "\x00line\n", strings.Repeat("x", MaxValueBytes)} {
		c.put = func(got context.Context, path string, data map[string]interface{}, opts ...api.KVOption) (*api.KVSecret, error) {
			if got != ctx || path != "app/key" || len(data) != 1 || data["value"] != value || len(opts) != 1 {
				t.Fatal("CAS mutation changed")
			}
			return nil, nil
		}
		if err := s.Set(ctx, "KEY", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, cause := range []error{api.ErrSecretNotFound, providerError(400), providerError(403), providerError(413), providerError(500)} {
		c.put = func(context.Context, string, map[string]interface{}, ...api.KVOption) (*api.KVSecret, error) {
			return nil, cause
		}
		err := s.Set(ctx, "KEY", "x")
		expect(t, err, secret.ErrIndeterminate)
		if errors.Is(err, secret.ErrUnavailable) || errors.Is(err, cause) {
			t.Fatal("uncertain write misclassified")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	c.metadata = func(context.Context, string) (*api.KVMetadata, error) { cancel(); return ownership(), nil }
	expect(t, s.Set(canceled, "KEY", "x"), context.Canceled)
}

// TestDelete distinguishes absent resources, owned deleted data, and unsafe shared documents.
func TestDelete(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	expect(t, s.Delete(nil, "KEY"), secret.ErrInvalid)
	expect(t, s.Delete(&stagedContext{Context: ctx}, "KEY"), context.Canceled)
	for _, test := range []struct {
		out       *api.KVMetadata
		err, want error
	}{{nil, api.ErrSecretNotFound, nil}, {nil, providerError(404), nil}, {nil, providerError(403), secret.ErrPermission}, {nil, nil, secret.ErrUnavailable}, {&api.KVMetadata{}, nil, secret.ErrUnavailable}} {
		c.metadata = func(context.Context, string) (*api.KVMetadata, error) { return test.out, test.err }
		expect(t, s.Delete(ctx, "KEY"), test.want)
	}
	c.metadata = func(context.Context, string) (*api.KVMetadata, error) { return ownership(), nil }
	for _, test := range []struct {
		out       *api.KVSecret
		err, want error
	}{{nil, nil, secret.ErrUnavailable}, {nil, providerError(403), secret.ErrPermission}, {active("\xff"), nil, secret.ErrUnavailable}} {
		c.get = func(context.Context, string) (*api.KVSecret, error) { return test.out, test.err }
		expect(t, s.Delete(ctx, "KEY"), test.want)
	}
	for _, out := range []*api.KVSecret{active("x"), {Data: nil}} {
		c.get = func(context.Context, string) (*api.KVSecret, error) { return out, nil }
		for _, cause := range []error{nil, providerError(404), api.ErrSecretNotFound, providerError(403), providerError(500)} {
			c.delete = func(context.Context, string) error { return cause }
			want := secret.ErrIndeterminate
			if cause == nil || errors.As(cause, new(*api.ResponseError)) && cause.(*api.ResponseError).StatusCode == 404 {
				want = nil
			}
			expect(t, s.Delete(ctx, "KEY"), want)
		}
	}
	c.get = func(context.Context, string) (*api.KVSecret, error) { return nil, api.ErrSecretNotFound }
	c.delete = func(context.Context, string) error { return nil }
	if err := s.Delete(ctx, "KEY"); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	c.get = func(context.Context, string) (*api.KVSecret, error) { cancel(); return active("x"), nil }
	expect(t, s.Delete(canceled, "KEY"), context.Canceled)
}

// TestSDKMissingTranslation verifies the real SDK's locator-bearing wrapped sentinel is sanitized.
func TestSDKMissingTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		fmt.Fprint(w, `{"errors":[]}`)
	}))
	defer server.Close()
	config := api.DefaultConfig()
	config.Address = server.URL
	client, err := api.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(client.KVv2("secret"), map[string]string{"KEY": "private-locator"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(context.Background(), "KEY")
	expect(t, err, secret.ErrNotFound)
	if strings.Contains(err.Error(), "private-locator") {
		t.Fatal("locator disclosed")
	}
}

// TestStoreMutationConcurrency proves canceled waiters make no SDK call and other bindings remain independent.
func TestStoreMutationConcurrency(t *testing.T) {
	c := &fakeClient{}
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var once sync.Once
	var calls atomic.Int32
	s, err := New(c, map[string]string{"KEY": "key", "OTHER": "other"})
	c.get = func(ctx context.Context, path string) (*api.KVSecret, error) {
		if path == "key" {
			calls.Add(1)
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return active("public-fixture"), nil
	}
	c.metadata = func(context.Context, string) (*api.KVMetadata, error) { return ownership(), nil }
	c.put = func(context.Context, string, map[string]interface{}, ...api.KVOption) (*api.KVSecret, error) {
		return nil, nil
	}

	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- s.Set(ctx, "KEY", "public-replacement") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first mutation did not enter provider")
	}
	waiting, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := s.Delete(waiting, "KEY"); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal("canceled waiter dispatched", err)
	}
	if calls.Load() != 1 {
		t.Fatal("same-binding waiter reached provider")
	}
	if err := s.Set(ctx, "OTHER", "public-replacement"); err != nil {
		t.Fatal("independent binding blocked", err)
	}
	// Cancel the first request so the blocked fixture exits without leaving a goroutine.
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("in-flight context lost", err)
	}
}
