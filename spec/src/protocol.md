# Protocol

## NA-API-001 — upstream-compatible client

Google login yields a base64-encoded signed kind-20443 event. The client checks popup source/origin,
event signature, central NIP-11 `self`, kind, email tag, and the upstream 24-hour age bound.

Account creation uses the Promenade trusted dealer locally. Central receives a kind-20445 event
containing threshold and `(operator URL, public shard)` tags. Each operator concurrently receives a
kind-20444 event containing only its private shard plus email, central, and OAuth-provider tags. The
operator token is SHA-256 over `session + ":" + exactOperatorURL`.

Pending registration must finish within 60 seconds. Central becomes operational only when all
selected operators acknowledge. Profile creation sends `{ "name": "default", "filter": ... }`; it
never sends the obsolete `restrictions` request property.

Recovery opens each operator's `/po/recover/google` flow, accepts material only from the opened
popup and exact operator origin, reconstructs from a threshold locally, verifies the account pubkey,
and reshards without sending the complete key to central.

An imported key must be an exportable, locally controlled 32-byte secret. Amber, NIP-07, NIP-55,
hardware-backed, and other external signers are not key-extraction sources for onboarding.
