# Release review

Reviewed on 2026-11-09 for the initial v0.1.0 candidate. Three serial passes covered implementation and concurrency, API ergonomics and alternatives, and release readiness. This review preserves the existing public capabilities and module Go minimums.

## Findings and changes

| Finding | Resolution |
| --- | --- |
| A fixture Set or Delete canceled while waiting for its mutex could still mutate the map. | Both operations recheck cancellation under the lock. A deterministic regression reproduced both failures before the fix and now verifies that the original fixture survives. |
| The design described a fixed fixture catalog, while the documented helper permits creating entries. | Updated the design to describe the established testing convenience and its distinction from provisioned production resources. |
| Method parameters were unnamed in the root interfaces. | Named and documented the parameters without changing method signatures or implementations required of consumers. |
| Uncertain writes lacked a runnable consumer example. | Added an executed example that checks ErrIndeterminate before a deadline error and demonstrates that both classifications remain detectable. README snippets and API links are generated from it. |
| Nested modules still required the unreleased v0.0.0 placeholder. | Aligned all runtime, example, and integration sibling requirements to v0.1.0, preserving relative development replacements. |
| Release planning accepted malformed versions and major versions incompatible with the module paths. | Added canonical v0/v1 version validation and direct tests for valid versions, prereleases, numeric leading zeros, build metadata, traversal, and unsupported major versions. |

The cancellation fix changes runtime behavior only for fixture mutations canceled while queued. There are no source/API removals, configuration migrations, persisted-data changes, new runtime dependencies, or minimum-Go increases. The sibling version changes prepare an unpublished release; they do not prove public module availability.

## Similar libraries

These comparisons use the projects' primary documentation as reviewed on the date above. They compare contracts and tradeoffs, rather than claiming a security audit of those projects.

| Library | Useful strengths | Decision for secret |
| --- | --- | --- |
| [Go CDK runtimevar](https://gocloud.dev/howto/runtimevar/) | Portable watched configuration, provider URLs, decoders, and latest-good snapshots. | Keep direct current-value reads and caller-owned clients. Add a watcher only if a concrete application needs its lifecycle and failure semantics. |
| [helmfile/vals](https://github.com/helmfile/vals#go) | Expands provider references throughout configuration documents, with broad provider support and an LRU cache. | Keep application names bound during construction. Configuration expansion, runtime locator syntax, and cache policy would enlarge this library's contract substantially. |
| [AWS Secrets Manager caching client](https://github.com/aws/aws-secretsmanager-caching-go#cache-configuration) | AWS-specific caching with refresh TTL, version stages, and hooks; its documented failure policy can serve stale data after a failed refresh. | Keep cache and stale-value policy explicit outside the core. A caller needing that behavior can adapt its retrieval through Func. |
| [rsjethani/secret](https://github.com/rsjethani/secret) | Secret value types with redacted formatting and serialization, plus explicit extraction. | This is complementary protection for values after retrieval. Keep ordinary strings for client interoperability, and clearly document caller responsibility for logging and storage. |

[Go CDK secrets](https://gocloud.dev/howto/secrets/) is a portable encryption/decryption API backed by key-management services. Local at-rest encryption is now provided by `filesecret.NewStore`, with key management kept caller-owned. The original mounted-file-only scope did not satisfy encrypted local storage requirements; the design amendment below addresses that gap.

The existing Reader, Writer, Deleter, Store, and Func surface remains appropriate. Provider SDKs retain their own credentials, transport, endpoint, logging, and lifecycle configuration. Features such as environment fallback, JSON field selection, global registries, automatic rotation, and implicit caching remain outside this API.

## Initial verification

The candidate was checked on macOS arm64 using Go 1.27.3:

- All ten modules are inventoried and tidied, with no third-party dependency or checksum changes.
- All-module tests, race tests, and vet pass, including the child example programs and the new uncertain-write example.
- Runtime unit evidence records 83 executed cases and 617 of 617 statements covered. Integration evidence is separate.
- Ten integration cases pass using the real SDKs against pinned AWS, Google gRPC/REST, and Azure emulators and a real Vault server.
- govulncheck v1.9.0 reports no vulnerabilities in any of the ten modules with Go 1.27.3. An initial scan using the machine's Go 1.27.0 reported standard-library advisory GO-2026-6613; the system Go installation was not changed.
- README generation is repeatable, and two further generations leave the README and badges unchanged.

Candidate archive verification uses the repository's release tool from a clean committed preparation branch. It checks all seven independently published modules in fresh consumers without local replacements, verifies their module checksums and selected root version, and compiles each at its declared minimum Go version. The local `.artifacts/release-archive-v0.1.0.json` records the exact tested candidate commit and checksums when that check completes. No tag or publication is performed by that tool.

## Publication boundaries

Hosted AWS, Google, and Azure accounts have not been exercised. Emulator tests do not establish hosted IAM, KMS, regional policy, or retention behavior. The follow-up also ran the descriptor and symlink suite with the race detector on Linux arm64 (Go 1.27.2) and macOS arm64 at the file module's Go 1.25.12 minimum. Windows execution remains covered by the existing CI matrix; a local Windows cross-compilation is not a substitute for executing that suite.

The library remains unreleased. Publish the tested root first, verify it, then publish and verify each independently tagged runtime module. Any candidate changes require a new archive check. The published-module verifier must run through the public proxy after authorized publication; a private candidate proxy is not publication evidence.

## Follow-up release review

A second round on 2026-11-09 reviewed runtime request ownership, the release trust boundary, and final candidate packaging. It found and addressed these additional issues:

| Finding | Resolution and regression evidence |
| --- | --- |
| AWS SDK inputs referenced the retained binding ARN directly. A client mutating that input could redirect later operations. | Each SDK request receives a fresh pointer to its resource ID. A test mutates every request and checks that subsequent operations still target the original binding. It failed before the fix. |
| A client could mutate the AWS input's request-token pointer, causing reconciliation to accept a different version as proof of this write. | The request receives a copy of the token. A regression simulates the mutation and now requires ErrIndeterminate instead of false success. It failed before the fix. |
| Archive generation repeatedly read HEAD and used working-tree files to identify module boundaries and Go minimums. | Source, module declarations, module boundaries, inherited licenses, and timestamps now come from a single captured commit. A fixture advances HEAD and adds an uncommitted module boundary while verifying the original snapshot. NUL-delimited tree enumeration also preserves Unicode filenames. |
| Publication verification trusted local tags, which can diverge from origin. | The verifier queries origin and resolves lightweight and annotated tags. A local bare-origin regression moves the local tag and proves the verifier still reads the remote commit. No hosted repository is mutated by this test. |
| There was no root-only verification step for root-first publication. | `release-published MODULE=.` verifies the root, and an explicit runtime module directory verifies a later individual publication. Archive checks always retain the complete module set. |
| Archive verification inherited a disabled checksum database from the caller. | The verifier forces the public checksum database on for external dependencies. A test runs the actual Go command with conflicting caller settings and checks the resulting environment. |
| Independent consumers did not exercise all SDK dependency graphs together. | Full archive and publication verification now also build one consumer importing all seven runtime modules, verify its module graph, and reject checkout resolution or unexpected versions. |

These changes retain the public library interfaces, driver construction, stored data, provider configuration, and minimum Go versions. They improve ownership of request data and release verification without adding runtime dependencies. The AWS request ownership, token reconciliation, and mutation serialization regressions are additionally stressed under the race detector.

The local release candidate remains unpublished. Final archive evidence must match the latest committed candidate, including the combined consumer check. Root-first and per-module public verification commands are available for use after each authorized publication. Hosted account checks and actual Windows execution remain outside this local pass.

## Encrypted local storage review

The original review accepted a plaintext mounted-file reader without establishing the encryption requirement for local persistent secrets. `filesecret.NewStore` now adds authenticated AES-256-GCM storage with a caller-supplied 32-byte key. The existing `New` API and plaintext files remain unchanged. No module minimum, third-party dependency, or existing configuration changes. The versioned encrypted format is new, and plaintext migration requires an explicit write into a separate directory.

The implementation pass covered binding-bound associated data, random nonces, bounded reads, ciphertext-only staging, atomic Unix replacement, directory flushing, authentication before destructive operations, and conservative post-dispatch error classification. The failure pass covered wrong keys, corruption, substitution, truncation, staging write/flush/close failures, cancellation at publication boundaries, and canceled lock waiters. The compatibility pass retained mounted-volume behavior and checked all ten modules plus an isolated encrypted-storage consumer.

Validation uses Go 1.27.3 on macOS arm64: all-module race tests and vet pass; runtime evidence records 92 unit cases and 720 of 720 statements; all ten disposable-provider integration cases pass. The encrypted example executes in the shared example runner, and README generation is repeatable. The file suite also passes with the race detector on macOS Go 1.25.12 and Linux arm64 Go 1.27.2. Windows cross-compilation passes; Windows runtime validation remains in CI. Security scans find no reachable vulnerabilities; the examples dependency graph contains an advisory for an unimported OpenPGP package.

The key is never persisted by the library. Key recovery and fewer than 2^32 encryptions per key remain caller responsibilities. The later multi-key amendment below replaces the original separate-directory rotation workflow. Filesystem rollback detection, filename and length confidentiality, secure erasure, cross-process locking, and Windows Unix-style directory durability are not claimed. These limits are documented in the provider contracts rather than hidden behind a broad encryption guarantee.

## Multi-key rotation review

[Laravel's graceful key rotation](https://laravel.com/framework/docs/13.x/encryption#gracefully-rotating-encryption-keys) uses the current key for encryption and tries previous keys during decryption. It does not automatically rewrite stored ciphertext. The file store adopts that behavior through `NewStoreWithKeys(root, currentKey, bindings, previousKeys...)` while preserving the exact existing `NewStore` signature and version 1 file format. Every configured key is validated and copied; only the current key encrypts new values.

`Get` remains read-only. The explicit `Rotate(ctx)` operation holds each binding's mutation gate across authentication and replacement, so a concurrent `Set` cannot be overwritten with stale rotation data. It skips absent and already-current files. Sorted iteration and first-error termination make partial migrations resumable. Old keys must remain recoverable for unprocessed files and retained backups; no successful rotation claims to have scanned unbound files or backups. Rotation is a backend method, leaving the root Reader and Store interfaces unchanged.

Regression coverage includes multiple previous keys, current-key precedence, caller key/slice mutation, malformed previous keys, exact empty and binary values, unchanged ciphertext after fallback reads, current-key-only writes, dropping previous keys after migration, retry after a corrupt-file failure, cancellation before publication, and overlapping rotation and writes. All-module race tests and vet pass on macOS Go 1.27.3. Runtime evidence records 98 cases with 756 of 756 statements covered, and all ten provider integration cases pass. The new example is executed by the shared runner. The file suite passes on macOS Go 1.25.12 and Linux arm64 Go 1.27.2, and Windows cross-compilation passes. Existing dependencies and minimum Go versions are unchanged.
