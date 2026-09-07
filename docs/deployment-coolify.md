# Deploying with Coolify

This runbook deploys the Pomegranate central service and, optionally, one operator with Coolify and
Traefik. Example origins are placeholders; replace them with domains you control.

## Topology

Deploy `compose.yml` with `compose.prod.yml`. The base configuration starts only `central`. Enable
the `operator` profile to start the optional colocated operator.

```text
auth.example.com      -> central:5033
operator.example.com  -> operator:5041 (optional)
```

Threshold security depends on independent operators. An operator colocated with central shares its
host, Docker daemon, cloud account, and administrator trust boundary, so it must not be counted as
independent from central. Place the remaining operators under separate administrative control.

## Routing and TLS

Configure Coolify and Traefik to:

- terminate TLS and redirect HTTP to HTTPS;
- preserve the forwarded host and protocol;
- allow WebSocket upgrades on central;
- route central to container port 5033;
- route the optional operator to container port 5041; and
- avoid publishing either container port directly on the host.

`SERVICE_URL` is authoritative for OAuth callbacks and must exactly match each public HTTPS origin.
OAuth state cookies derive their `Secure` attribute from that public callback URL, so TLS
termination at the proxy does not weaken them.

## Environment and secrets

Start from `.env.example`, but configure production values through separate Coolify environment and
secret sets rather than committing an `.env` file.

Central inputs:

```text
CENTRAL_SERVICE_URL=https://auth.example.com
CENTRAL_SECRET_KEY_FILE=/run/secrets/central-key
CENTRAL_GOOGLE_CLIENT_ID=...
CENTRAL_GOOGLE_CLIENT_SECRET_FILE=/run/secrets/central-google-secret
CENTRAL_GITHUB_CLIENT_ID=...
CENTRAL_GITHUB_CLIENT_SECRET_FILE=/run/secrets/central-github-secret
```

Optional operator inputs:

```text
OPERATOR_SERVICE_URL=https://operator.example.com
OPERATOR_TRUSTED_CENTRAL_URLS=https://auth.example.com
OPERATOR_GOOGLE_CLIENT_ID=...
OPERATOR_GOOGLE_CLIENT_SECRET_FILE=/run/secrets/operator-google-secret
OPERATOR_GITHUB_CLIENT_ID=...
OPERATOR_GITHUB_CLIENT_SECRET_FILE=/run/secrets/operator-github-secret
```

The image accepts either an upstream secret variable or its `_FILE` counterpart, including
`SECRET_KEY`, `GOOGLE_CLIENT_SECRET`, and `GITHUB_CLIENT_SECRET`. Setting both forms for one secret
is an error. Central and operator credentials remain isolated even if they refer to the same
provider application.

Set `OPERATOR_TRUSTED_CENTRAL_URLS` to the comma-separated, exact HTTPS origins allowed to register
shards. The operator rejects every other central before making a request. Allowed requests use a
bounded five-second client that does not follow redirects or accept oversized NIP-11 responses.

## OAuth applications

Configure exact callback URLs and disable wildcard callback matching.

Google callbacks:

```text
https://auth.example.com/callback/google
https://operator.example.com/po/callback/google
```

GitHub callbacks:

```text
https://auth.example.com/callback/github
https://operator.example.com/po/callback/github
```

The GitHub flow requests `read:user` and `user:email`. The latter provides read-only access to
private email addresses when needed. Pomegranate accepts a GitHub identity only when it can obtain a
usable verified email address. Never log or persist the provider access token outside Pomegranate's
OAuth request lifecycle.

## Client topology

A client needs the central origin, exact operator origins, and threshold:

```json
{
  "centralUrl": "https://auth.example.com",
  "operators": [
    "https://operator.example.com",
    "https://operator-a.example.net",
    "https://operator-b.example.net"
  ],
  "threshold": 2
}
```

The client accepts any upstream-valid `m-of-n` arrangement with at least two configured operators.
It never requires the optional colocated operator.

## Storage and backup

Mount central at `/var/lib/pomegranate/central.db` and every operator at its own
`/var/lib/pomegranate/operator.db`. Operator databases contain plaintext private FROST shards, so
their host or block storage must be encrypted.

Backups must be cold or snapshot-consistent and split across independent destinations. Never place a
threshold number of operator archives in one destination. See the repository README for the backup
manifest checker.

## Staging acceptance

Before promoting the deployment:

1. Verify both configured public roots pass health checks over HTTPS and central accepts a WebSocket
   connection.
2. Complete Google and GitHub login through their exact central callbacks and finish account
   onboarding.
3. Confirm the default profile returns a bunker URI and can sign an allowed event.
4. Recover from each selected operator through the matching Google and GitHub provider flow, only
   after explicit user confirmation.
5. Reconstruct the expected pubkey from a threshold of recovered shares, reshard to a replacement
   operator set, and verify NIP-46 signing resumes.
6. Confirm container logs contain no token, shard, nsec, OAuth access token, recovery payload,
   decrypted content, or signed-event content.

Real provider login and recovery remain manual acceptance gates because they require interactive
authorization and explicit recovery confirmation. Automated tests exercise the production routes,
state handling, registration contracts, and signed central-token validation without adding an
authentication bypass.
