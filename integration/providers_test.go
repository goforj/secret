//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/goforj/secret"
	"github.com/goforj/secret/driver/awssecret"
	"github.com/goforj/secret/driver/azuresecret"
	"github.com/goforj/secret/driver/gcpsecret"
	"github.com/goforj/secret/driver/vaultsecret"
	"github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// servicePool shares each disposable service for the complete package run.
var servicePool struct {
	sync.Mutex
	containers map[string]testcontainers.Container
	endpoints  map[string]string
}

// TestMain guarantees cleanup even when resource setup or a later assertion fails.
func TestMain(m *testing.M) {
	servicePool.containers = map[string]testcontainers.Container{}
	servicePool.endpoints = map[string]string{}
	result := m.Run()
	for _, container := range servicePool.containers {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := container.Terminate(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "integration: container cleanup failed")
			result = 1
		}
		cancel()
	}
	os.Exit(result)
}

// endpoint starts exactly one pinned container for each selected provider.
func endpoint(t *testing.T, name string) string {
	t.Helper()
	servicePool.Lock()
	defer servicePool.Unlock()
	if endpoint, ok := servicePool.endpoints[name]; ok {
		return endpoint
	}
	request := testcontainers.ContainerRequest{}
	port := ""
	scheme := "http"
	switch name {
	case "aws":
		request.Image = "motoserver/moto:5.1.16@sha256:f78c8e0370cabe2516395f351fb27e1df1aa6dc2d728d6de16d5dfbc9ccf595d"
		port = "5000/tcp"

	case "gcp":
		request.Image = "ghcr.io/blackwell-systems/gcp-secret-manager-emulator-dual@sha256:2c9d3bb0453fdff102b29af00b4abd9b2c6fa466031a9d8e6d93a270ba22a6e2"
		port = "9090/tcp"
		scheme = ""
	case "azure":
		request.Image = "nagyesta/lowkey-vault:7.4.0-ubi10-minimal@sha256:7d78373738e77b7782195e8dc1103c6ea0883947d639dab9373d0407dda8b499"
		port = "8443/tcp"
		request.Env = map[string]string{"LOWKEY_ARGS": "--LOWKEY_VAULT_RELAXED_PORTS=true"}
		scheme = "https"
	case "vault":
		request.Image = "hashicorp/vault:1.21.4@sha256:4e33b126a59c0c333b76fb4e894722462659a6bec7c48c9ee8cea56fccfd2569"
		port = "8200/tcp"
		request.Env = map[string]string{"VAULT_DEV_ROOT_TOKEN_ID": "public-integration-token", "VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200"}
		request.Cmd = []string{"server", "-dev"}
	default:
		t.Fatal("unknown integration provider")
	}
	request.ExposedPorts = []string{port}
	if name == "gcp" {
		request.ExposedPorts = append(request.ExposedPorts, "8080/tcp")
	}
	request.WaitingFor = wait.ForListeningPort(port).WithStartupTimeout(2 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: false})
	if container != nil {
		servicePool.containers[name] = container
	}
	if err != nil {
		t.Fatal("container creation failed:", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatal("container startup failed:", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := container.MappedPort(ctx, port)
	if err != nil {
		t.Fatal(err)
	}
	address := host + ":" + mapped.Port()
	if scheme != "" {
		address = scheme + "://" + address
	}
	servicePool.endpoints[name] = address
	return address
}

// contract verifies the portable behavior without exposing provider values in failures.
func contract(t *testing.T, s secret.Store, empty, binary bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"invalid", "UNKNOWN"} {
		_, err := s.Get(ctx, name)
		want := secret.ErrNotFound
		if name == "invalid" {
			want = secret.ErrInvalid
		}
		if !errors.Is(err, want) {
			t.Fatal("request validation failed", err)
		}
	}
	values := []string{"public-replacement\n"}
	if empty {
		values = append(values, "")
	}
	if binary {
		values = append(values, "\x00\xff\n")
	}
	for _, value := range values {
		if err := s.Set(ctx, "KEY", value); err != nil {
			t.Fatal("replacement failed", err)
		}
		got, err := s.Get(ctx, "KEY")
		if err != nil || got != value {
			t.Fatal("value fidelity failed", err)
		}
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.Get(canceled, "KEY"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation failed", err)
	}
	for range 2 {
		if err := s.Delete(ctx, "KEY"); err != nil {
			t.Fatal("idempotent delete failed", err)
		}
	}
	if err := s.Set(ctx, "KEY", "public-after-delete"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal("deleted resource replaced", err)
	}
}

// TestAWS exercises text and binary resources against Moto through the real AWS SDK.
func TestAWS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	address := endpoint(t, "aws")
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("public-test", "public-test", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = &address })
	for _, encoding := range []awssecret.Encoding{awssecret.Text, awssecret.Binary} {
		t.Run(fmt.Sprint(encoding), func(t *testing.T) {
			name := fmt.Sprintf("secret-integration-%d", time.Now().UnixNano())
			value := "public-initial"
			input := &secretsmanager.CreateSecretInput{Name: &name}
			if encoding == awssecret.Text {
				input.SecretString = &value
			} else {
				input.SecretBinary = []byte(value)
			}
			created, err := client.CreateSecret(ctx, input)
			if err != nil {
				t.Fatal("AWS provisioning failed", err)
			}
			t.Cleanup(func() {
				force := true
				client.DeleteSecret(context.Background(), &secretsmanager.DeleteSecretInput{SecretId: created.ARN, ForceDeleteWithoutRecovery: &force})
			})
			s, err := awssecret.New(client, map[string]awssecret.Binding{"KEY": {ARN: *created.ARN, Encoding: encoding}})
			if err != nil {
				t.Fatal(err)
			}
			contract(t, s, false, encoding == awssecret.Binary)
		})
	}
}

// TestGoogle exercises checksum, non-retried version writes, and ETag deletion through gRPC.
func TestGoogle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := grpc.NewClient(endpoint(t, "gcp"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client, err := secretmanager.NewClient(ctx, option.WithGRPCConn(conn))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	id := fmt.Sprintf("secret-%d", time.Now().UnixNano())
	parent := "projects/public-integration"
	created, err := client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{Parent: parent, SecretId: id, Secret: &secretmanagerpb.Secret{Replication: &secretmanagerpb.Replication{Replication: &secretmanagerpb.Replication_Automatic_{Automatic: &secretmanagerpb.Replication_Automatic{}}}}})
	if err != nil {
		t.Fatal("Google provisioning failed", err)
	}
	t.Cleanup(func() {
		client.DeleteSecret(context.Background(), &secretmanagerpb.DeleteSecretRequest{Name: created.Name})
	})
	s, err := gcpsecret.New(client, parent, map[string]string{"KEY": id})
	if err != nil {
		t.Fatal(err)
	}
	contract(t, s, true, true)
}

// testCredential supplies a public dummy token accepted only by the isolated Azure emulator.
type testCredential struct{}

// GetToken never reads developer or shared credentials during emulator verification.
func (testCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "public-emulator-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// TestAzure verifies latest text versions and repeated soft deletion using the real Azure SDK.
func TestAzure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// TLS verification is disabled only for the emulator's self-signed certificate.
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer transport.CloseIdleConnections()
	client, err := azsecrets.NewClient(endpoint(t, "azure"), testCredential{}, &azsecrets.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: &http.Client{Transport: transport}, Retry: policy.RetryOptions{MaxRetries: 3}}, DisableChallengeResourceVerification: true})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("secret-%d", time.Now().UnixNano())
	value := "public-initial"
	_, err = client.SetSecret(ctx, name, azsecrets.SetSecretParameters{Value: &value}, nil)
	if err != nil {
		t.Fatal("Azure provisioning failed", err)
	}
	s, err := azuresecret.New(client, map[string]azuresecret.Binding{"KEY": {Name: name}})
	if err != nil {
		t.Fatal(err)
	}
	// Lowkey Vault rejects empty values despite Azure's wire schema allowing them.
	// Keep the contract visible without claiming that this emulator proves hosted compatibility.
	if err := s.Set(ctx, "KEY", ""); !errors.Is(err, secret.ErrIndeterminate) {
		t.Fatal("unexpected emulator empty-value behavior", err)
	}
	contract(t, s, false, false)
}

// TestVault verifies actual KV v2 ownership metadata, CAS, and all-version deletion.
func TestVault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	config := api.DefaultConfig()
	config.Address = endpoint(t, "vault")
	client, err := api.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	client.SetToken("public-integration-token")
	health, err := client.Sys().HealthWithContext(ctx)
	if err != nil || !strings.HasPrefix(health.Version, "1.21.4") {
		t.Fatal("unexpected Vault server version")
	}
	kv := client.KVv2("secret")
	for _, state := range []string{"active", "soft-deleted", "shared"} {
		t.Run(state, func(t *testing.T) {
			path := fmt.Sprintf("secret-integration/%d", time.Now().UnixNano())
			t.Cleanup(func() { kv.DeleteMetadata(context.Background(), path) })
			_, err := kv.Put(ctx, path, map[string]interface{}{"value": "public-initial"})
			if err != nil {
				t.Fatal(err)
			}
			if state != "shared" {
				err = kv.PutMetadata(ctx, path, api.KVMetadataPutInput{CustomMetadata: map[string]interface{}{"goforj-secret-format": "scalar-v1"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			s, err := vaultsecret.New(kv, map[string]string{"KEY": path})
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "active":
				contract(t, s, true, false)
			case "soft-deleted":
				if err := kv.Delete(ctx, path); err != nil {
					t.Fatal(err)
				}
				if err := s.Delete(ctx, "KEY"); err != nil {
					t.Fatal(err)
				}
				if _, err := kv.GetMetadata(ctx, path); !errors.Is(err, api.ErrSecretNotFound) {
					t.Fatal("metadata retained after whole-resource deletion")
				}
			case "shared":
				if err := s.Delete(ctx, "KEY"); !errors.Is(err, secret.ErrUnavailable) {
					t.Fatal("unmarked path deleted", err)
				}
				if _, err := kv.Get(ctx, path); err != nil {
					t.Fatal("shared path damaged")
				}
			}
		})
	}
}

// TestGoogleREST checks the REST client's real error normalization and checksums independently of gRPC.
func TestGoogleREST(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	endpoint(t, "gcp")
	container := servicePool.containers["gcp"]
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatal(err)
	}
	client, err := secretmanager.NewRESTClient(ctx, option.WithEndpoint("http://"+host+":"+port.Port()), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	id := fmt.Sprintf("secret-rest-%d", time.Now().UnixNano())
	parent := "projects/public-integration"
	_, err = client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{Parent: parent, SecretId: id, Secret: &secretmanagerpb.Secret{Replication: &secretmanagerpb.Replication{Replication: &secretmanagerpb.Replication_Automatic_{Automatic: &secretmanagerpb.Replication_Automatic{}}}}})
	if err != nil {
		t.Fatal("Google REST provisioning failed")
	}
	s, err := gcpsecret.New(client, parent, map[string]string{"KEY": id})
	if err != nil {
		t.Fatal(err)
	}
	contract(t, s, true, true)
}
