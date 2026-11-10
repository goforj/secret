# Provider contracts

Application code uses `secret.Reader`, `secret.Writer`, `secret.Deleter`, or `secret.Store`. Construction fixes resource locations and copies binding configuration. Every driver validates a complete binding catalog before returning. Clients must support concurrent calls; their standard SDK implementations do.

Managed mutations require exclusive lifecycle ownership: other actors must not delete, purge, recreate, repoint, or repurpose a bound resource concurrently. Native rotation that only publishes a new version remains allowed. Same-binding mutations serialize within one Store, including preflight and reconciliation. Cancellation while waiting makes no provider call and never returns `ErrIndeterminate`.

Managed drivers never provision resources. Configure policies, ownership, access grants, replication, retention, mounts, and clients explicitly outside the library. The encrypted local store may create bound files inside an existing directory. Injected SDK clients, encryption keys, and file roots remain caller-owned. SDK retry and logging settings are also caller-owned, except Google AddSecretVersion retries, which are disabled per call.

| Driver | Read selection | Value representation and limit | Whole-resource deletion |
| --- | --- | --- | --- |
| AWS | AWSCURRENT | Configured text or binary, 1 to 65,536 bytes | Standard AWS recovery window, normally 30 days |
| Google | latest | Arbitrary bytes, 0 to 65,536 bytes; CRC32C required | ETag-protected secret and all versions |
| Azure | Latest version | Valid UTF-8, 0 to 25,000 bytes | Vault-configured soft deletion |
| Vault KV v2 | Latest version only | Valid UTF-8 scalar, 0 to 1 MiB | All versions and metadata, irreversible |
| Encrypted local file | Authenticated ciphertext at each read | Arbitrary plaintext bytes, 0 to 1 MiB | Unlinks ciphertext; no secure-erasure guarantee |
| Mounted file | Contents at each read | Arbitrary bytes, 0 to 1 MiB | Reader only |

The portable intersection for managed writes is nonempty valid UTF-8 of at most 25,000 bytes. A successful driver operation preserves exactly the values supported by that provider. The library adds no cache, environment fallback, parsing, trimming, or automatic rotation.

## AWS Secrets Manager

Import `github.com/goforj/secret/driver/awssecret` and inject a standard `*secretsmanager.Client`:

```go
store, err := awssecret.New(client, map[string]awssecret.Binding{
    "DATABASE_PASSWORD": {
        ARN:      "arn:aws:secretsmanager:us-east-1:123456789012:secret:production/database-AbCdEf",
        Encoding: awssecret.Text,
    },
})
```

Full canonical ARNs prevent name/ARN aliases from identifying the same mutable resource. Duplicate ARNs are rejected. Text bindings accept only UTF-8 SecretString; binary bindings accept only SecretBinary. Neither, both, empty, oversized, or mismatched representations are malformed read responses.

Set describes the secret first and refuses an already scheduled deletion. It supplies a fresh cryptographic UUID as ClientRequestToken to one PutSecretValue call, stable across SDK retries. ResourceNotFound is definite because PutSecretValue cannot create a secret. InvalidRequest triggers one DescribeSecret reconciliation; only an entry for that request token proves commitment. Token absence never proves non-commit, even when a later deletion is visible. SDK requests receive independent copies of the resource ID and request token, so client middleware cannot change retained binding policy or the token used for reconciliation.

Delete describes the secret, accepts absence or scheduled deletion, and otherwise requests default recovery behavior. InvalidRequest triggers one reconciliation, accepting only a now-scheduled deletion. The driver never forces destruction or changes recovery policy. IAM must permit DescribeSecret alongside the requested read/write/delete operations.

## Google Secret Manager

Import `github.com/goforj/secret/driver/gcpsecret` and inject a standard gRPC or REST Secret Manager client:

```go
store, err := gcpsecret.New(client, "projects/my-project", map[string]string{
    "DATABASE_PASSWORD": "database-password",
})
```

The parent can also be `projects/{project}/locations/{location}`. Configure the client's regional endpoint separately. Secret IDs contain 1 to 255 ASCII letters, digits, underscores, or hyphens, and must be unique within the catalog.

Get requires a payload with a matching Castagnoli CRC32C checksum. Empty and arbitrary byte values remain valid. Set sends exact bytes with CRC32C in one AddSecretVersion call and disables automatic retries because the API has no idempotency token. It never calls CreateSecret.

Delete first obtains the secret's ETag, then deletes the whole resource with that ETag. Missing resources succeed idempotently. A failed precondition is a definite conflict; other post-dispatch failures are indeterminate. A new Delete call gets a new ETag, so retry safety still requires exclusive lifecycle ownership.

## Azure Key Vault

Import `github.com/goforj/secret/driver/azuresecret` and inject an `*azsecrets.Client` configured for one vault:

```go
store, err := azuresecret.New(client, map[string]azuresecret.Binding{
    "DATABASE_PASSWORD": {
        Name: "database-password",
    },
})
```

Names contain 1 to 127 ASCII letters, digits, or hyphens. Case-insensitive aliases are rejected. Optional ContentType, Enabled, NotBefore, Expires, and Tags are validated and deeply copied. That construction policy is authoritative for every replacement version. Metadata changed by external actors may be overwritten.

Set checks that a latest version exists, then calls SetSecret. Azure has no conditional create guard: an external delete-and-purge between these calls can allow recreation. Exclusive lifecycle ownership is required. Applications unable to enforce it use this driver only through Reader and perform mutations through provider tooling.

Delete accepts HTTP 404 and the exact nested `error.innererror.code=ObjectIsBeingDeleted` state from HTTP 409. Top-level Conflict or matching prose is insufficient. Response inspection is bounded to 64 KiB. The driver never recovers or purges deleted secrets.

Lowkey Vault 7.4.0 rejects empty writes. Integration tests verify the resulting uncertain mutation classification and test nonempty round trips, version replacement, and repeated deletion. Empty value preservation is covered by local SDK protocol tests; hosted empty-value behavior remains unverified.

## Vault KV v2

Import `github.com/goforj/secret/driver/vaultsecret` and inject a configured `client.KVv2("secret")`:

```go
store, err := vaultsecret.New(client.KVv2("secret"), map[string]string{
    "DATABASE_PASSWORD": "production/database-password",
})
```

Paths are relative, 1 to 1,024 bytes, without empty, dot, or dot-dot components. Every path owns one complete data document containing exactly `{"value": "..."}`. Provision mutable paths with custom metadata `goforj-secret-format=scalar-v1`. Unmarked or shared documents are refused.

Set validates the current document, requires a positive version number and the durable ownership marker, then performs a CAS Put. A final CAS error can follow a committed first attempt and is indeterminate. Values exceeding the local cap are invalid before I/O; values within it rejected by a lower server cap after Put are indeterminate.

Delete validates ownership metadata even when every data version is soft-deleted or destroyed. A readable latest document must still have the owned scalar shape. It then removes all metadata and versions. This is irreversible. Wrapped `api.ErrSecretNotFound` from reads is absence; the same sentinel after Put or DeleteMetadata does not prove non-commit.

The caller owns Vault authentication and token renewal. The driver never logs in, renews credentials, or falls back to earlier versions.

## Encrypted local files

Import `github.com/goforj/secret/driver/filesecret` and call `filesecret.NewStore(root, key, bindings)`. The root is a caller-owned `*os.Root`, the key is exactly 32 random bytes, and bindings map application names to relative file paths. Identical paths are rejected. The store implements `secret.Store`; `Set` can create an absent bound file, but never creates parent directories. See the [runnable example](../examples/encrypted/main.go).

**Encryption.** Every value is encrypted with standard-library AES-256-GCM and a fresh random 96-bit nonce. The application name and relative path are authenticated as associated data. Wrong keys, modified ciphertext, swapped bindings, unsupported versions, and plaintext files return `ErrUnavailable` without a value. Existing files must authenticate before replacement or deletion; there is no plaintext fallback or automatic migration. Stored data contains the version header, nonce, ciphertext, and authentication tag, with no plaintext values or key material.

**Key ownership.** Supply a cryptographically random key from your application's key manager, OS credential store, or deployment secret system. The constructor copies key material into the cipher; it never saves or derives a key from a password. Keep keys outside the secret directory and its backups. Losing them loses access to the values; storing them beside the ciphertext defeats protection against directory theft. Follow Go's GCM limit of fewer than 2^32 encryptions per key across all instances.

**Key rotation.** `NewStoreWithKeys(root, currentKey, bindings, previousKeys...)` always encrypts with the current key. Reads try that key first, then each previous key in order. `Get` remains read-only, including when a previous key succeeds. Call `store.Rotate(ctx)` to re-encrypt existing bound files in place with the current key. It skips missing and already-current files, preserves exact values, and serializes with `Set` and `Delete` on each binding. The original `NewStore` signature and version 1 file format are unchanged. See the [runnable rotation example](../examples/rotation/main.go).

Rotation processes bindings in name order and stops on the first error. It is not a transaction across files: keep previous keys after a failed or canceled run, resolve the failure, and retry. Successfully migrated files are skipped on the next run. Success covers configured bindings only; unbound files and backups are not scanned. Never retire an old key until all writers use the current key, rotation succeeds, and retained backups no longer require it. During a rolling key change, first deploy readers that accept both keys, then promote the new write key across writers before migrating files. The library never generates, distributes, persists, or retires keys automatically. Only the key fallback and new-write key selection are automatic.

**Filesystem operations.** Use a private directory and trusted parent directories, with exclusive lifecycle ownership across processes. `Set` stages only ciphertext in a random, exclusively created 0600 file, flushes and closes it, then renames it over the bound path. Unix rename provides atomic replacement on local filesystems; the parent directory is synced after replacement and deletion. Windows directory syncing is unavailable through this API, and replacement is subject to the platform's `os.Rename` guarantees. Filesystem and power-loss durability remain platform-dependent. After a replacement or removal is attempted, errors conservatively include `ErrIndeterminate`; reconcile before retrying. Cancellation before publication leaves the existing value unchanged. Cleanup of ciphertext staging files is best effort after failures or crashes.

**Protection boundaries.** Encryption protects stored values, not filenames, file sizes, access patterns, application memory, or a compromised process holding the key. It detects modification and cross-binding substitution, but cannot detect replay of an older valid ciphertext for the same binding. File deletion does not erase earlier blocks, snapshots, or backups. Local encryption does not replace filesystem permissions or full-disk encryption.

The encrypted store uses the same confined path rules and patched toolchain requirements as mounted-file reads below. It adds no external dependencies or minimum Go version increase.

### Persistent keys and recovery

The [configured application example](../examples/configured/persistent/main.go) loads an existing key on every process start. It never generates a replacement, writes secret values, or prints keys or values. Its `loadKey` helper accepts exactly 32 raw bytes from a regular file, caps reads at 33 bytes, and requires owner-only file access on Unix. On Windows, provision equivalent access through ACLs. Use trusted key-file paths and parent directories; the key can come from a deployment secret mount or a separately managed credential file. Other applications can supply the same bytes directly from their key manager without using this helper.

```sh
go -C examples run ./configured/persistent \
  -directory /var/lib/myapp/secrets -key-file /run/secrets/myapp-master.key
```

Replace these paths with your existing encrypted directory and separately provisioned key file. The example reads the `DATABASE_PASSWORD` binding at `database-password.enc`. The key file contains raw random bytes, not a passphrase, base64 text, or a newline-terminated string. Keep recovery copies separately protected and outside ciphertext backups.

If key delivery fails, restore the exact original key through its key manager or protected recovery copy, then restart the same application. Do not generate a replacement key or delete ciphertext to make startup succeed. A wrong or missing key fails closed and leaves the encrypted files unchanged. Lost keys cannot be recovered from ciphertext. After rotation, retain recovery access to old keys for old backups even after removing them from live configuration. Binding names and relative paths are authenticated, so recovery must preserve those identifiers too.

`TestPersistentKeyProgram` builds this actual program and runs separate processes against a fixture: repeated reads, missing and malformed keys, a wrong key, unsafe permissions, corrupt ciphertext, and restoring a protected recovery copy. `TestEncryptedCrashRecovery` separately kills real writer processes after synced staging, after publication before acknowledgement, and partway through rotation, then verifies recovery in new processes. These tests exercise process death, not a machine power cut or storage-device failure. Orphaned staging files remain ciphertext and are not automatically promoted or removed after a crash.

## Mounted files

`filesecret.New(root, bindings)` is explicitly a **plaintext, read-only** mounted-file reader, suitable for Docker secrets and Kubernetes projected volumes. It performs no encryption or decryption. Relative paths are at most 4,096 bytes and reject traversal components, backslashes, NUL, and platform-specific drive syntax. Contained symlinks are allowed for projected volumes. Aliases are harmless for these read-only bindings.

Every Get opens once, stats that descriptor, requires a regular file, and reads at most 1 MiB plus one byte before rejecting oversize data. Unix opens use O_NONBLOCK so FIFOs cannot hang before the type check. Directories, sockets, devices, loops, and symlink escapes are unavailable. Missing files are not found and filesystem access denial is permission failure. Values are never trimmed.

Supported systems are Linux, macOS, and Windows. Patched toolchains are mandatory: Go 1.25.12+ in the 1.25 branch, Go 1.26.5+ in the 1.26 branch, or stable Go 1.27+. The regression test verifies that opening a symlink target ending in a slash cannot escape the root, before any descriptor is returned. Privileged mount changes inside the root are outside this boundary.

## Running provider examples

The programs under `examples/providers/` authenticate through their provider SDKs and read existing secrets. They print `credential loaded` on success without printing the value. They do not provision, replace, or delete resources.

| Example | Authentication and configuration |
| --- | --- |
| [AWS](../examples/providers/aws/main.go) | AWS default credential chain and configured region, including profiles or workload roles |
| [Google](../examples/providers/gcp/main.go) | Application Default Credentials |
| [Azure](../examples/providers/azure/main.go) | DefaultAzureCredential, including Azure CLI or managed identity |
| [Vault](../examples/providers/vault/main.go) | `VAULT_ADDR`, `VAULT_TOKEN`, and optional Vault SDK TLS and namespace settings |

Grant read access to the selected resources. Vault documents must have the scalar shape `{"value":"..."}`. Normal tests compile these programs without accessing credentials or services; hosted example runs remain unverified.

## Verification boundaries

All drivers have independent unit contract suites. SDK errors are tested with unsafe fixture prose and must not survive in returned error graphs. Unit statements are measured independently of integrations.

Cloud integration uses real SDKs against pinned disposable emulators. Vault integration uses an actual Vault server and verifies its reported version. Tests use public values and no developer cloud credentials. They verify real serialization and service behavior, but do not establish hosted IAM, KMS, regional, network-policy, or retention compatibility. Dedicated hosted verification remains pending.

The accepted [design](design.md) contains the complete classification and mutation sequencing tables.

## Provider references

- [AWS GetSecretValue](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html)
- [Google Secret Manager](https://cloud.google.com/secret-manager/docs/reference/rest)
- [Azure Key Vault Secrets](https://learn.microsoft.com/en-us/rest/api/keyvault/secrets)
- [Vault KV v2](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2)
- [Go os.Root](https://pkg.go.dev/os#Root)
- [Go randomized-nonce GCM](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)
