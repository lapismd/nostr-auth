# Architecture

## NA-ARCH-001 — replaceable upstream service

The image fetches an immutable Pomegranate commit during a multi-stage build, verifies the checkout,
and produces the two upstream Go binaries. Runtime role selection is configuration, so central and
operator images cannot drift.

Two documented wrappers compensate for properties of the pinned revision: central bind forwarding
and pre-stdout sensitive-response redaction. Neither changes the HTTP, NIP-46, FROST, persistence,
or authentication protocol.

## NA-ARCH-002 — topology boundaries

Development runs central and three operators on one Docker network solely for functional tests.
Production starts central and may enable one Lapis-operated operator. All other operators are
ordinary compatible URLs selected by client configuration.

The central database, each operator database, central identity secret, OAuth credentials, test
token, and user-side transient secret material have distinct owners and storage boundaries.
