// Package awssecret binds application names to immutable AWS Secrets Manager ARNs.
// The caller owns SDK credentials, retry configuration, and exclusive resource lifecycle.
package awssecret

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/smithy-go"
	"github.com/goforj/secret"
	"regexp"
	"unicode/utf8"
)

// MaxValueBytes is the AWS payload limit for both representations.
const MaxValueBytes = 65536

// Encoding fixes a binding's representation at construction.
type Encoding uint8

const (
	// Text requires valid UTF-8 in SecretString.
	Text Encoding = iota + 1
	// Binary preserves arbitrary bytes in SecretBinary.
	Binary
)

// Binding identifies one entire AWS resource, including its immutable suffix.
type Binding struct {
	ARN      string
	Encoding Encoding
}

// Client is implemented by *secretsmanager.Client.
type Client interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	PutSecretValue(context.Context, *secretsmanager.PutSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
	DescribeSecret(context.Context, *secretsmanager.DescribeSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DescribeSecretOutput, error)
	DeleteSecret(context.Context, *secretsmanager.DeleteSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error)
}

// binding associates copied policy with one context-aware mutation gate.
type binding struct {
	Binding
	gate chan struct{}
}

// Store borrows a concurrency-safe client without changing SDK retries or logging.
type Store struct {
	client   Client
	bindings map[string]*binding
	token    func() (string, error)
}

var canonicalARN = regexp.MustCompile(`^arn:aws(?:-[a-z0-9-]+)?:secretsmanager:[a-z0-9]+(?:-[a-z0-9]+)+:[0-9]{12}:secret:[A-Za-z0-9/_+=.@-]+-[A-Za-z0-9]{6}$`)

// New validates and copies unique full ARNs. Mutations require DescribeSecret in
// addition to PutSecretValue or DeleteSecret permissions; resource provisioning stays external.
func New(client Client, bindings map[string]Binding) (*Store, error) {
	if nilClient(client) || len(bindings) == 0 {
		return nil, secret.ErrInvalid
	}
	copy := make(map[string]*binding, len(bindings))
	seen := make(map[string]bool, len(bindings))
	for name, b := range bindings {
		if !validName(name) || len(b.ARN) > 2048 || !canonicalARN.MatchString(b.ARN) || (b.Encoding != Text && b.Encoding != Binary) || seen[b.ARN] {
			return nil, secret.ErrInvalid
		}
		seen[b.ARN] = true
		copy[name] = &binding{Binding: b, gate: make(chan struct{}, 1)}
	}
	return &Store{client: client, bindings: copy, token: newToken}, nil
}

// stringPointer gives each SDK request its own copy of retained policy and reconciliation tokens.
func stringPointer(value string) *string { return &value }

// newToken creates one UUID per public Set, stable across SDK retry attempts.
func newToken() (string, error) {
	var data [16]byte
	// crypto/rand.Read terminates the process on entropy failure on supported Go versions.
	rand.Read(data[:])
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:]), nil
}

// code extracts only structured error codes and never retains provider errors.
func code(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}

// readError distinguishes unreadable current values from other InvalidRequest states.
func readError(ctx context.Context, err error, current bool) error {
	kind := secret.ErrUnavailable
	switch code(err) {
	case "ResourceNotFoundException":
		kind = secret.ErrNotFound
	case "InvalidRequestException":
		if current {
			kind = secret.ErrNotFound
		}
	case "AccessDeniedException", "IncompleteSignature", "InvalidClientTokenId", "NotAuthorized", "OptInRequired", "UnrecognizedClientException":
		kind = secret.ErrPermission
	}
	return failure(ctx, kind)
}

// Get leaves version selectors unset so AWS selects AWSCURRENT.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return "", err
	}
	result, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: stringPointer(b.ARN)})
	if err != nil {
		return "", readError(ctx, err, true)
	}
	if result == nil {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	var value string
	if b.Encoding == Text && result.SecretString != nil && result.SecretBinary == nil && utf8.ValidString(*result.SecretString) {
		value = *result.SecretString
	} else if b.Encoding == Binary && result.SecretString == nil && result.SecretBinary != nil {
		value = string(result.SecretBinary)
	} else {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	if len(value) == 0 || len(value) > MaxValueBytes {
		return "", failure(ctx, secret.ErrUnavailable)
	}
	return value, nil
}

// Set replaces an active resource, reconciling only AWS's ambiguous InvalidRequest state.
func (s *Store) Set(ctx context.Context, name, value string) error {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if len(value) == 0 || len(value) > MaxValueBytes || b.Encoding == Text && !utf8.ValidString(value) {
		return secret.ErrInvalid
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	token, err := s.token()
	if err != nil {
		return failure(ctx, secret.ErrUnavailable)
	}
	state, err := s.client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: stringPointer(b.ARN)})
	if err != nil {
		return readError(ctx, err, false)
	}
	if state == nil {
		return failure(ctx, secret.ErrUnavailable)
	}
	if state.DeletedDate != nil {
		return failure(ctx, secret.ErrNotFound)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	input := &secretsmanager.PutSecretValueInput{SecretId: stringPointer(b.ARN), ClientRequestToken: stringPointer(token)}
	if b.Encoding == Text {
		input.SecretString = &value
	} else {
		input.SecretBinary = []byte(value)
	}
	_, err = s.client.PutSecretValue(ctx, input)
	if err == nil {
		return nil
	}
	if code(err) == "ResourceNotFoundException" {
		return failure(ctx, secret.ErrNotFound)
	}
	if code(err) == "InvalidRequestException" {
		state, check := s.client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: stringPointer(b.ARN)})
		if check == nil && state != nil {
			if _, committed := state.VersionIdsToStages[token]; committed {
				return nil
			}
		}
	}
	return mutation(ctx)
}

// Delete requests AWS's standard recovery window without forcing destruction.
func (s *Store) Delete(ctx context.Context, name string) error {
	b, err := lookup(ctx, name, s.bindings)
	if err != nil {
		return err
	}
	if err := acquire(ctx, b.gate); err != nil {
		return err
	}
	defer func() { <-b.gate }()
	state, err := s.client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: stringPointer(b.ARN)})
	if err != nil {
		mapped := readError(ctx, err, false)
		if errors.Is(mapped, secret.ErrNotFound) {
			return nil
		}
		return mapped
	}
	if state == nil {
		return failure(ctx, secret.ErrUnavailable)
	}
	if state.DeletedDate != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = s.client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{SecretId: stringPointer(b.ARN)})
	if err == nil || code(err) == "ResourceNotFoundException" {
		return nil
	}
	if code(err) == "InvalidRequestException" {
		state, check := s.client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: stringPointer(b.ARN)})
		if check == nil && state != nil && state.DeletedDate != nil {
			return nil
		}
	}
	return mutation(ctx)
}
