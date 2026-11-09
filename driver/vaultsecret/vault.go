// Package vaultsecret binds scalar values to exclusively owned Vault KV v2 paths.
// The caller owns authentication, token renewal, mount configuration, and client lifecycle.
package vaultsecret

import (
	"context"
	"errors"
	"github.com/goforj/secret"
	"github.com/hashicorp/vault/api"
	"strings"
	"unicode/utf8"
)

// MaxValueBytes bounds scalar values before allocation or provider dispatch.
const MaxValueBytes = 1 << 20

// Client is the subset of a configured *api.KVv2 used by Store.
type Client interface {
	Get(context.Context, string) (*api.KVSecret, error)
	GetMetadata(context.Context, string) (*api.KVMetadata, error)
	Put(context.Context, string, map[string]interface{}, ...api.KVOption) (*api.KVSecret, error)
	DeleteMetadata(context.Context, string) error
}

// binding holds immutable ownership configuration and a per-resource mutation gate.
type binding struct {
	path string
	gate chan struct{}
}

// Store borrows its client and serializes mutations per binding.
type Store struct {
	client   Client
	bindings map[string]*binding
}

// New copies unique relative paths. Mutable paths require exclusive lifecycle ownership
// and custom metadata goforj-secret-format=scalar-v1, provisioned outside this library.
func New(client Client, bindings map[string]string) (*Store, error) {
	if nilClient(client) || len(bindings) == 0 {
		return nil, secret.ErrInvalid
	}
	copy := make(map[string]*binding, len(bindings))
	seen := make(map[string]bool, len(bindings))
	for name, path := range bindings {
		if !validName(name) || !validPath(path) || seen[path] {
			return nil, secret.ErrInvalid
		}
		seen[path] = true
		copy[name] = &binding{path: path, gate: make(chan struct{}, 1)}
	}
	return &Store{client: client, bindings: copy}, nil
}

// validPath excludes normalized aliases and traversal while leaving Vault syntax to Vault.
func validPath(path string) bool {
	if len(path) == 0 || len(path) > 1024 || strings.ContainsAny(path, "\x00\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// readError maps only structured state, discarding provider prose and locators.
func readError(ctx context.Context, err error) error {
	kind := secret.ErrUnavailable
	var response *api.ResponseError
	if errors.Is(err, api.ErrSecretNotFound) {
		kind = secret.ErrNotFound
	} else if errors.As(err, &response) {
		switch response.StatusCode {
		case 404:
			kind = secret.ErrNotFound
		case 401, 403:
			kind = secret.ErrPermission
		}
	}
	return failure(ctx, kind)
}

// document refuses shared or malformed paths rather than coercing values.
func document(value *api.KVSecret) (string, error) {
	if value == nil || value.Data == nil {
		return "", secret.ErrNotFound
	}
	text, ok := value.Data["value"].(string)
	if !ok || len(value.Data) != 1 || len(text) > MaxValueBytes || !utf8.ValidString(text) {
		return "", secret.ErrUnavailable
	}
	return text, nil
}

// owned requires the durable marker even when all data versions are deleted.
func owned(metadata *api.KVMetadata) bool {
	return metadata != nil && metadata.CustomMetadata["goforj-secret-format"] == "scalar-v1"
}

// Get selects only the latest version and returns its exact scalar value.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	b, ok := s.bindings[name]
	if err := request(ctx, name, ok); err != nil {
		return "", err
	}
	value, err := s.client.Get(ctx, b.path)
	if err != nil {
		return "", readError(ctx, err)
	}
	text, err := document(value)
	if err != nil {
		return "", failure(ctx, err)
	}
	return text, nil
}

// Set validates ownership and publishes with CAS. A failed Put may already have committed.
func (s *Store) Set(ctx context.Context, name, value string) error {
	b, ok := s.bindings[name]
	if err := request(ctx, name, ok); err != nil {
		return err
	}
	if len(value) > MaxValueBytes || !utf8.ValidString(value) {
		return secret.ErrInvalid
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	current, err := s.client.Get(ctx, b.path)
	if err != nil {
		return readError(ctx, err)
	}
	if _, err := document(current); err != nil {
		return failure(ctx, err)
	}
	if current.VersionMetadata == nil || current.VersionMetadata.Version < 1 {
		return failure(ctx, secret.ErrUnavailable)
	}
	metadata, err := s.client.GetMetadata(ctx, b.path)
	if err != nil {
		return readError(ctx, err)
	}
	if !owned(metadata) {
		return failure(ctx, secret.ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = s.client.Put(ctx, b.path, map[string]interface{}{"value": value}, api.WithCheckAndSet(current.VersionMetadata.Version))
	if err != nil {
		return mutation(ctx)
	}
	return nil
}

// Delete removes all versions and metadata only after proving exclusive scalar ownership.
func (s *Store) Delete(ctx context.Context, name string) error {
	b, ok := s.bindings[name]
	if err := request(ctx, name, ok); err != nil {
		return err
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	metadata, err := s.client.GetMetadata(ctx, b.path)
	if err != nil {
		err = readError(ctx, err)
		if errors.Is(err, secret.ErrNotFound) {
			return nil
		}
		return err
	}
	if !owned(metadata) {
		return failure(ctx, secret.ErrUnavailable)
	}
	current, err := s.client.Get(ctx, b.path)
	if err != nil {
		if mapped := readError(ctx, err); !errors.Is(mapped, secret.ErrNotFound) {
			return mapped
		}
	} else if current == nil {
		return failure(ctx, secret.ErrUnavailable)
	} else if current.Data != nil {
		if _, err := document(current); err != nil {
			return failure(ctx, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = s.client.DeleteMetadata(ctx, b.path)
	if err == nil {
		return nil
	}
	var response *api.ResponseError
	if errors.As(err, &response) && response.StatusCode == 404 {
		return nil
	}
	return mutation(ctx)
}
