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
