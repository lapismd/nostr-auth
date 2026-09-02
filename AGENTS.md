# nostr-auth repository guidance

This repository owns the container, deployment, TypeScript integration, and auditable vendor
boundary around Pomegranate. It does not independently reimplement Pomegranate's signing protocol.

Before changing behavior:

1. Read `spec/src/index.md` and the chapter governing the change.
2. Keep the Pomegranate base pinned to a full commit, keep its manifest current, and keep base
   images pinned by digest.
3. Never add credentials, private shards, nsec values, OAuth/session tokens, database files, or
   decrypted NIP-46 content to source, fixtures, snapshots, logs, or command output.
4. Add the narrowest regression first, then run `deno task check:all` in the pinned test container
   and the relevant Compose smoke lane.
5. Treat wrapper changes, shared test infrastructure, and public TypeScript exports as broad changes
   requiring the full suite.

The current `vendor/pomegranate` tree must match `vendor/pomegranate.sha256`. A deliberate local
patch requires focused Go regression coverage, a `localPatches` entry in
`vendor/pomegranate.lock.json`, and an intentional checksum refresh. Preserve the vendored Unlicense
and never import nested VCS metadata.

Use Jujutsu for all repository operations. Commit each verified slice, leave the final working-copy
change empty, and do not create another JJ workspace.
