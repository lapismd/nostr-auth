# nostr-auth

`nostr-auth` is LapisMD's reproducible deployment and TypeScript integration boundary for upstream
Pomegranate. Pomegranate remains an independently replaceable Go service; this repository does not
reimplement or vendor its FROST/NIP-46 protocol.

## Pinned supply chain

| Component                | Pin                                                                                                  |
| ------------------------ | ---------------------------------------------------------------------------------------------------- |
| Pomegranate source       | `ca0e7a9d697a7db0fc918f9b9335bc5b58d2ac3f`                                                           |
| Promenade Go module      | `v0.4.4-0.20260511003220-ce69ab3c4a4d` (`ce69ab3c4a4d9a6079d0986a1a2fd932eb239d26`)                  |
| Promenade browser dealer | `jsr:@fiatjaf/promenade-trusted-dealer@0.4.3`                                                        |
| Nostr tools              | `jsr:@nostr/tools@2.25.1`                                                                            |
| Go                       | `1.26.2`                                                                                             |
| Builder                  | `golang:1.26.2-alpine3.23@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1`   |
| Runtime                  | `alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659`              |
| Test runtime             | `denoland/deno:alpine-2.9.6@sha256:aa665f8777136863b5b8a0445a5cdfccff8103b5f40c9a877de5276b04facb1e` |
| Docker CLI in test image | `29.5.3-r0` with Compose `5.1.4-r0`                                                                  |

The Docker build accepts only a full lowercase commit SHA and verifies that the fetched checkout
resolves to it. Upstream Pomegranate is Unlicense-licensed; the deployment wrapper and TypeScript
integration in this repository are MIT.

## Local development

The local topology is a functional test fixture, not a safe production trust boundary:

```text
central ─┬─ operator-1
         ├─ operator-2       threshold: 2
         └─ operator-3
```

Start it with only Docker installed:

```sh
docker compose -f compose.yml -f compose.dev.yml up --build
```

Run the source checks and full protocol smoke test in the pinned test image:

```sh
docker compose -f compose.yml -f compose.dev.yml run --rm test-runner task check:all
docker compose -f compose.yml -f compose.dev.yml --profile test run --rm test-runner task smoke
```

The smoke container mounts `/var/run/docker.sock` so it can stop and restart operators. That mount
is equivalent to host-level control and is strictly a local test facility. Never enable
`test-runner` in production.

Development creates a central signing key and a 24-hour kind-20443 test token inside separate named
volumes. The test runner can read the token but cannot read the central key. Dummy Google values
only make the operator registration route available; they cannot complete OAuth.

## Coolify production deployment

Deploy `compose.yml` with `compose.prod.yml`. The base deployment starts only central. Enable the
`lapis-operator` profile when Lapis will also operate `po.lapis.md`.

```text
auth.lapis.md  -> central:5033
po.lapis.md    -> lapis-operator:5041 (optional)
```

Configure Coolify/Traefik to terminate TLS, redirect HTTP to HTTPS, preserve forwarded host/proto,
and allow WebSocket upgrades on central. Do not publish the container ports on the host.
`SERVICE_URL` is authoritative for callbacks and must exactly match the public origin.

Google OAuth redirect URIs are:

```text
https://auth.lapis.md/callback/google
https://po.lapis.md/po/callback/google
```

Give central and the optional operator separate Coolify environment/secret sets. The image accepts
either the exact upstream variable or its `_FILE` counterpart for secret values, for example
`SECRET_KEY_FILE` and `GOOGLE_CLIENT_SECRET_FILE`; setting both forms is an error. When deploying
these Compose files, configure the `CENTRAL_*` and `OPERATOR_*` inputs shown in `.env.example`;
Compose maps them to the exact upstream names inside only the relevant service. Coolify may instead
inject the exact names directly when it deploys each service independently.

Recommended client configuration:

```json
{
  "centralUrl": "https://auth.lapis.md",
  "operators": [
    "https://po.lapis.md",
    "https://operator-a.example",
    "https://operator-b.example"
  ],
  "threshold": 2
}
```

The client accepts any upstream-valid `m-of-n` arrangement with at least two operators. It never
requires the Lapis-operated operator.

### Coolify acceptance

After staging deployment, verify manually:

1. Both public roots pass health checks over HTTPS and central accepts a WebSocket connection.
2. Google login returns through `https://auth.lapis.md/callback/google` and account onboarding
   completes.
3. The default profile returns a bunker URI and can sign an allowed event.
4. Recovery at each selected operator returns through `/po/callback/google` only after the user
   confirms sharing the shard.
5. Two recovered shares reconstruct the expected pubkey; resharing to the replacement operator set
   restores NIP-46 signing.
6. Container logs contain no token, shard, nsec, recovery payload, decrypted content, or
   signed-event content.

## Persistence, backup, and recovery

Central stores account/operator public-share metadata, profile restrictions, email associations, and
NIP-46 handler keys in `/var/lib/pomegranate/central.db`. A central backup alone cannot sign for a
user.

Each operator stores its user's private FROST shard, user pubkey, central URL and pubkey, and
Google/email association in its own `/var/lib/pomegranate/operator.db`. Upstream bbolt storage does
**not** encrypt these records. Production operator volumes therefore require encrypted host/block
storage.

Create cold backups by stopping or quiescing one service at a time, copying its named volume, and
restarting it before moving to another trust boundary. Never place a threshold number of operator
backups in one ordinary destination. Run:

```sh
docker compose run --rm test-runner run --allow-read scripts/backup-check.ts backup-manifest.json
```

The manifest records `role`, `operatorId`, archive `path`, and independent `destination`; the
checker verifies readable non-empty archives and rejects a destination containing `threshold`
distinct operator backups.

Recovery is intentionally client-side. The browser authenticates separately to threshold operators,
decodes the returned shares, reconstructs and verifies the pubkey, reshards to replacements, and
clears temporary buffers. JavaScript strings, `bigint` values, and garbage collection prevent a
guarantee of physical memory erasure; the implementation performs best-effort clearing and does not
persist recovery material.

Existing-key onboarding accepts only an exportable 32-byte secret that the caller already controls
locally. It cannot extract private keys from Amber, NIP-07 or NIP-55 providers, hardware signers, or
any other external signer, and this repository does not claim that capability.

## Security and upstream wrapper deviations

- Central compromise alone cannot sign user events.
- One operator compromise alone cannot sign in a 2-of-3 deployment.
- Threshold operator compromise can reconstruct/sign as the user.
- Google account or provider compromise may enable recovery attempts and is part of Pomegranate's
  trust model.
- Operators that share a host, Docker daemon, cloud account, administrator, or backup destination do
  not provide independent threshold security.

The pinned upstream central listens only on loopback. The entrypoint therefore runs it on
`127.0.0.1:15033` and supervises a `socat` listener on `0.0.0.0:5033`. The pinned upstream also logs
the full NIP-46 response object. Before a line reaches stdout, the wrapper replaces response-bearing
and secret-bearing records with fixed redaction events. These shims are regression-tested and can be
removed when a later pinned upstream revision supplies equivalent behavior.

## Scope

This repository does not alter Lapis login screens, migrate current users, deploy to Coolify,
publish a package, or patch Pomegranate. A later Lapis change can expose “Continue with Google” and
pass the resulting bunker URI to the existing signer abstraction.
