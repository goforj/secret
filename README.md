<p align="center">
  <img src="docs/assets/logo.png" width="300" alt="secret">
</p>

<p align="center">Read, replace, and delete bound secrets through a small Go API.</p>

<p align="center">
  <img src="docs/assets/badges/summary-status.svg" alt="Status: unreleased">
  <img src="docs/assets/badges/summary-go.svg" alt="Go: 1.24.4+">
  <img src="docs/assets/badges/summary-license.svg" alt="License: MIT">
  <img src="docs/assets/badges/summary-coverage.svg" alt="Library unit coverage: 100%">
  <img src="docs/assets/badges/summary-tests.svg" alt="Executed library unit tests">
</p>

<p align="center">
  <a href="#features">Features</a> · <a href="#quick-start">Quick Start</a> · <a href="#drivers">Drivers</a> · <a href="#installation">Installation</a> · <a href="#runnable-examples">Examples</a> · <a href="#api-index">API</a>
</p>

## Features

<table>
<thead><tr><th align="left">Feature</th><th align="left">What you get</th></tr></thead>
<tbody>
<tr><td width="240" nowrap><img src="docs/assets/badges/feature-icon-simple.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Small capabilities</strong></td><td>Read, replace, or delete with ordinary strings.</td></tr>
<tr><td width="240" nowrap><img src="docs/assets/badges/feature-icon-bindings.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Explicit bindings</strong></td><td>Choose provider locations once during construction.</td></tr>
<tr><td width="240" nowrap><img src="docs/assets/badges/feature-icon-bytes.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Exact values</strong></td><td>Preserve bytes within each provider&#39;s documented limits.</td></tr>
<tr><td width="240" nowrap><img src="docs/assets/badges/feature-icon-errors.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Safe failures</strong></td><td>Classify failures without exposing provider data.</td></tr>
<tr><td width="240" nowrap><img src="docs/assets/badges/feature-icon-mutations.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Deliberate mutations</strong></td><td>Serialize same-secret writes and report uncertain outcomes.</td></tr>
<tr><td width="240" nowrap><img src="docs/assets/badges/feature-icon-testing.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Isolated testing</strong></td><td>Use a concurrency-safe store for application fixtures.</td></tr>
</tbody>
</table>


## Quick Start

Use the same application name with a mounted file or a managed provider. The `directory` below is the path to your mounted secrets directory. The caller owns and closes the root.

```go
root, err := os.OpenRoot(directory)
if err != nil {
	panic(err)
}
defer root.Close()

reader, err := filesecret.New(root, map[string]string{
	"DATABASE_PASSWORD": "database-password",
})
if err != nil {
	panic(err)
}

password, err := reader.Get(context.Background(), "DATABASE_PASSWORD")
if err != nil {
	panic(err)
}
// Pass password directly to your database client. Keep it out of logs.
_ = password
fmt.Println("credential loaded")
// Output: credential loaded
```

Import `github.com/goforj/secret/driver/filesecret` for this example. The complete [runnable program](examples/files/main.go) creates a public fixture directory first. File confinement requires Go 1.25.12+, Go 1.26.5+, or stable Go 1.27+.

Application services can accept `secret.Reader`. Workflows that replace or delete values can accept `secret.Writer`, `secret.Deleter`, or `secret.Store`.

## Drivers

<table>
<thead><tr><th align="left">Driver</th><th align="left">Behavior</th><th align="left">Verification</th></tr></thead>
<tbody>
<tr><td width="260" nowrap><img src="docs/assets/badges/driver-icon-aws.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/awssecret"><strong>AWS Secrets Manager</strong></a></td><td>Text or binary; standard recovery window</td><td>Moto 5.1.16 emulator</td></tr>
<tr><td width="260" nowrap><img src="docs/assets/badges/driver-icon-azure.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/azuresecret"><strong>Azure Key Vault</strong></a></td><td>UTF-8; vault-configured soft deletion</td><td>Lowkey Vault 7.4.0 emulator</td></tr>
<tr><td width="260" nowrap><img src="docs/assets/badges/driver-icon-files.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/filesecret"><strong>Mounted files</strong></a></td><td>Read exact bytes beneath a caller-owned root</td><td>Real filesystem and projected-volume tests</td></tr>
<tr><td width="260" nowrap><img src="docs/assets/badges/driver-icon-google.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/gcpsecret"><strong>Google Secret Manager</strong></a></td><td>Binary values, CRC32C, and ETag deletion</td><td>Google Secret Manager emulator (pinned image)</td></tr>
<tr><td width="260" nowrap><img src="docs/assets/badges/driver-icon-vault.svg" width="22" height="22" alt="" align="absmiddle"> <a href="driver/vaultsecret"><strong>Vault KV v2</strong></a></td><td>Owned scalar paths; CAS and all-version deletion</td><td>Real Vault 1.21.4 server</td></tr>
</tbody>
</table>


Cloud verification uses the listed emulators and real provider SDKs. Hosted AWS, Google, and Azure behavior has not been verified against dedicated accounts. See [provider contracts](docs/drivers.md) for value limits, deletion behavior, construction, and emulator boundaries.

## Installation

The library is unreleased. Clone the repository to run every example against the local modules:

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

Azure and Vault pin golang.org/x/net v0.62.0 to fix known HTTP/2 vulnerabilities; that dependency requires Go 1.26. Google's pinned SDK stack also requires Go 1.26. These requirements do not affect the core, test support, or AWS driver. The file driver requires the patched `os.Root` implementation; Go 1.26.0 through 1.26.4 and prerelease toolchains are unsupported.

Development replacements intentionally exercise unpublished sibling modules. Release tooling validates candidate archives through an isolated module proxy before any tags are created. See [development and releases](docs/development.md).

## Runnable Examples

| Workflow | Run from this checkout |
| --- | --- |
| [Mounted files](examples/files/main.go) | `go -C examples run ./files` |
| [Reader adapter](examples/reader/main.go) | `go -C examples run ./reader` |
| [Uncertain writes](examples/errors/main.go) | `go -C examples run ./errors` |
| [Application tests](examples/testing_test.go) | `go -C examples test -run TestStore -v` |

Every program is executed by the shared example runner and checked against the output comments beside its calls. Test fixtures use public placeholder values.

## API Index

| API | Example |
| --- | --- |
| [filesecret.New](#mounted-files) | Read mounted files |
| [Reader.Get](#mounted-files) | Read mounted files |
| [Func.Get](#reader-adapter) | Adapt an existing reader |
| [ErrNotFound](#reader-adapter) | Adapt an existing reader |
| [Writer.Set](#mutation-errors) | Handle an uncertain write |
| [ErrIndeterminate](#mutation-errors) | Handle an uncertain write |
| [secrettest.New](#application-testing) | Test replacements and deletion |
| [Store.Set](#application-testing) | Test replacements and deletion |
| [Store.Delete](#application-testing) | Test replacements and deletion |


## Examples

<a id="mounted-files"></a>

### Read mounted files

```go
root, err := os.OpenRoot(directory)
if err != nil {
	panic(err)
}
defer root.Close()

reader, err := filesecret.New(root, map[string]string{
	"DATABASE_PASSWORD": "database-password",
})
if err != nil {
	panic(err)
}

password, err := reader.Get(context.Background(), "DATABASE_PASSWORD")
if err != nil {
	panic(err)
}
// Pass password directly to your database client. Keep it out of logs.
_ = password
fmt.Println("credential loaded")
// Output: credential loaded
```

<a id="reader-adapter"></a>

### Adapt an existing reader

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



## Behavior

Names follow `[A-Z][A-Z0-9_]*`, with a maximum of 255 bytes. Names are opaque application identifiers such as `DATABASE_PASSWORD`. Bindings are copied at construction, and unknown names never reach a provider.

Values are ordinary strings. Reads preserve exact bytes within the provider's capabilities. No trimming, JSON parsing, automatic environment fallback, caching, or rotation is added. Returned strings belong to the caller and must stay out of logs and diagnostics.

`Set` publishes a new current value at an existing resource. `Delete` targets the entire resource using its provider's retention policy. A Store serializes same-binding mutations; different bindings and reads can proceed concurrently. Mutable bindings require exclusive lifecycle ownership. Provider SDK retries remain caller-owned except Google version creation, where the driver disables automatic retries.

Errors support `errors.Is` with `ErrInvalid`, `ErrNotFound`, `ErrPermission`, `ErrUnavailable`, and `ErrIndeterminate`. An indeterminate mutation may have committed; reconcile before retrying. Caller cancellation remains detectable, including alongside `ErrIndeterminate`. Provider errors and response bodies are never retained in the public error graph.

Check `ErrIndeterminate` before cancellation or deadline errors when handling a failed mutation. A canceled request can still have committed remotely. The [uncertain-write example](#mutation-errors) demonstrates this ordering without contacting a provider.

Constructing code owns the SDK client or file root. Drivers borrow them and never close them. Review SDK logging separately: first-party drivers do not log, but injected client logging can disclose provider metadata.

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

Integration tests require Docker. Each service runs once per package, uses isolated public fixtures, and is cleaned up after failures. Unit coverage is measured independently from integration coverage and enforced at exactly 100% of library statements. Documentation and badge counts are generated from source-bound passing test evidence.

See [development and releases](docs/development.md) and the [accepted design](docs/design.md) for detailed contracts and verification policy.

## License

MIT. See [LICENSE](LICENSE).
