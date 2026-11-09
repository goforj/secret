package gcpsecret

import (
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"context"
	"errors"
	"github.com/goforj/secret"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"hash/crc32"
	"strings"
	"testing"
)

// fakeClient records typed SDK requests and per-call retry policy.
type fakeClient struct {
	access func(context.Context, *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error)
	get    func(context.Context, *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error)
	add    func(context.Context, *secretmanagerpb.AddSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.SecretVersion, error)
	delete func(context.Context, *secretmanagerpb.DeleteSecretRequest) error
}

// AccessSecretVersion exposes read responses for integrity and current-version tests.
func (c *fakeClient) AccessSecretVersion(ctx context.Context, in *secretmanagerpb.AccessSecretVersionRequest, _ ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	return c.access(ctx, in)
}

// GetSecret exposes metadata preflight responses.
func (c *fakeClient) GetSecret(ctx context.Context, in *secretmanagerpb.GetSecretRequest, _ ...gax.CallOption) (*secretmanagerpb.Secret, error) {
	return c.get(ctx, in)
}

// AddSecretVersion exposes uncertain writes and retry options.
func (c *fakeClient) AddSecretVersion(ctx context.Context, in *secretmanagerpb.AddSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.SecretVersion, error) {
	return c.add(ctx, in, opts...)
}

// DeleteSecret exposes ETag-protected resource deletion.
func (c *fakeClient) DeleteSecret(ctx context.Context, in *secretmanagerpb.DeleteSecretRequest, _ ...gax.CallOption) error {
	return c.delete(ctx, in)
}

// fixture supplies an isolated client and bound resource.
func fixture(t *testing.T) (*Store, *fakeClient) {
	t.Helper()
	c := &fakeClient{}
	s, err := New(c, "projects/test", map[string]string{"KEY": "key"})
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

// expect rejects provider-error disclosure while asserting classifications.
func expect(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("classification %v, want %v", err, want)
	}
	if err != nil && strings.Contains(err.Error(), "private-value") {
		t.Fatal("provider error leaked")
	}
}

// payload creates a valid checksum for conspicuously public fixture bytes.
func payload(value string) *secretmanagerpb.AccessSecretVersionResponse {
	data := []byte(value)
	checksum := int64(crc32.Checksum(data, checksumTable))
	return &secretmanagerpb.AccessSecretVersionResponse{Payload: &secretmanagerpb.SecretPayload{Data: data, DataCrc32C: &checksum}}
}

// TestConstructor freezes regional names, copied bindings, aliases, and nil dependencies.
func TestConstructor(t *testing.T) {
	c := &fakeClient{}
	for _, parent := range []string{"", "projects/", "projects/a/locations/", "projects/a/extra/b", strings.Repeat("x", 1025)} {
		_, err := New(c, parent, map[string]string{"KEY": "key"})
		expect(t, err, secret.ErrInvalid)
	}
	for _, bindings := range []map[string]string{nil, {"bad": "key"}, {"KEY": "../key"}, {"KEY": strings.Repeat("a", 256)}, {"KEY": "key", "OTHER": "key"}} {
		_, err := New(c, "projects/a", bindings)
		expect(t, err, secret.ErrInvalid)
	}
	var missing *fakeClient
	_, err := New(missing, "projects/a", map[string]string{"KEY": "key"})
	expect(t, err, secret.ErrInvalid)
	bindings := map[string]string{"KEY": "key"}
	s, err := New(c, "projects/a/locations/europe-west1", bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings["KEY"] = "changed"
	if s.bindings["KEY"].resource != "projects/a/locations/europe-west1/secrets/key" {
		t.Fatal("binding changed")
	}
}

// TestGet checks integrity, limits, exact byte preservation, cancellation, and every gRPC code.
func TestGet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	for _, code := range []codes.Code{codes.NotFound, codes.FailedPrecondition, codes.Unauthenticated, codes.PermissionDenied, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Unknown} {
		c.access = func(context.Context, *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
			return nil, status.Error(code, "private-value locator")
		}
		want := secret.ErrUnavailable
		if code == codes.NotFound || code == codes.FailedPrecondition {
			want = secret.ErrNotFound
		}
		if code == codes.Unauthenticated || code == codes.PermissionDenied {
			want = secret.ErrPermission
		}
		_, err := s.Get(ctx, "KEY")
		expect(t, err, want)
	}
	bad := payload("x")
	*bad.Payload.DataCrc32C = 0
	for _, out := range []*secretmanagerpb.AccessSecretVersionResponse{nil, {}, {Payload: &secretmanagerpb.SecretPayload{}}, bad, payload(strings.Repeat("x", MaxValueBytes+1))} {
		c.access = func(context.Context, *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
			return out, nil
		}
		value, err := s.Get(ctx, "KEY")
		expect(t, err, secret.ErrUnavailable)
		if value != "" {
			t.Fatal("malformed data returned")
		}
	}
	for _, want := range []string{"", "\x00\xff\n", strings.Repeat("x", MaxValueBytes)} {
		c.access = func(got context.Context, in *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
			if got != ctx || in.Name != "projects/test/secrets/key/versions/latest" {
				t.Fatal("wrong current selector")
			}
			return payload(want), nil
		}
		if value, err := s.Get(ctx, "KEY"); err != nil || value != want {
			t.Fatal("bytes changed")
		}
	}
	_, err := s.Get(nil, "KEY")
	expect(t, err, secret.ErrInvalid)
	canceled, cancel := context.WithCancel(ctx)
	c.access = func(context.Context, *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
		cancel()
		return nil, status.Error(codes.Unavailable, "private-value")
	}
	_, err = s.Get(canceled, "KEY")
	expect(t, err, context.Canceled)
}

// TestSet verifies one AddSecretVersion call, disabled automatic retries, and indeterminate commits.
func TestSet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	expect(t, s.Set(nil, "KEY", "x"), secret.ErrInvalid)
	expect(t, s.Set(ctx, "KEY", strings.Repeat("x", MaxValueBytes+1)), secret.ErrInvalid)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	expect(t, s.Set(canceled, "KEY", "x"), context.Canceled)
	for _, value := range []string{"", "\x00\xff\n", strings.Repeat("x", MaxValueBytes)} {
		c.add = func(got context.Context, in *secretmanagerpb.AddSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.SecretVersion, error) {
			if got != ctx || in.Parent != "projects/test/secrets/key" || string(in.Payload.Data) != value || *in.Payload.DataCrc32C != int64(crc32.Checksum([]byte(value), checksumTable)) {
				t.Fatal("write changed")
			}
			settings := gax.CallSettings{}
			for _, option := range opts {
				option.Resolve(&settings)
			}
			if settings.Retry == nil || settings.Retry() != nil {
				t.Fatal("unsafe automatic retry")
			}
			return nil, nil
		}
		if err := s.Set(ctx, "KEY", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []codes.Code{codes.NotFound, codes.PermissionDenied, codes.ResourceExhausted, codes.DeadlineExceeded, codes.Unavailable, codes.FailedPrecondition} {
		c.add = func(context.Context, *secretmanagerpb.AddSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.SecretVersion, error) {
			return nil, status.Error(code, "private-value")
		}
		want := secret.ErrIndeterminate
		if code == codes.NotFound {
			want = secret.ErrNotFound
		}
		expect(t, s.Set(ctx, "KEY", "x"), want)
	}
}

// TestDelete requires a usable ETag and distinguishes definite conflicts from uncertainty.
func TestDelete(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	expect(t, s.Delete(nil, "KEY"), secret.ErrInvalid)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	expect(t, s.Delete(canceled, "KEY"), context.Canceled)
	for _, test := range []struct {
		out       *secretmanagerpb.Secret
		err, want error
	}{{nil, nil, secret.ErrUnavailable}, {&secretmanagerpb.Secret{}, nil, secret.ErrUnavailable}, {nil, status.Error(codes.NotFound, "private-value"), nil}, {nil, status.Error(codes.PermissionDenied, "private-value"), secret.ErrPermission}, {nil, status.Error(codes.FailedPrecondition, "private-value"), secret.ErrUnavailable}} {
		c.get = func(context.Context, *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return test.out, test.err
		}
		expect(t, s.Delete(ctx, "KEY"), test.want)
	}
	for _, code := range []codes.Code{codes.OK, codes.NotFound, codes.FailedPrecondition, codes.PermissionDenied, codes.Unavailable} {
		c.get = func(context.Context, *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{Etag: "etag"}, nil
		}
		c.delete = func(got context.Context, in *secretmanagerpb.DeleteSecretRequest) error {
			if got != ctx || in.Name != "projects/test/secrets/key" || in.Etag != "etag" {
				t.Fatal("delete lost identity")
			}
			return status.Error(code, "private-value")
		}
		want := secret.ErrIndeterminate
		switch code {
		case codes.OK, codes.NotFound:
			want = nil
		case codes.FailedPrecondition:
			want = secret.ErrUnavailable
		}
		expect(t, s.Delete(ctx, "KEY"), want)
	}
	canceled, cancel = context.WithCancel(ctx)
	c.get = func(context.Context, *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
		cancel()
		return &secretmanagerpb.Secret{Etag: "etag"}, nil
	}
	expect(t, s.Delete(canceled, "KEY"), context.Canceled)
}

// TestMutationCanceledBeforeGate freezes validation-to-lock cancellation races.
func TestMutationCanceledBeforeGate(t *testing.T) {
	s, _ := fixture(t)
	expect(t, s.Set(&stagedContext{Context: context.Background()}, "KEY", "x"), context.Canceled)
	expect(t, s.Delete(&stagedContext{Context: context.Background()}, "KEY"), context.Canceled)
}
