# Operations

## NA-OPS-001 — persistence and deployment

Central uses `/var/lib/pomegranate/central.db`; each operator independently uses
`/var/lib/pomegranate/operator.db`. Production operators require encrypted storage because the
upstream database stores private shards as plaintext.

Coolify terminates TLS and routes `auth.lapis.md` to central port 5033 and the optional
`po.lapis.md` to operator port 5041. `SERVICE_URL` must be the exact public HTTPS origin. Central
requires WebSocket upgrade support. Each operator requires `TRUSTED_CENTRAL_URLS`; production uses
comma-separated exact HTTPS origins, while the development fixture explicitly permits its internal
HTTP central origin.

Backups are cold or snapshot-consistent and remain split across independent destinations. A
destination must never contain enough distinct operator archives to meet the account threshold.

Container hardening includes non-root execution, read-only root filesystems, dropped capabilities,
no-new-privileges, bounded PIDs/resources, health checks, restarts, and rotated Docker logs.
