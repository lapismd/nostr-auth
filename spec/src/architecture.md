# Architecture

## NA-ARCH-001 — replaceable upstream service

The image copies an auditable Pomegranate vendor tree during a multi-stage build and performs no
source-network fetch. A lock records the immutable upstream base commit, licensing, local patch
ledger, and complete file manifest. Both Deno validation and the Docker build reject missing,
additional, or modified vendor files until the manifest is intentionally refreshed. The build also
checks the Go, templ, and Promenade pins, runs the vendored Go tests, and produces both binaries.
Runtime role selection is configuration, so central and operator images cannot drift.

The initial vendor tree is byte-for-byte the pinned upstream source. Future OAuth-provider changes
may patch the common, central, and operator packages locally, but each patch must be named in the
lock, covered by focused Go tests, and retain the upstream Unlicense. This makes the fork boundary
explicit without hiding divergence behind Docker build-time mutations.

Two documented wrappers compensate for properties of the pinned revision: central bind forwarding
and pre-stdout sensitive-response redaction. Neither changes the HTTP, NIP-46, FROST, persistence,
or authentication protocol.

## NA-ARCH-002 — topology boundaries

Development runs central and three operators on one Docker network solely for functional tests.
Production starts central and may enable one Lapis-operated operator. All other operators are
ordinary compatible URLs selected by client configuration.

The central database, each operator database, central identity secret, OAuth credentials, test
token, and user-side transient secret material have distinct owners and storage boundaries.
