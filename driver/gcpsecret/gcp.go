// Package gcpsecret binds application names to Google Secret Manager resources.
// Regional parents require a client configured for the corresponding regional endpoint.
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
	"regexp"
)

// MaxValueBytes bounds values at the provider's 64 KiB limit.
const MaxValueBytes = 65536

// Client is implemented by both Google gRPC and REST Secret Manager clients.
type Client interface {
	AccessSecretVersion(context.Context, *secretmanagerpb.AccessSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
	GetSecret(context.Context, *secretmanagerpb.GetSecretRequest, ...gax.CallOption) (*secretmanagerpb.Secret, error)
	AddSecretVersion(context.Context, *secretmanagerpb.AddSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.SecretVersion, error)
	DeleteSecret(context.Context, *secretmanagerpb.DeleteSecretRequest, ...gax.CallOption) error
}

// binding retains a complete trusted resource name and a mutation gate.
type binding struct {
	resource string
	gate     chan struct{}
}

// Store borrows its client. The caller retains responsibility for closing it.
type Store struct {
	client   Client
	bindings map[string]*binding
}

var validParent = regexp.MustCompile(`^projects/[^/\s]+(?:/locations/[^/\s]+)?$`)
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
var checksumTable = crc32.MakeTable(crc32.Castagnoli)

// New copies unique secret IDs under a global or regional parent. It performs no I/O.
func New(client Client, parent string, bindings map[string]string) (*Store, error) {
	if nilClient(client) || len(bindings) == 0 || len(parent) > 1024 || !validParent.MatchString(parent) {
		return nil, secret.ErrInvalid
	}
	copy := make(map[string]*binding, len(bindings))
	seen := make(map[string]bool, len(bindings))
	for name, id := range bindings {
		if !validName(name) || !validID.MatchString(id) || seen[id] {
			return nil, secret.ErrInvalid
		}
		seen[id] = true
		copy[name] = &binding{resource: parent + "/secrets/" + id, gate: make(chan struct{}, 1)}
	}
	return &Store{client: client, bindings: copy}, nil
}

// readError classifies structured status while keeping transport prose private.
func readError(ctx context.Context, err error, current bool) error {
	kind := secret.ErrUnavailable
	switch status.Code(err) {
	case codes.NotFound:
		kind = secret.ErrNotFound
	case codes.FailedPrecondition:
		if current {
			kind = secret.ErrNotFound
		}
	case codes.PermissionDenied, codes.Unauthenticated:
		kind = secret.ErrPermission
	}
	return failure(ctx, kind)
}

// Get verifies CRC32C before returning the latest version's exact bytes.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return "", err
	}
	value, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: b.resource + "/versions/latest"})
	if err != nil {
		return "", readError(ctx, err, true)
	}
	if value == nil || value.Payload == nil || value.Payload.DataCrc32C == nil || len(value.Payload.Data) > MaxValueBytes || int64(crc32.Checksum(value.Payload.Data, checksumTable)) != *value.Payload.DataCrc32C {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	return string(value.Payload.Data), nil
}

// Set publishes exact bytes without automatic AddSecretVersion retries or resource creation.
func (s *Store) Set(ctx context.Context, name, value string) error {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if len(value) > MaxValueBytes {
		return secret.ErrInvalid
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	data := []byte(value)
	checksum := int64(crc32.Checksum(data, checksumTable))
	_, err = s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{Parent: b.resource, Payload: &secretmanagerpb.SecretPayload{Data: data, DataCrc32C: &checksum}}, gax.WithRetry(func() gax.Retryer { return nil }))
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.NotFound {
		return failure(ctx, secret.ErrNotFound)
	}
	return mutation(ctx)
}

// Delete guards the whole-resource deletion with the preflight ETag.
func (s *Store) Delete(ctx context.Context, name string) error {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	state, err := s.client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: b.resource})
	if err != nil {
		mapped := readError(ctx, err, false)
		if errors.Is(mapped, secret.ErrNotFound) {
			return nil
		}
		return mapped
	}
	if state == nil || state.Etag == "" {
		return failure(ctx, secret.ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = s.client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: b.resource, Etag: state.Etag})
	switch status.Code(err) {
	case codes.OK, codes.NotFound:
		return nil
	case codes.FailedPrecondition:
		return failure(ctx, secret.ErrUnavailable)
	}
	return mutation(ctx)
}
