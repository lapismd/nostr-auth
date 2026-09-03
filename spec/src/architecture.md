# Architecture

## NA-ARCH-001 — replaceable upstream service

The image copies an auditable Pomegranate vendor tree during a multi-stage build and performs no
source-network fetch. A lock records the immutable upstream base commit, licensing, local patch
ledger, and complete file manifest. Both Deno validation and the Docker build reject missing,
additional, or modified vendor files until the manifest is intentionally refreshed. The build also
checks the Go, templ, and Promenade pins, runs the vendored Go tests, and produces both binaries.
Runtime role selection is configuration, so central and operator images cannot drift.

The vendor tree starts from the pinned upstream source and may carry governed local changes in the
common, central, and operator packages. Each patch must be named in the lock, covered by focused Go
tests, included in the complete checksum manifest, and retain the upstream Unlicense. The current
signing-hardening patch validates FROST inputs and the final signature without changing the wire
protocol. This makes the fork boundary explicit without hiding divergence behind Docker build-time
mutations.

The operator accepts registration only from administrator-configured exact central origins. Its
acknowledgement and NIP-11 requests have fixed time and response-size bounds and never follow
redirects. OAuth cookie security follows the configured public callback scheme rather than the
container's loopback HTTP connection behind the TLS terminator.

The Go toolchain and security-sensitive `golang.org/x` modules are pinned at patched versions in
the vendored module and build image. Dependency refreshes remain explicit lock-ledger entries and
must pass a current vulnerability scan in the pinned builder.

Two documented wrappers compensate for properties of the pinned revision: central bind forwarding
and pre-stdout sensitive-response redaction. Neither changes the HTTP, NIP-46, FROST, persistence,
or authentication protocol.

## NA-ARCH-002 — topology boundaries

Development runs central and three operators on one Docker network solely for functional tests.
Production starts central and may enable one Lapis-operated operator. All other operators are
ordinary compatible URLs selected by client configuration.

The central database, each operator database, central identity secret, OAuth credentials, test
token, and user-side transient secret material have distinct owners and storage boundaries.
