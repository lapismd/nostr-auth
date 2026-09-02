# nostr-auth repository guidance

This repository owns the container, deployment, and TypeScript integration boundary around upstream
Pomegranate. It does not own or vendor Pomegranate's signing protocol.

Before changing behavior:

1. Read `spec/src/index.md` and the chapter governing the change.
2. Keep Pomegranate pinned to a full commit and keep base images pinned by digest.
3. Never add credentials, private shards, nsec values, OAuth/session tokens, database files, or
   decrypted NIP-46 content to source, fixtures, snapshots, logs, or command output.
4. Add the narrowest regression first, then run `deno task check:all` in the pinned test container
   and the relevant Compose smoke lane.
5. Treat wrapper changes, shared test infrastructure, and public TypeScript exports as broad changes
   requiring the full suite.

Use Jujutsu for all repository operations. Commit each verified slice, leave the final working-copy
change empty, and do not create another JJ workspace.
