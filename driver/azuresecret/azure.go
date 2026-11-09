// Package azuresecret binds values to one caller-configured Azure Key Vault.
// Mutations require exclusive lifecycle ownership because Azure Set has no conditional create guard.
package azuresecret

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/goforj/secret"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxValueBytes bounds UTF-8 values at Azure's documented 25 KB limit.
const MaxValueBytes = 25000

// Binding contains authoritative metadata for every newly published version.
type Binding struct {
	Name        string
	ContentType *string
	Enabled     *bool
	NotBefore   *time.Time
	Expires     *time.Time
	Tags        map[string]*string
}

// Client is implemented by *azsecrets.Client.
type Client interface {
	GetSecret(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
	SetSecret(context.Context, string, azsecrets.SetSecretParameters, *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error)
	DeleteSecret(context.Context, string, *azsecrets.DeleteSecretOptions) (azsecrets.DeleteSecretResponse, error)
}

// binding holds a deep-copied version policy and a per-resource gate.
type binding struct {
	Binding
	gate chan struct{}
}

// Store never closes or reconfigures the supplied concurrency-safe client.
type Store struct {
	client   Client
	bindings map[string]*binding
}

var validID = regexp.MustCompile(`^[A-Za-z0-9-]{1,127}$`)

// clone isolates optional metadata pointers from application mutations.
func clone[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// New validates unique case-insensitive names and copies all version metadata.
func New(client Client, bindings map[string]Binding) (*Store, error) {
	if nilClient(client) || len(bindings) == 0 {
		return nil, secret.ErrInvalid
	}
	copy := make(map[string]*binding, len(bindings))
	seen := make(map[string]bool, len(bindings))
	for name, b := range bindings {
		id := strings.ToLower(b.Name)
		if !validName(name) || !validID.MatchString(b.Name) || seen[id] || !validMetadata(b) {
			return nil, secret.ErrInvalid
		}
		seen[id] = true
		b.ContentType = clone(b.ContentType)
		b.Enabled = clone(b.Enabled)
		b.NotBefore = clone(b.NotBefore)
		b.Expires = clone(b.Expires)
		tags := make(map[string]*string, len(b.Tags))
		for k, v := range b.Tags {
			tags[k] = clone(v)
		}
		b.Tags = tags
		copy[name] = &binding{Binding: b, gate: make(chan struct{}, 1)}
	}
	return &Store{client: client, bindings: copy}, nil
}

// validMetadata rejects values Azure cannot represent before retaining construction policy.
func validMetadata(b Binding) bool {
	if b.ContentType != nil && (!utf8.ValidString(*b.ContentType) || len(*b.ContentType) > 255) {
		return false
	}
	if len(b.Tags) > 15 {
		return false
	}
	for k, v := range b.Tags {
		if len(k) == 0 || len(k) > 512 || !utf8.ValidString(k) || v == nil || len(*v) > 256 || !utf8.ValidString(*v) {
			return false
		}
	}
	if b.NotBefore != nil && b.Expires != nil && !b.NotBefore.Before(*b.Expires) {
		return false
	}
	return true
}

// readError discards SDK errors after examining their HTTP status.
func readError(ctx context.Context, err error) error {
	kind := secret.ErrUnavailable
	var response *azcore.ResponseError
	if errors.As(err, &response) {
		switch response.StatusCode {
		case 404:
			kind = secret.ErrNotFound
		case 401, 403:
			kind = secret.ErrPermission
		}
	}
	return failure(ctx, kind)
}

// Get requests the latest version and rejects malformed or oversized text.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return "", err
	}
	value, err := s.client.GetSecret(ctx, b.Name, "", nil)
	if err != nil {
		return "", readError(ctx, err)
	}
	if value.Value == nil || len(*value.Value) > MaxValueBytes || !utf8.ValidString(*value.Value) {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	return *value.Value, nil
}

// Set checks existence before publishing authoritative binding metadata.
// External delete/purge/recreate races require exclusive lifecycle ownership.
func (s *Store) Set(ctx context.Context, name, value string) error {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if len(value) > MaxValueBytes || !utf8.ValidString(value) {
		return secret.ErrInvalid
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	_, err = s.client.GetSecret(ctx, b.Name, "", nil)
	if err != nil {
		return readError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = s.client.SetSecret(ctx, b.Name, azsecrets.SetSecretParameters{Value: &value, ContentType: clone(b.ContentType), Tags: copyTags(b.Tags), SecretAttributes: &azsecrets.SecretAttributes{Enabled: clone(b.Enabled), NotBefore: clone(b.NotBefore), Expires: clone(b.Expires)}}, nil)
	if err != nil {
		return mutation(ctx)
	}
	return nil
}

// copyTags prevents an injected client from mutating retained binding metadata.
func copyTags(tags map[string]*string) map[string]*string {
	copy := make(map[string]*string, len(tags))
	for k, v := range tags {
		copy[k] = clone(v)
	}
	return copy
}

// deleting recognizes only Azure's exact bounded nested in-progress state.
func deleting(err error) bool {
	var response *azcore.ResponseError
	if !errors.As(err, &response) || response.StatusCode != 409 || response.RawResponse == nil || response.RawResponse.Body == nil {
		return false
	}
	// Bound new reads even when an injected client did not cache the response body.
	bounded := *response.RawResponse
	defer response.RawResponse.Body.Close()
	bounded.Body = io.NopCloser(io.LimitReader(response.RawResponse.Body, 65537))
	data, readErr := runtime.Payload(&bounded)
	if readErr != nil || len(data) > 65536 {
		return false
	}
	var body struct {
		Error struct {
			InnerError struct {
				Code string `json:"code"`
			} `json:"innererror"`
		} `json:"error"`
	}
	return json.Unmarshal(data, &body) == nil && body.Error.InnerError.Code == "ObjectIsBeingDeleted"
}

// Delete initiates vault-configured soft deletion without purging or polling.
func (s *Store) Delete(ctx context.Context, name string) error {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	_, err = s.client.DeleteSecret(ctx, b.Name, nil)
	if err == nil {
		return nil
	}
	var response *azcore.ResponseError
	if errors.As(err, &response) && response.StatusCode == 404 || deleting(err) {
		return nil
	}
	return mutation(ctx)
}
