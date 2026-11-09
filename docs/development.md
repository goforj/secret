# Development and releases

## Source ownership

`secret.go` owns the framework-independent capabilities, adapter, and classifications. Independently versioned drivers own their private validation and safe-error helpers. No driver imports root internal packages. `secrettest` is a separate module so normal consumers do not import testing helpers.

Canonical runnable programs live under `examples/`. Their output expectations sit immediately after the producing call. `examples/run_test.go` discovers all programs, builds them with coverage instrumentation, carries race instrumentation into child programs, executes them, and verifies output. Set `SECRET_EXAMPLE_COVERDIR` to an existing directory to retain child coverage data.

The README is generated from `docs/readme/README.md.tmpl`, curated API metadata, and executable example snippets. SVG badges are generated in Go. Feature and driver names remain real text for browser search; icons use distinct filenames and sit outside name links.

Run `make generate`, then `make check-generated`. The latter snapshots the README and assets and compares two generations against them. Reports bind test outcomes to relevant source fingerprints. Stale coverage or integration evidence fails generation.

## Commands

- `make test`: every independent module and executable examples.
- `make test-race`: the same modules and child programs under the race detector.
- `make vet`: every module, including docs and integration fixtures.
- `make tidy`: each module with intentional relative replacements retained.
- `make modules-check`: filesystem inventory compared against the module manifest.
- `make test-coverage`: independent runtime unit profiles and exact covered/total statement enforcement.
- `make test-integration`: SDK contracts against pinned disposable services.
- `make security`: govulncheck across every module on a patched supported toolchain.

Unit and integration evidence remain separate. `docs/reports/unit.json` records exact counts and executed test cases; `docs/reports/integration.json` records the complete provider run. Local protocol tests do not substitute for hosted verification.

## Integration services

Testcontainers owns one service per provider for the package run. The harness registers containers before starting them, uses isolated resources, and tears down every container even after setup or assertion failures. Image digests are pinned in the test source. Fixture credentials are public emulator tokens; developer cloud credentials are never loaded.

- AWS: Moto 5.1.16, including text and binary resources.
- Google: the pinned Blackwell Systems Secret Manager emulator, gRPC, CRC32C, ETags, and exact bytes.
- Azure: Lowkey Vault 7.4.0, latest text versions and repeated soft deletion. It rejects empty values, which is covered explicitly as an emulator limitation.
- Vault: real Vault 1.21.4, version-checked at runtime, including owned soft-deleted paths and refusal to destroy shared paths.

File tests use actual descriptor reads, projected-volume rotation layouts, and concurrent symlink swaps. Linux/macOS CI runs the slash-symlink confinement regression on Go 1.25.12, Go 1.26.5, and stable. Windows runs its descriptor suite separately. Do not remove the patched-toolchain policy merely because a later Go branch satisfies the module directive.

## Module publication

Runtime modules have independent tags:

| Module directory | Tag prefix |
| --- | --- |
| root | vX.Y.Z |
| secrettest | secrettest/vX.Y.Z |
| driver/awssecret | driver/awssecret/vX.Y.Z |
| driver/azuresecret | driver/azuresecret/vX.Y.Z |
| driver/filesecret | driver/filesecret/vX.Y.Z |
| driver/gcpsecret | driver/gcpsecret/vX.Y.Z |
| driver/vaultsecret | driver/vaultsecret/vX.Y.Z |

`examples`, `docs`, and `integration` are untagged tooling modules. Relative replacements are intentional for repository development. `GOWORK=off` alone does not disable them.

`make release-plan VERSION=v0.1.0` prints the intended tags without creating them. Before a release, align nested root requirements with the intended published root version. Run `make release-check VERSION=v0.1.0` only from a clean committed candidate. The current unreleased dependency placeholder is v0.0.0, so use that version for an archive rehearsal of the initial checkout.

The archive verifier packages committed files, excludes nested module boundaries, and serves exact candidate zip, mod, and info files through a private temporary file proxy. Each consumer has no replace directives and uses a fresh temporary module cache. It compiles exported capabilities, executes consumer contract checks, runs `go mod verify`, and rejects replacements, checkout resolution, or unexpected root/module versions. External dependency checksum verification remains enabled.

Archive rehearsal validates unpublished composition. It does not establish that prerequisites are publicly available. For a release, publish the tested root first, verify it independently, and then validate each driver candidate against that published root. Never infer nested-module publication from a root tag.

After each authorized publication, run `make release-published VERSION=vX.Y.Z` from a new consumer/cache through the public module proxy with checksum verification. Check each expected tag points to the tested commit and compare its downloaded archive checksums with the candidate. Do not reuse a private preview cache as evidence of public availability. No command here creates or moves release tags automatically.

## GitHub integration

CI checks formatting, tidy parity, complete module inventory, unit coverage, executable examples, race behavior, vet, generated parity, platform confinement, and disposable integrations. Security scans run on stable patched Go. Codecov upload is conditional on repository variable `CODECOV_ENABLED=true` and the repository's own upload token; this checkout does not assume external app setup is complete.
