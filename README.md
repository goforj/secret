<p align="center">
  <img src="docs/assets/logo.png" width="300" alt="secret">
</p>

<p align="center">Read, replace, and delete bound secrets through a small Go API.</p>

<p align="center">
  <a href="LICENSE"><img src="docs/assets/badges/summary-license.svg" alt="License: MIT"></a>
  <a href="https://go.dev"><img src="docs/assets/badges/summary-go.svg" alt="Go: 1.24.4+"></a>
  <img src="docs/assets/badges/summary-status.svg" alt="Status: unreleased">
  <img src="docs/assets/badges/summary-coverage.svg" alt="Library unit coverage: 100%">
  <img src="docs/assets/badges/summary-tests.svg" alt="Executed library unit tests">
</p>

<p align="center">
  <a href="#features">Features</a> · <a href="#quick-start">Quick Start</a> · <a href="#drivers">Drivers</a> · <a href="#installation">Installation</a> · <a href="#how-it-works">How it works</a> · <a href="#runnable-examples">Examples</a> · <a href="#api-index">API</a>
</p>

Bind names such as `DATABASE_PASSWORD` to mounted files, AWS Secrets Manager, Google Secret Manager, Azure Key Vault, or Vault KV v2. Application code reads by name through `secret.Reader`; provider SDKs and resource locations stay in setup code.

Managed drivers also support replacing and deleting existing secrets. You own provisioning, authentication, and access policies.

## Features

<table>
<thead><tr><th align="left">Feature</th><th align="left">What you get</th></tr></thead>
<tbody>
<tr><td><img src="docs/assets/badges/feature-icon-simple.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Small capabilities</strong></td><td>Read, replace, or delete with ordinary strings.</td></tr>
<tr><td><img src="docs/assets/badges/feature-icon-bindings.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Explicit bindings</strong></td><td>Choose provider locations once during construction.</td></tr>
<tr><td><img src="docs/assets/badges/feature-icon-bytes.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Exact values</strong></td><td>Preserve bytes within each provider&#39;s documented limits.</td></tr>
<tr><td><img src="docs/assets/badges/feature-icon-errors.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Safe failures</strong></td><td>Classify driver failures without exposing provider errors.</td></tr>
<tr><td><img src="docs/assets/badges/feature-icon-mutations.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Deliberate mutations</strong></td><td>Serialize same-secret writes and report uncertain outcomes.</td></tr>
<tr><td><img src="docs/assets/badges/feature-icon-testing.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Isolated testing</strong></td><td>Use a concurrency-safe store for application fixtures.</td></tr>
</tbody>
</table>


## Quick Start

Bind application names to mounted files or managed provider resources. The `directory` below is the path to your mounted secrets directory. The caller owns and closes the root.

```go
root, err := os.OpenRoot(directory)
if err != nil {
	panic(err)
}
defer root.Close()

reader, err := filesecret.New(root, map[string]string{
	"DATABASE_USERNAME": "database-username",
	"DATABASE_PASSWORD": "database-password",
})
if err != nil {
	panic(err)
}

ctx := context.Background()
username, err := reader.Get(ctx, "DATABASE_USERNAME")
if err != nil {
	panic(err)
}
password, err := reader.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	panic(err)
}
// Pass both values directly to your database client. Keep them out of logs.
_, _ = username, password
fmt.Println("database credentials loaded")
// Output: database credentials loaded
```

Import `github.com/goforj/secret/driver/filesecret` for this example. The complete [runnable program](examples/files/main.go) creates a public fixture directory first. File confinement requires Go 1.25.12+, Go 1.26.5+, or stable Go 1.27+.

Each `Get` reads one binding. Reuse the reader and context for additional names; the same pattern works with every managed driver. Reads are independent, so values read together are not an atomic snapshot.

Application services can accept `secret.Reader`. Workflows that replace or delete values can accept `secret.Writer`, `secret.Deleter`, or `secret.Store`.

## Drivers

<table>
<thead><tr><th align="left">Driver</th><th align="left">Behavior</th><th align="left">Verification</th></tr></thead>
<tbody>
<tr><td><img src="docs/assets/badges/driver-icon-aws.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/awssecret"><strong>AWS Secrets Manager</strong></a></td><td>Text or binary; standard recovery window</td><td>Moto 5.1.16 emulator</td></tr>
<tr><td><img src="docs/assets/badges/driver-icon-azure.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/azuresecret"><strong>Azure Key Vault</strong></a></td><td>UTF-8; vault-configured soft deletion</td><td>Lowkey Vault 7.4.0 emulator</td></tr>
<tr><td><img src="docs/assets/badges/driver-icon-files.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/filesecret"><strong>Mounted files</strong></a></td><td>Read exact bytes beneath a caller-owned root</td><td>Real filesystem and projected-volume tests</td></tr>
<tr><td><img src="docs/assets/badges/driver-icon-google.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/gcpsecret"><strong>Google Secret Manager</strong></a></td><td>Binary values, CRC32C, and ETag deletion</td><td>Google Secret Manager emulator (pinned image)</td></tr>
<tr><td><img src="docs/assets/badges/driver-icon-vault.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/vaultsecret"><strong>Vault KV v2</strong></a></td><td>Owned scalar paths; CAS and all-version deletion</td><td>Real Vault 1.21.4 server</td></tr>
</tbody>
</table>


Cloud verification uses the listed emulators and real provider SDKs. Hosted AWS, Google, and Azure behavior has not been verified against dedicated accounts. See [provider contracts](docs/drivers.md) for value limits, deletion behavior, construction, and emulator boundaries.

## Installation

The library is unreleased. To try it, clone the repository and use its local module replacements. The examples module requires Go 1.26; use Go 1.26.5+ in that branch or stable Go 1.27+ for the mounted-file example:

```sh
git clone https://github.com/goforj/secret.git
cd secret
go -C examples run ./files
go -C examples run ./reader
```

The core has no external dependencies and requires Go 1.24.4. Optional modules isolate provider SDKs and their Go requirements:

| Module | Minimum Go | Purpose |
| --- | --- | --- |
| github.com/goforj/secret | 1.24.4 | Reader, Writer, Deleter, Store, Func, and error classifications |
| github.com/goforj/secret/secrettest | 1.24.4 | Map-backed application test fixtures |
| github.com/goforj/secret/driver/awssecret | 1.24.4 | AWS Secrets Manager |
| github.com/goforj/secret/driver/azuresecret | 1.26.0 | Azure Key Vault |
| github.com/goforj/secret/driver/filesecret | 1.25.12 | Mounted files |
| github.com/goforj/secret/driver/gcpsecret | 1.26.0 | Google Secret Manager |
| github.com/goforj/secret/driver/vaultsecret | 1.26.0 | Vault KV v2 |

Import only the capabilities and drivers your application needs. Provider SDK dependencies stay in their driver modules. The file driver requires a patched `os.Root` implementation; Go 1.26.0 through 1.26.4 and prerelease toolchains are unsupported. See [development and releases](docs/development.md) for dependency constraints and publication checks.

## How it works

- **Names and bindings.** Names follow `[A-Z][A-Z0-9_]*`, with a maximum of 255 bytes. Use application identifiers such as `DATABASE_PASSWORD`. Drivers copy bindings at construction, and unknown names never reach a provider.

- **Exact values.** Values are ordinary strings. Reads preserve exact bytes within the provider's capabilities, without trimming, JSON parsing, environment fallback, caching, or rotation. Keep returned values out of logs and diagnostics.

- **Writes and deletion.** Managed `Set` publishes a new current value at an existing resource. `Delete` targets the entire resource, including its versions, under the provider's retention policy. Vault deletion is irreversible. The file driver is read-only.

- **Concurrency.** Each first-party managed store serializes mutations to the same binding. Different bindings and reads can proceed concurrently. Mutable resources require [exclusive lifecycle ownership](docs/drivers.md), including across separate store instances.

- **Retries.** Provider SDK retries remain caller-owned except Google version creation, where the driver disables automatic retries.

- **Safe failures.** Errors support `errors.Is` with `ErrInvalid`, `ErrNotFound`, `ErrPermission`, `ErrUnavailable`, and `ErrIndeterminate`. First-party drivers do not retain provider errors or response bodies in returned errors. `secret.Func` returns callback errors unchanged; the callback owns their classification and sanitization.

- **Uncertain writes.** Check `ErrIndeterminate` before cancellation or deadline errors. A canceled mutation may still have committed remotely, so reconcile before retrying. Caller cancellation remains detectable alongside `ErrIndeterminate`. See the [uncertain-write example](#mutation-errors).

- **Client ownership.** Constructing code owns the SDK client or file root. Drivers borrow them and never close them. First-party drivers do not log; review injected client logging separately because it can disclose provider metadata.

## Runnable Examples

Run from the repository root:

**[Mounted files](examples/files/main.go)**

```sh
go -C examples run ./files
```

**[Reader adapter](examples/reader/main.go)**

```sh
go -C examples run ./reader
```

**[Uncertain writes](examples/errors/main.go)**

```sh
go -C examples run ./errors
```

**[Application tests](examples/testing_test.go)**

```sh
go -C examples test -run TestStore -v
```

These offline programs are executed by the shared example runner and checked against the output comments beside their calls. Test fixtures use public placeholder values.

### Managed providers

Each program below shows SDK authentication, client construction, bindings, and a read of an existing secret. They print `credential loaded` on success and keep the value out of the output. Configure the provider credentials and read permissions before running one. These examples do not create, replace, or delete resources.

**AWS Secrets Manager** ([source](examples/providers/aws/main.go)). Uses the AWS default credential chain and configured region, including profiles or workload roles.

```sh
go -C examples run ./providers/aws \
  -arn arn:aws:secretsmanager:us-east-1:123456789012:secret:production/database-AbCdEf
```

**Google Secret Manager** ([source](examples/providers/gcp/main.go)). Uses Application Default Credentials.

```sh
go -C examples run ./providers/gcp \
  -parent projects/my-project -secret database-password
```

**Azure Key Vault** ([source](examples/providers/azure/main.go)). Uses DefaultAzureCredential, including Azure CLI or managed identity.

```sh
go -C examples run ./providers/azure \
  -vault https://my-vault.vault.azure.net/ -secret database-password
```

**Vault KV v2** ([source](examples/providers/vault/main.go)). Uses `VAULT_ADDR`, `VAULT_TOKEN`, and optional Vault SDK TLS and namespace settings.

```sh
go -C examples run ./providers/vault \
  -mount secret -path production/database-password
```

Replace the public resource identifiers with your own. Vault documents must have the scalar shape `{"value":"..."}`; see [provider contracts](docs/drivers.md#vault-kv-v2) for ownership requirements before enabling writes. The complete programs include imports and command-line flags; their SDK setup and read workflows also appear below.

The examples module requires Go 1.26 because it imports every managed driver. Normal tests compile these provider programs without accessing credentials or services. Provider runs are explicit, and their success output has not been verified against hosted accounts.

## API Index

| API | Example |
| --- | --- |
| [filesecret.New](#mounted-files) | Read mounted files |
| [Reader.Get](#mounted-files) | Read mounted files |
| [awssecret.New](#aws-secrets) | Read from AWS Secrets Manager |
| [gcpsecret.New](#google-secrets) | Read from Google Secret Manager |
| [azuresecret.New](#azure-secrets) | Read from Azure Key Vault |
| [vaultsecret.New](#vault-secrets) | Read from Vault KV v2 |
| [Func.Get](#reader-adapter) | Adapt an existing reader |
| [ErrNotFound](#reader-adapter) | Adapt an existing reader |
| [Writer.Set](#mutation-errors) | Handle an uncertain write |
| [ErrIndeterminate](#mutation-errors) | Handle an uncertain write |
| [secrettest.New](#application-testing) | Test replacements and deletion |
| [Store.Set](#application-testing) | Test replacements and deletion |
| [Store.Delete](#application-testing) | Test replacements and deletion |


## Examples

Provider snippets use resource identifiers from your application configuration. The complete programs accept those identifiers as command-line flags.

<a id="mounted-files"></a>

### Read mounted files

[Complete source](examples/files/main.go)

```go
root, err := os.OpenRoot(directory)
if err != nil {
	panic(err)
}
defer root.Close()

reader, err := filesecret.New(root, map[string]string{
	"DATABASE_USERNAME": "database-username",
	"DATABASE_PASSWORD": "database-password",
})
if err != nil {
	panic(err)
}

ctx := context.Background()
username, err := reader.Get(ctx, "DATABASE_USERNAME")
if err != nil {
	panic(err)
}
password, err := reader.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	panic(err)
}
// Pass both values directly to your database client. Keep them out of logs.
_, _ = username, password
fmt.Println("database credentials loaded")
// Output: database credentials loaded
```

<a id="aws-secrets"></a>

### Read from AWS Secrets Manager

[Complete source](examples/providers/aws/main.go)

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
cfg, err := config.LoadDefaultConfig(ctx)
if err != nil {
	return err
}
client := secretsmanager.NewFromConfig(cfg)
store, err := awssecret.New(client, map[string]awssecret.Binding{
	"DATABASE_PASSWORD": {ARN: arn, Encoding: awssecret.Text},
})
if err != nil {
	return err
}
password, err := store.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	return err
}
// Pass password to your database client instead of logging it.
_ = password
fmt.Println("credential loaded")
// Output: credential loaded
```

<a id="google-secrets"></a>

### Read from Google Secret Manager

[Complete source](examples/providers/gcp/main.go)

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
client, err := secretmanager.NewClient(ctx)
if err != nil {
	return err
}
defer client.Close()
store, err := gcpsecret.New(client, parent, map[string]string{
	"DATABASE_PASSWORD": id,
})
if err != nil {
	return err
}
password, err := store.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	return err
}
// Pass password to your database client instead of logging it.
_ = password
fmt.Println("credential loaded")
// Output: credential loaded
```

<a id="azure-secrets"></a>

### Read from Azure Key Vault

[Complete source](examples/providers/azure/main.go)

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
credential, err := azidentity.NewDefaultAzureCredential(nil)
if err != nil {
	return err
}
client, err := azsecrets.NewClient(vault, credential, nil)
if err != nil {
	return err
}
store, err := azuresecret.New(client, map[string]azuresecret.Binding{
	"DATABASE_PASSWORD": {Name: name},
})
if err != nil {
	return err
}
password, err := store.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	return err
}
// Pass password to your database client instead of logging it.
_ = password
fmt.Println("credential loaded")
// Output: credential loaded
```

<a id="vault-secrets"></a>

### Read from Vault KV v2

[Complete source](examples/providers/vault/main.go)

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
client, err := api.NewClient(nil)
if err != nil {
	return err
}
store, err := vaultsecret.New(client.KVv2(mount), map[string]string{
	"DATABASE_PASSWORD": path,
})
if err != nil {
	return err
}
password, err := store.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	return err
}
// Pass password to your database client instead of logging it.
_ = password
fmt.Println("credential loaded")
// Output: credential loaded
```

<a id="reader-adapter"></a>

### Adapt an existing reader

[Complete source](examples/reader/main.go)

```go
reader := secret.Func(func(ctx context.Context, name string) (string, error) {
	if name != "DATABASE_PASSWORD" {
		return "", secret.ErrNotFound
	}
	return "public-example-password", nil
})

_, err := reader.Get(context.Background(), "DATABASE_PASSWORD")
fmt.Println("loaded:", err == nil)
// Output: loaded: true

_, err = reader.Get(context.Background(), "UNKNOWN")
fmt.Println("missing:", errors.Is(err, secret.ErrNotFound))
// Output: missing: true
```

<a id="mutation-errors"></a>

### Handle an uncertain write

[Complete source](examples/errors/main.go)

```go
err := writer.Set(context.Background(), "DATABASE_PASSWORD", "public-replacement")
if errors.Is(err, secret.ErrIndeterminate) {
	// Read back or reconcile through provider tooling before attempting another write.
	fmt.Println("check provider state before retrying")
	// Output: check provider state before retrying
} else if err != nil {
	panic(err)
}
fmt.Println("deadline still detectable:", errors.Is(err, context.DeadlineExceeded))
// Output: deadline still detectable: true
```

<a id="application-testing"></a>

### Test replacements and deletion

[Complete source](examples/testing_test.go)

```go
store := secrettest.New(t, map[string]string{
	"DATABASE_PASSWORD": "public-test-password",
})
ctx := context.Background()

if err := store.Set(ctx, "DATABASE_PASSWORD", "public-replacement"); err != nil {
	t.Fatal(err)
}
_, err := store.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	t.Fatal(err)
}
fmt.Println("replacement readable:", err == nil)
// Output: replacement readable: true

if err := store.Delete(ctx, "DATABASE_PASSWORD"); err != nil {
	t.Fatal(err)
}
_, err = store.Get(ctx, "DATABASE_PASSWORD")
if !errors.Is(err, secret.ErrNotFound) {
	t.Fatal("deleted fixture remains readable")
}
fmt.Println("removed:", errors.Is(err, secret.ErrNotFound))
// Output: removed: true
```



## Development

```sh
make test
make test-race
make vet
make test-integration
make test-coverage
make generate
make check-generated
make modules-check
```

Integration tests require Docker. They use real provider SDKs and isolated public fixtures. Unit coverage is measured independently from integration coverage and enforced at exactly 100% of library statements. Badges are generated from passing test evidence tied to the library source. README snippets come from the example programs.

See [development and releases](docs/development.md) and the [accepted design](docs/design.md) for detailed contracts and verification policy.

## License

MIT. See [LICENSE](LICENSE).
