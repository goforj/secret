package awssecret

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/smithy-go"
	"github.com/goforj/secret"
	"strings"
	"sync"
	"testing"
	"time"
)

const testARN = "arn:aws:secretsmanager:us-east-1:123456789012:secret:test-AbCdEf"

// fakeClient records exact SDK dispatch without retaining any production credentials.
type fakeClient struct {
	get      func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error)
	describe func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error)
	put      func(context.Context, *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error)
	delete   func(context.Context, *secretsmanager.DeleteSecretInput) (*secretsmanager.DeleteSecretOutput, error)
}

// GetSecretValue exposes the SDK method seam to the read contract suite.
func (c *fakeClient) GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return c.get(ctx, in)
}

// DescribeSecret exposes preflight and reconciliation responses.
func (c *fakeClient) DescribeSecret(ctx context.Context, in *secretsmanager.DescribeSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.DescribeSecretOutput, error) {
	return c.describe(ctx, in)
}

// PutSecretValue exposes mutation acknowledgement and uncertain outcomes.
func (c *fakeClient) PutSecretValue(ctx context.Context, in *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	return c.put(ctx, in)
}

// DeleteSecret exposes whole-resource deletion responses.
func (c *fakeClient) DeleteSecret(ctx context.Context, in *secretsmanager.DeleteSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
	return c.delete(ctx, in)
}

// apiError supplies structured codes with intentionally unsafe prose for disclosure checks.
func apiError(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: "private-value locator credential"}
}

// ptr provides public fixture pointers without changing production imports.
func ptr[T any](v T) *T { return &v }

// fixture constructs a store whose unused client methods fail visibly if dispatched.
func fixture(t *testing.T) (*Store, *fakeClient) {
	t.Helper()
	c := &fakeClient{}
	s, err := New(c, map[string]Binding{"KEY": {testARN, Text}})
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

// expect checks classification and the absence of provider data from the unwrap graph.
func expect(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("classification %v, want %v", err, want)
	}
	if err != nil && strings.Contains(err.Error(), "private-value") {
		t.Fatal("provider prose disclosed")
	}
}

// TestConstructor rejects aliases, malformed ARNs, nil dependencies, and representation ambiguity.
func TestConstructor(t *testing.T) {
	c := &fakeClient{}
	for _, bindings := range []map[string]Binding{nil, {"bad": {testARN, Text}}, {"KEY": {"name", Text}}, {"KEY": {testARN, 0}}, {"KEY": {testARN, Binary}, "OTHER": {testARN, Text}}, {"KEY": {strings.Repeat("x", 2049), Text}}} {
		if _, err := New(c, bindings); !errors.Is(err, secret.ErrInvalid) {
			t.Fatal("invalid constructor accepted")
		}
	}
	var missing *fakeClient
	if _, err := New(missing, map[string]Binding{"KEY": {testARN, Text}}); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal(err)
	}
	s, _ := fixture(t)
	token, err := s.token()
	if err != nil || len(token) != 36 || token[14] != '4' {
		t.Fatal("invalid token")
	}
}

// TestGet covers the complete structured error mapping and representation contract.
func TestGet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	for _, test := range []struct {
		code string
		want error
	}{
		{"ResourceNotFoundException", secret.ErrNotFound}, {"InvalidRequestException", secret.ErrNotFound},
		{"AccessDeniedException", secret.ErrPermission}, {"IncompleteSignature", secret.ErrPermission}, {"InvalidClientTokenId", secret.ErrPermission}, {"NotAuthorized", secret.ErrPermission}, {"OptInRequired", secret.ErrPermission}, {"UnrecognizedClientException", secret.ErrPermission},
		{"DecryptionFailure", secret.ErrUnavailable}, {"EncryptionFailure", secret.ErrUnavailable}, {"InternalFailure", secret.ErrUnavailable}, {"InternalServiceError", secret.ErrUnavailable}, {"InvalidAction", secret.ErrUnavailable}, {"InvalidNextTokenException", secret.ErrUnavailable}, {"InvalidParameterException", secret.ErrUnavailable}, {"LimitExceededException", secret.ErrUnavailable}, {"MalformedPolicyDocumentException", secret.ErrUnavailable}, {"PreconditionNotMetException", secret.ErrUnavailable}, {"PublicPolicyException", secret.ErrUnavailable}, {"RequestExpired", secret.ErrUnavailable}, {"ResourceExistsException", secret.ErrUnavailable}, {"ServiceUnavailable", secret.ErrUnavailable}, {"ThrottlingException", secret.ErrUnavailable}, {"ValidationError", secret.ErrUnavailable}, {"ValidationException", secret.ErrUnavailable}, {"unknown", secret.ErrUnavailable},
	} {
		t.Run(test.code, func(t *testing.T) {
			cause := apiError(test.code)
			c.get = func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
				return nil, cause
			}
			_, err := s.Get(ctx, "KEY")
			expect(t, err, test.want)
			if errors.Is(err, cause) {
				t.Fatal("SDK error retained")
			}
		})
	}
	for _, out := range []*secretsmanager.GetSecretValueOutput{nil, {}, {SecretString: ptr("")}, {SecretBinary: []byte("x")}, {SecretString: ptr("x"), SecretBinary: []byte("x")}, {SecretString: ptr("\xff")}, {SecretString: ptr(strings.Repeat("x", MaxValueBytes+1))}} {
		c.get = func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
			return out, nil
		}
		value, err := s.Get(ctx, "KEY")
		expect(t, err, secret.ErrUnavailable)
		if value != "" {
			t.Fatal("malformed read disclosed bytes")
		}
	}
	for _, encoding := range []Encoding{Text, Binary} {
		s.bindings["KEY"].Encoding = encoding
		want := "line\n"
		if encoding == Binary {
			want = "\x00\xff\n"
		}
		c.get = func(got context.Context, in *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
			if got != ctx || *in.SecretId != testARN || in.VersionId != nil || in.VersionStage != nil {
				t.Fatal("wrong dispatch")
			}
			out := &secretsmanager.GetSecretValueOutput{}
			if encoding == Text {
				out.SecretString = &want
			} else {
				out.SecretBinary = []byte(want)
			}
			return out, nil
		}
		if value, err := s.Get(ctx, "KEY"); err != nil || value != want {
			t.Fatal("bytes changed")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	c.get = func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
		cancel()
		return nil, errors.New("private-value")
	}
	_, err := s.Get(canceled, "KEY")
	expect(t, err, context.Canceled)
}

// TestSet freezes preflight, per-call tokens, exact bytes, and post-dispatch uncertainty.
func TestSet(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	s.token = func() (string, error) { return "stable-token", nil }
	for _, value := range []string{"", strings.Repeat("x", MaxValueBytes+1), "\xff"} {
		expect(t, s.Set(ctx, "KEY", value), secret.ErrInvalid)
	}
	s.token = func() (string, error) { return "", errors.New("entropy failed") }
	expect(t, s.Set(ctx, "KEY", "x"), secret.ErrUnavailable)
	s.token = func() (string, error) { return "stable-token", nil }
	for _, test := range []struct {
		out       *secretsmanager.DescribeSecretOutput
		err, want error
	}{{nil, nil, secret.ErrUnavailable}, {nil, apiError("InvalidRequestException"), secret.ErrUnavailable}, {nil, apiError("ResourceNotFoundException"), secret.ErrNotFound}, {&secretsmanager.DescribeSecretOutput{DeletedDate: ptr(time.Now())}, nil, secret.ErrNotFound}} {
		c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
			return test.out, test.err
		}
		expect(t, s.Set(ctx, "KEY", "x"), test.want)
	}
	c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
		return &secretsmanager.DescribeSecretOutput{}, nil
	}
	for _, encoding := range []Encoding{Text, Binary} {
		s.bindings["KEY"].Encoding = encoding
		value := strings.Repeat("x", MaxValueBytes)
		if encoding == Binary {
			value = "\x00\xff"
		}
		c.put = func(got context.Context, in *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
			if got != ctx || *in.SecretId != testARN || *in.ClientRequestToken != "stable-token" {
				t.Fatal("mutation dispatch changed")
			}
			if encoding == Text && (*in.SecretString != value || in.SecretBinary != nil) || encoding == Binary && (string(in.SecretBinary) != value || in.SecretString != nil) {
				t.Fatal("bytes changed")
			}
			return nil, nil
		}
		if err := s.Set(ctx, "KEY", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []string{"ResourceNotFoundException", "AccessDeniedException", "ThrottlingException", "unknown"} {
		c.put = func(context.Context, *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
			return nil, apiError(code)
		}
		want := secret.ErrIndeterminate
		if code == "ResourceNotFoundException" {
			want = secret.ErrNotFound
		}
		expect(t, s.Set(ctx, "KEY", "x"), want)
	}
	for _, committed := range []bool{false, true} {
		calls := 0
		c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
			calls++
			out := &secretsmanager.DescribeSecretOutput{}
			if calls == 2 {
				out.DeletedDate = ptr(time.Now())
				if committed {
					out.VersionIdsToStages = map[string][]string{"stable-token": nil}
				}
			}
			return out, nil
		}
		c.put = func(context.Context, *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
			return nil, apiError("InvalidRequestException")
		}
		err := s.Set(ctx, "KEY", "x")
		if committed {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			expect(t, err, secret.ErrIndeterminate)
		}
		if calls != 2 {
			t.Fatal("unbounded reconciliation")
		}
	}
}

// TestDelete preserves recovery defaults and recognizes only proven idempotence.
func TestDelete(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	for _, test := range []struct {
		out       *secretsmanager.DescribeSecretOutput
		err, want error
	}{{nil, nil, secret.ErrUnavailable}, {nil, apiError("ResourceNotFoundException"), nil}, {nil, apiError("AccessDeniedException"), secret.ErrPermission}, {&secretsmanager.DescribeSecretOutput{DeletedDate: ptr(time.Now())}, nil, nil}} {
		c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
			return test.out, test.err
		}
		expect(t, s.Delete(ctx, "KEY"), test.want)
	}
	for _, state := range []string{"ok", "missing", "permission", "invalid", "scheduled"} {
		calls := 0
		c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
			calls++
			out := &secretsmanager.DescribeSecretOutput{}
			if calls == 2 && state == "scheduled" {
				out.DeletedDate = ptr(time.Now())
			}
			return out, nil
		}
		c.delete = func(_ context.Context, in *secretsmanager.DeleteSecretInput) (*secretsmanager.DeleteSecretOutput, error) {
			if in.ForceDeleteWithoutRecovery != nil || in.RecoveryWindowInDays != nil {
				t.Fatal("recovery policy changed")
			}
			switch state {
			case "ok":
				return nil, nil
			case "missing":
				return nil, apiError("ResourceNotFoundException")
			case "permission":
				return nil, apiError("AccessDeniedException")
			default:
				return nil, apiError("InvalidRequestException")
			}
		}
		want := secret.ErrIndeterminate
		if state == "ok" || state == "missing" || state == "scheduled" {
			want = nil
		}
		expect(t, s.Delete(ctx, "KEY"), want)
	}
}

// TestMutationSerialization proves one blocked binding cannot block another binding or canceled waiters.
func TestMutationSerialization(t *testing.T) {
	c := &fakeClient{}
	other := strings.Replace(testARN, "test-AbCdEf", "other-AbCdEf", 1)
	s, err := New(c, map[string]Binding{"KEY": {testARN, Text}, "OTHER": {other, Text}})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c.describe = func(ctx context.Context, in *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
		if *in.SecretId == testARN {
			once.Do(func() { close(entered) })
			<-release
		}
		return &secretsmanager.DescribeSecretOutput{}, nil
	}
	c.put = func(context.Context, *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
		return nil, nil
	}
	done := make(chan error, 1)
	go func() { done <- s.Set(context.Background(), "KEY", "x") }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	expect(t, s.Delete(ctx, "KEY"), context.DeadlineExceeded)
	if err := s.Set(context.Background(), "OTHER", "x"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestRequestAndPreflightCancellation freezes non-dispatch behavior on every public operation.
func TestRequestAndPreflightCancellation(t *testing.T) {
	s, c := fixture(t)
	_, err := s.Get(nil, "KEY")
	expect(t, err, secret.ErrInvalid)
	expect(t, s.Set(nil, "KEY", "x"), secret.ErrInvalid)
	expect(t, s.Delete(nil, "KEY"), secret.ErrInvalid)
	for _, op := range []func(context.Context) error{func(ctx context.Context) error { return s.Set(ctx, "KEY", "x") }, func(ctx context.Context) error { return s.Delete(ctx, "KEY") }} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		expect(t, op(ctx), context.Canceled)
		ctx, cancel = context.WithCancel(context.Background())
		c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
			cancel()
			return &secretsmanager.DescribeSecretOutput{}, nil
		}
		expect(t, op(ctx), context.Canceled)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.describe = func(context.Context, *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
		return &secretsmanager.DescribeSecretOutput{}, nil
	}
	c.put = func(context.Context, *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
		cancel()
		return nil, apiError("AccessDeniedException")
	}
	err = s.Set(ctx, "KEY", "x")
	expect(t, err, secret.ErrIndeterminate)
	expect(t, err, context.Canceled)
}

// TestMutationCanceledBeforeGate freezes validation-to-lock cancellation races.
func TestMutationCanceledBeforeGate(t *testing.T) {
	s, _ := fixture(t)
	expect(t, s.Set(&stagedContext{Context: context.Background()}, "KEY", "x"), context.Canceled)
	expect(t, s.Delete(&stagedContext{Context: context.Background()}, "KEY"), context.Canceled)
}

// TestRequestOwnership prevents client middleware from changing retained binding policy.
func TestRequestOwnership(t *testing.T) {
	s, c := fixture(t)
	check := func(id *string) {
		if id == nil || *id != testARN {
			t.Fatal("request no longer targets the configured resource")
		}
		*id = "changed-by-client"
	}
	c.get = func(_ context.Context, in *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
		check(in.SecretId)
		return &secretsmanager.GetSecretValueOutput{SecretString: ptr("public-fixture")}, nil
	}
	c.describe = func(_ context.Context, in *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
		check(in.SecretId)
		return &secretsmanager.DescribeSecretOutput{}, nil
	}
	c.put = func(_ context.Context, in *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
		check(in.SecretId)
		return &secretsmanager.PutSecretValueOutput{}, nil
	}
	c.delete = func(_ context.Context, in *secretsmanager.DeleteSecretInput) (*secretsmanager.DeleteSecretOutput, error) {
		check(in.SecretId)
		return &secretsmanager.DeleteSecretOutput{}, nil
	}
	ctx := context.Background()
	if _, err := s.Get(ctx, "KEY"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "KEY", "public-replacement"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "KEY"); err != nil {
		t.Fatal(err)
	}
	if s.bindings["KEY"].ARN != testARN {
		t.Fatal("client changed retained binding")
	}
}

// TestReconciliationTokenOwnership refuses to use a client-mutated request token as commit evidence.
func TestReconciliationTokenOwnership(t *testing.T) {
	s, c := fixture(t)
	s.token = func() (string, error) { return "stable-token", nil }
	c.describe = func(_ context.Context, in *secretsmanager.DescribeSecretInput) (*secretsmanager.DescribeSecretOutput, error) {
		return &secretsmanager.DescribeSecretOutput{VersionIdsToStages: map[string][]string{"changed-token": nil}}, nil
	}
	c.put = func(_ context.Context, in *secretsmanager.PutSecretValueInput) (*secretsmanager.PutSecretValueOutput, error) {
		*in.ClientRequestToken = "changed-token"
		return nil, apiError("InvalidRequestException")
	}
	expect(t, s.Set(context.Background(), "KEY", "public-replacement"), secret.ErrIndeterminate)
}
