package azuresecret

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/goforj/secret"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeClient exposes only operations owned by the driver.
type fakeClient struct {
	get    func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
	set    func(context.Context, string, azsecrets.SetSecretParameters, *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error)
	delete func(context.Context, string, *azsecrets.DeleteSecretOptions) (azsecrets.DeleteSecretResponse, error)
}

// GetSecret exposes current-version reads and existence preflights.
func (c *fakeClient) GetSecret(ctx context.Context, name, version string, opts *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
	return c.get(ctx, name, version, opts)
}

// SetSecret exposes exact version payloads and authoritative metadata.
func (c *fakeClient) SetSecret(ctx context.Context, name string, in azsecrets.SetSecretParameters, opts *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error) {
	return c.set(ctx, name, in, opts)
}

// DeleteSecret exposes vault-configured whole-resource soft deletion.
func (c *fakeClient) DeleteSecret(ctx context.Context, name string, opts *azsecrets.DeleteSecretOptions) (azsecrets.DeleteSecretResponse, error) {
	return c.delete(ctx, name, opts)
}

// ptr provides fixture pointers without aliases or shared mutable metadata.
func ptr[T any](v T) *T { return &v }

// fixture creates a store with deterministic fixture policy.
func fixture(t *testing.T) (*Store, *fakeClient) {
	t.Helper()
	c := &fakeClient{}
	s, err := New(c, map[string]Binding{"KEY": {Name: "key"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

// expect checks stable classifications and rejects unsafe provider prose.
func expect(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("classification %v, want %v", err, want)
	}
	if err != nil && strings.Contains(err.Error(), "private-value") {
		t.Fatal("SDK error leaked")
	}
}

// providerError models Azure's actual nested conflict envelope.
func providerError(code int, body string) error {
	return &azcore.ResponseError{StatusCode: code, ErrorCode: "Conflict", RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body)), StatusCode: code}}
}

// TestConstructor rejects aliases and malformed metadata while deeply isolating trusted bindings.
func TestConstructor(t *testing.T) {
	c := &fakeClient{}
	for _, bindings := range []map[string]Binding{nil, {"bad": {Name: "key"}}, {"KEY": {Name: "bad/key"}}, {"KEY": {Name: "KEY"}, "OTHER": {Name: "key"}}, {"KEY": {Name: "key", ContentType: ptr(strings.Repeat("x", 256))}}, {"KEY": {Name: "key", ContentType: ptr("\xff")}}, {"KEY": {Name: "key", Tags: map[string]*string{"": ptr("x")}}}, {"KEY": {Name: "key", Tags: map[string]*string{"tag": nil}}}, {"KEY": {Name: "key", Tags: map[string]*string{"tag": ptr(strings.Repeat("x", 257))}}}, {"KEY": {Name: "key", NotBefore: ptr(time.Now()), Expires: ptr(time.Now().Add(-time.Hour))}}} {
		_, err := New(c, bindings)
		expect(t, err, secret.ErrInvalid)
	}
	tags := map[string]*string{}
	for i := range 16 {
		tags[string(rune('a'+i))] = ptr("x")
	}
	_, err := New(c, map[string]Binding{"KEY": {Name: "key", Tags: tags}})
	expect(t, err, secret.ErrInvalid)
	var missing *fakeClient
	_, err = New(missing, map[string]Binding{"KEY": {Name: "key"}})
	expect(t, err, secret.ErrInvalid)
	content := "text/plain"
	enabled := true
	start := time.Unix(100, 0).UTC()
	expiry := time.Unix(200, 0).UTC()
	tag := "original"
	bindings := map[string]Binding{"KEY": {Name: "key", ContentType: &content, Enabled: &enabled, NotBefore: &start, Expires: &expiry, Tags: map[string]*string{"tag": &tag}}}
	s, err := New(c, bindings)
	if err != nil {
		t.Fatal(err)
	}
	content = "changed"
	tag = "changed"
	enabled = false
	start = time.Time{}
	delete(bindings, "KEY")
	c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
		return azsecrets.GetSecretResponse{}, nil
	}
	c.set = func(_ context.Context, _ string, in azsecrets.SetSecretParameters, _ *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error) {
		if *in.ContentType != "text/plain" || *in.Tags["tag"] != "original" || !*in.SecretAttributes.Enabled || in.SecretAttributes.NotBefore.Unix() != 100 {
			t.Fatal("metadata not copied")
		}
		*in.Tags["tag"] = "client mutation"
		return azsecrets.SetSecretResponse{}, nil
	}
	for range 2 {
		if err := s.Set(context.Background(), "KEY", "x"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestGet checks exact text, malformed responses, every read classification, and cancellation.
func TestGet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	for _, code := range []int{401, 403, 404, 409, 429, 500} {
		c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
			return azsecrets.GetSecretResponse{}, providerError(code, "private-value")
		}
		want := secret.ErrUnavailable
		switch code {
		case 401, 403:
			want = secret.ErrPermission
		case 404:
			want = secret.ErrNotFound
		}
		_, err := s.Get(ctx, "KEY")
		expect(t, err, want)
	}
	for _, value := range []*string{nil, ptr("\xff"), ptr(strings.Repeat("x", MaxValueBytes+1))} {
		c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
			return azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: value}}, nil
		}
		_, err := s.Get(ctx, "KEY")
		expect(t, err, secret.ErrUnavailable)
	}
	for _, want := range []string{"", "\x00line\n", strings.Repeat("x", MaxValueBytes)} {
		c.get = func(got context.Context, name, version string, opts *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
			if got != ctx || name != "key" || version != "" || opts != nil {
				t.Fatal("latest selector changed")
			}
			return azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: &want}}, nil
		}
		if value, err := s.Get(ctx, "KEY"); err != nil || value != want {
			t.Fatal("text changed")
		}
	}
	_, err := s.Get(nil, "KEY")
	expect(t, err, secret.ErrInvalid)
	canceled, cancel := context.WithCancel(ctx)
	c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
		cancel()
		return azsecrets.GetSecretResponse{}, errors.New("private-value")
	}
	_, err = s.Get(canceled, "KEY")
	expect(t, err, context.Canceled)
}

// TestSet preserves existing-resource preflight and marks every post-dispatch error uncertain.
func TestSet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	expect(t, s.Set(nil, "KEY", "x"), secret.ErrInvalid)
	expect(t, s.Set(&stagedContext{Context: ctx}, "KEY", "x"), context.Canceled)
	for _, value := range []string{"\xff", strings.Repeat("x", MaxValueBytes+1)} {
		expect(t, s.Set(ctx, "KEY", value), secret.ErrInvalid)
	}
	c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
		return azsecrets.GetSecretResponse{}, providerError(404, "")
	}
	expect(t, s.Set(ctx, "KEY", "x"), secret.ErrNotFound)
	c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
		return azsecrets.GetSecretResponse{}, nil
	}
	for _, value := range []string{"", "\x00line\n", strings.Repeat("x", MaxValueBytes)} {
		c.set = func(got context.Context, name string, in azsecrets.SetSecretParameters, opts *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error) {
			if got != ctx || name != "key" || *in.Value != value || opts != nil {
				t.Fatal("payload changed")
			}
			return azsecrets.SetSecretResponse{}, nil
		}
		if err := s.Set(ctx, "KEY", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []int{400, 401, 403, 404, 409, 429, 500} {
		c.set = func(context.Context, string, azsecrets.SetSecretParameters, *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error) {
			return azsecrets.SetSecretResponse{}, providerError(code, "private-value")
		}
		expect(t, s.Set(ctx, "KEY", "x"), secret.ErrIndeterminate)
	}
	canceled, cancel := context.WithCancel(ctx)
	c.get = func(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
		cancel()
		return azsecrets.GetSecretResponse{}, nil
	}
	expect(t, s.Set(canceled, "KEY", "x"), context.Canceled)
}

// brokenBody exercises payload read failure without a network or response-data dependency.
type brokenBody struct{}

// Read exposes a deterministic bounded-body error.
func (brokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// Close satisfies the response lifecycle without owning any outside resource.
func (brokenBody) Close() error { return nil }

// TestDelete recognizes only the native nested deletion state and bounds untrusted body reads.
func TestDelete(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	expect(t, s.Delete(nil, "KEY"), secret.ErrInvalid)
	expect(t, s.Delete(&stagedContext{Context: ctx}, "KEY"), context.Canceled)
	cases := []struct{ cause, want error }{
		{nil, nil}, {providerError(404, ""), nil}, {providerError(409, `{"error":{"innererror":{"code":"ObjectIsBeingDeleted"}}}`), nil},
		{providerError(409, `{"error":{"code":"ObjectIsBeingDeleted"}}`), secret.ErrIndeterminate},
		{providerError(409, `{"error":{"innererror":{"code":"Other"}}}`), secret.ErrIndeterminate},
		{providerError(409, `{"error":{"innererror":{"code":"objectisbeingdeleted"}}}`), secret.ErrIndeterminate},
		{providerError(409, `{"innererror":{"code":"ObjectIsBeingDeleted"}}`), secret.ErrIndeterminate},
		{providerError(409, "invalid json"), secret.ErrIndeterminate}, {providerError(409, strings.Repeat("x", 65537)), secret.ErrIndeterminate},
		{&azcore.ResponseError{StatusCode: 409}, secret.ErrIndeterminate}, {&azcore.ResponseError{StatusCode: 409, RawResponse: &http.Response{}}, secret.ErrIndeterminate},
		{&azcore.ResponseError{StatusCode: 409, RawResponse: &http.Response{Body: brokenBody{}}}, secret.ErrIndeterminate},
		{errors.New("private-value"), secret.ErrIndeterminate}, {providerError(403, "private-value"), secret.ErrIndeterminate},
	}
	for _, test := range cases {
		c.delete = func(got context.Context, name string, opts *azsecrets.DeleteSecretOptions) (azsecrets.DeleteSecretResponse, error) {
			if got != ctx || name != "key" || opts != nil {
				t.Fatal("delete changed")
			}
			return azsecrets.DeleteSecretResponse{}, test.cause
		}
		expect(t, s.Delete(ctx, "KEY"), test.want)
	}
}
