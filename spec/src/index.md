# nostr-auth specification

This specification governs the reproducible Pomegranate deployment and its typed client boundary.

- **NA-ARCH-001:** The repository must build Pomegranate central and operator binaries from an
  integrity-checked vendor tree based on a pinned upstream commit, without independently
  reimplementing the signing protocol.
- **NA-ARCH-002:** Development provides a single-network 2-of-3 topology; production provides
  central, one optional colocated operator, and configurable external operator URLs.
- **NA-SEC-001:** Central never receives the complete user private key or a private FROST share.
- **NA-SEC-002:** Secrets and decrypted NIP-46 data never reach committed files, images, test
  artifacts, or container logs.
- **NA-API-001:** The TypeScript API mirrors upstream authentication, registration, profile, NIP-46,
  and recovery contracts.
- **NA-API-002:** Google and GitHub browser authentication are provider-bound from central login
  through operator registration and provider-specific shard recovery.
- **NA-OPS-001:** Every bbolt path is mounted on a role-specific persistent volume and is covered by
  backup guidance.
- **NA-TEST-001:** Automated acceptance proves onboarding, 2-of-3 signing, one-operator
  availability, two-operator failure, restart persistence, and protocol-level recovery.
- **NA-TEST-002:** Pull requests and main-branch changes run the complete repository checks and live
  2-of-3 Compose acceptance before they are accepted or published.
- **NA-OPS-002:** Trusted main, version-tag, and manual builds publish one public `linux/amd64` and
  `linux/arm64` image to GHCR with an SBOM, build provenance, immutable workflow dependencies, and
  least-privilege credentials.

See `architecture.md`, `protocol.md`, `operations.md`, and `verification.md`.
