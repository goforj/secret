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

[Go CDK secrets](https://gocloud.dev/howto/secrets/) is a portable encryption/decryption API backed by key-management services, not an equivalent named-secret store. It is useful for a different boundary and is not treated as a missing feature here.

The existing Reader, Writer, Deleter, Store, and Func surface remains appropriate. Provider SDKs retain their own credentials, transport, endpoint, logging, and lifecycle configuration. Features such as environment fallback, JSON field selection, global registries, automatic rotation, and implicit caching remain outside this API.

## Verification

The candidate was checked on macOS arm64 using Go 1.27.3:

- All ten modules are inventoried and tidied, with no third-party dependency or checksum changes.
- All-module tests, race tests, and vet pass, including the child example programs and the new uncertain-write example.
- Runtime unit evidence records 81 executed cases and 616 of 616 statements covered. Integration evidence is separate.
- Ten integration cases pass using the real SDKs against pinned AWS, Google gRPC/REST, and Azure emulators and a real Vault server.
- govulncheck v1.9.0 reports no vulnerabilities in any of the ten modules with Go 1.27.3. An initial scan using the machine's Go 1.27.0 reported standard-library advisory GO-2026-6613; the system Go installation was not changed.
- README generation is repeatable, and two further generations leave the README and badges unchanged.

Candidate archive verification uses the repository's release tool from a clean committed preparation branch. It checks all seven independently published modules in fresh consumers without local replacements, verifies their module checksums and selected root version, and compiles each at its declared minimum Go version. The local `.artifacts/release-archive-v0.1.0.json` records the exact tested candidate commit and checksums when that check completes. No tag or publication is performed by that tool.

## Publication boundaries

Hosted AWS, Google, and Azure accounts have not been exercised. Emulator tests do not establish hosted IAM, KMS, regional policy, or retention behavior. Linux and Windows confinement remain covered by the existing CI matrix; this local pass ran the actual macOS descriptor and symlink tests.

The library remains unreleased. Publish the tested root first, verify it, then publish and verify each independently tagged runtime module. Any candidate changes require a new archive check. The published-module verifier must run through the public proxy after authorized publication; a private candidate proxy is not publication evidence.
