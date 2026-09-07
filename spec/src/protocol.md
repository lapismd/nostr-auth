# Protocol

## NA-API-001 — upstream-compatible client

Account creation uses the Promenade trusted dealer locally. Central receives a kind-20445 event
containing threshold and `(operator URL, public shard)` tags. Each operator concurrently receives a
kind-20444 event containing only its private shard plus email, central, and the same OAuth-provider
tag selected for login. The operator token is SHA-256 over `session + ":" + exactOperatorURL`.

Pending registration must finish within 60 seconds. Central becomes operational only when all
selected operators acknowledge. Profile creation sends `{ "name": "default", "filter": ... }`; it
never sends the obsolete `restrictions` request property.

An imported key must be an exportable, locally controlled 32-byte secret. Amber, NIP-07, NIP-55,
hardware-backed, and other external signers are not key-extraction sources for onboarding.

## NA-API-002 — Google and GitHub authentication

Google or GitHub login yields a base64-encoded signed kind-20443 event. The client checks popup
source/origin, event signature, central NIP-11 `self`, kind, email tag, and the upstream 24-hour age
bound. It preserves the selected OAuth provider in the local session.

Recovery opens each operator's provider-specific `/po/recover/google` or `/po/recover/github` flow,
accepts material only from the opened popup and exact operator origin, reconstructs from a threshold
locally, verifies the account pubkey, and reshards without sending the complete key to central.
