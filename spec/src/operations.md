# Operations

## NA-OPS-001 — persistence and deployment

Central uses `/var/lib/pomegranate/central.db`; each operator independently uses
`/var/lib/pomegranate/operator.db`. Production operators require encrypted storage because the
upstream database stores private shards as plaintext.

Coolify terminates TLS and routes the configured public central origin to port 5033 and an optional
operator origin to port 5041. `SERVICE_URL` must be the exact public HTTPS origin. Central requires
WebSocket upgrade support. Each operator requires `TRUSTED_CENTRAL_URLS`; production uses
comma-separated exact HTTPS origins, while the development fixture explicitly permits its internal
HTTP central origin.

Backups are cold or snapshot-consistent and remain split across independent destinations. A
destination must never contain enough distinct operator archives to meet the account threshold.

Container hardening includes non-root execution, read-only root filesystems, dropped capabilities,
no-new-privileges, bounded PIDs/resources, health checks, restarts, and rotated Docker logs.

Production maps separate central and operator Google/GitHub client credentials into the exact
upstream variables. Secret values may be supplied only directly or through their mutually exclusive
`_FILE` form. GitHub callbacks are the exact public service URLs plus `/callback/github` for central
and `/po/callback/github` for operators; wildcard callback matching is not required.

## NA-OPS-002 — public container distribution

Trusted pushes to `main`, semantic version tags, and explicitly dispatched builds publish
`ghcr.io/<repository-owner>/nostr-auth-pomegranate` for `linux/amd64` and `linux/arm64`. Publication
is gated on the same complete repository, live 2-of-3 smoke checks, and current scan rejecting
fixable high or critical runtime vulnerabilities as CI. The image is built from the governed
Pomegranate commit, carries an OCI source link to this repository, and is published by digest with
BuildKit SBOM/provenance plus GitHub build provenance.

The workflow grants package, attestation, artifact-metadata, and OIDC write scopes only to the
publish job. External actions, the QEMU helper image, and the BuildKit daemon image are pinned
immutably. The final job step drops registry credentials and verifies the resulting digest can be
resolved anonymously. Because GitHub keeps package visibility separate from repository visibility,
an administrator must make the package public after its first push; the anonymous-read gate fails
until that is done.

Repository and Compose acceptance use the digest-pinned Deno 2.9.6 test image with Alpine Docker CLI
`29.9.3-r1` and Compose `5.6.1-r0`. These exact package versions must install successfully before
repository or live smoke qualification. A pin refresh retains the non-root test user and existing
hardened runtime.

Both build and runtime stages pin CA certificates `20261020-r0`; runtime OpenSSL libraries pin
`3.5.9-r0`, advancing the existing security floor without changing the digest-pinned base images or
vendored Pomegranate source.

The digest-pinned Go `1.26.10` build toolchain compiles the unchanged vendor manifest and Go module.
Runtime vulnerability qualification must reject every fixable HIGH or CRITICAL finding, including Go
standard-library findings.

Runtime musl/musl-utils `1.2.5-r24` and zlib `1.4-r0` retain the current Alpine security fixes. The
recorded vendor dependency patch advances x/net to `0.62.0`, x/crypto to `0.58.0`, x/sys to `0.49.0`
and klauspost/compress to `1.18.7`; vendor checksums and dependency assertions must match that
graph.
