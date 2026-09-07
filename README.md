# nostr-auth

`nostr-auth` is a reproducible deployment and TypeScript integration boundary for upstream
[Pomegranate](https://github.com/fiatjaf/pomegranate). It packages central and operator services for
threshold FROST signing over NIP-46 without independently reimplementing the signing protocol.

The repository provides:

- an integrity-checked, commit-pinned Pomegranate vendor tree;
- hardened central and operator containers with persistent role-specific storage;
- Google and GitHub OAuth login and operator-recovery integration;
- a typed browser client for onboarding, profiles, NIP-46, and recovery;
- a local 2-of-3 Compose topology and live protocol smoke test; and
- attested `linux/amd64` and `linux/arm64` images published through GitHub Actions.

The normative requirements are in [`spec/src/index.md`](spec/src/index.md). Production setup is
documented in [`docs/deployment-coolify.md`](docs/deployment-coolify.md).

## Pinned supply chain

| Component                 | Pin                                                                                                  |
| ------------------------- | ---------------------------------------------------------------------------------------------------- |
| Pomegranate source        | `ca0e7a9d697a7db0fc918f9b9335bc5b58d2ac3f`                                                           |
| Promenade Go module       | `v0.4.4-0.20260511003220-ce69ab3c4a4d` (`ce69ab3c4a4d9a6079d0986a1a2fd932eb239d26`)                  |
| Promenade browser dealer  | `jsr:@fiatjaf/promenade-trusted-dealer@0.4.3`                                                        |
| Nostr tools               | `jsr:@nostr/tools@2.25.1`                                                                            |
| Go                        | `1.26.6`                                                                                             |
| Go crypto/network modules | `golang.org/x/crypto@0.56.0`; `golang.org/x/net@0.58.0`                                              |
| Builder                   | `golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406`   |
| Runtime                   | `alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40`              |
| Runtime OpenSSL           | `3.5.8-r0`                                                                                           |
| Test runtime              | `denoland/deno:alpine-2.9.6@sha256:aa665f8777136863b5b8a0445a5cdfccff8103b5f40c9a877de5276b04facb1e` |
| Docker CLI in test image  | `29.5.3-r1` with Compose `5.1.4-r1`                                                                  |

`vendor/pomegranate` is an auditable source fork based on the pinned upstream commit. Its lock
records the base and named local patches, while the complete SHA-256 manifest binds every current
file. Docker performs no Pomegranate source fetch: it verifies the requested full commit, the vendor
manifest, dependency pins, generated Go source, and upstream tests before building the binaries. The
upstream Unlicense is preserved in the vendor tree; this deployment boundary is MIT licensed.

## Continuous integration and public image

Pull requests and `main` changes run the complete repository checks, the hardened image build, a
current Trivy scan that rejects fixable high or critical runtime vulnerabilities, and the live
2-of-3 Compose smoke test.

Trusted `main` pushes, `v*.*.*` tags, and manual runs repeat those gates before publishing:

```text
ghcr.io/<repository-owner>/nostr-auth-pomegranate
```

Each publication includes `linux/amd64` and `linux/arm64` manifests, an SBOM, BuildKit provenance,
and GitHub build provenance. The default branch produces `edge`; every build produces a
`sha-<40-character-commit>` tag; version releases additionally produce `<major>.<minor>.<patch>`,
`<major>.<minor>`, and `latest`.

Production deployments should select a version and pin the resolved manifest digest. GitHub keeps
container-package visibility separate from repository visibility. After the first workflow push, a
package administrator must make `nostr-auth-pomegranate` public and rerun the workflow. Its final
anonymous-read gate fails while the package is private.

## Authentication client

The browser client supports both configured providers:

```ts
const googleSession = await client.loginWithGoogle();
const githubSession = await client.loginWithGitHub();
```

Sessions retain their `oauthProvider`, and operator registration binds that provider into the
upstream kind-20444 event. Recovery uses the matching `recoverShardWithGoogle` or
`recoverShardWithGitHub` flow. Popup messages are accepted only from the exact opened window and
configured service origin. Central tokens must have a valid signature, kind, email, age, and signer
matching the central NIP-11 `self` field.

The GitHub flow requests `read:user` and `user:email` and requires a usable verified email address.
OAuth access tokens remain inside the upstream service flow; they are not returned by the TypeScript
API or written to application logs.

## Local development

The development topology is a functional test fixture, not a production trust boundary:

```text
central ─┬─ operator-1
         ├─ operator-2       threshold: 2
         └─ operator-3
```

Start it with Docker:

```sh
docker compose -f compose.yml -f compose.dev.yml up --build
```

Run the repository checks and protocol smoke test in the pinned test image:

```sh
docker compose -f compose.yml -f compose.dev.yml run --rm test-runner task check:all
docker compose -f compose.yml -f compose.dev.yml --profile test run --rm test-runner task smoke
```

The smoke container mounts `/var/run/docker.sock` and maps the socket's host group so its non-root
user can stop and restart operators across Linux and Docker Desktop hosts. That mount is equivalent
to host-level control and is strictly a local test facility. Never enable `test-runner` in
production.

Development creates a central signing key and a 24-hour kind-20443 test token in separate named
volumes. The test runner can read the token but not the central key. Dummy Google and GitHub values
make the production authentication routes testable without granting real provider access.

## Production deployment

Use `compose.yml` with `compose.prod.yml`. The base topology starts central; the optional `operator`
profile starts one colocated operator. Configure each service with independent secrets and storage,
and keep external threshold operators under genuinely independent administrative boundaries.

See [`docs/deployment-coolify.md`](docs/deployment-coolify.md) for routing, OAuth callbacks,
environment variables, storage, and staging acceptance.

## Persistence, backup, and recovery

Central stores account/operator public-share metadata, profile restrictions, email associations, and
NIP-46 handler keys in `/var/lib/pomegranate/central.db`. A central backup alone cannot sign for a
user.

Each operator stores its user's private FROST shard, user pubkey, central URL and pubkey, and OAuth
association in `/var/lib/pomegranate/operator.db`. Upstream bbolt storage does **not** encrypt these
records. Production operator volumes therefore require encrypted host or block storage.

Create cold backups by stopping or quiescing one service at a time, copying its named volume, and
restarting it before moving to another trust boundary. Never place a threshold number of operator
backups in one ordinary destination. The backup manifest checker verifies readable archives and
rejects any destination containing enough distinct operator backups to meet the threshold:

```sh
docker compose run --rm test-runner run --allow-read scripts/backup-check.ts backup-manifest.json
```

Recovery is client-side. The browser authenticates separately to threshold operators, reconstructs
and verifies the pubkey locally, reshards to replacements, and clears temporary buffers. JavaScript
strings, `bigint` values, and garbage collection prevent a guarantee of physical memory erasure; the
implementation performs best-effort clearing and does not persist recovery material.

Existing-key onboarding accepts only an exportable 32-byte secret already controlled locally. It
cannot extract keys from NIP-07, NIP-55, hardware-backed, or other external signers.

## Security boundaries

- Central compromise alone cannot sign user events.
- One operator compromise alone cannot sign in a 2-of-3 deployment.
- Threshold operator compromise can reconstruct or sign as the user.
- OAuth-provider account compromise may enable recovery attempts and is part of the trust model.
- Operators sharing a host, Docker daemon, cloud account, administrator, or backup destination do
  not provide independent threshold security.

The pinned upstream central listens only on loopback. The entrypoint runs it on `127.0.0.1:15033`
and supervises a `socat` listener on `0.0.0.0:5033`. The pinned upstream also logs the full NIP-46
response object, so the wrapper replaces response-bearing and secret-bearing records with fixed
redaction events before they reach stdout. Both shims are regression-tested.

## Scope

This repository packages and integrates Pomegranate. It does not implement product login screens,
migrate users, deploy itself, or publish an npm package. Local upstream patches are limited to
documented deployment hardening in `vendor/pomegranate.lock.json`.
