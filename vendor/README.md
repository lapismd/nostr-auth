# Vendored Pomegranate

`pomegranate/` is an auditable source snapshot of the repository and commit recorded in
`pomegranate.lock.json`. Its upstream Unlicense is preserved at `pomegranate/LICENSE`.

The image builds this local source and never fetches Pomegranate. `pomegranate.sha256` records every
file in the current vendor tree. `deno task vendor:check` rejects missing, extra, or modified files
and confirms the Go, templ, and Promenade pins.

Future local patches, including OAuth providers, must:

1. remain narrowly scoped in `pomegranate/common`, `pomegranate/central`, or
   `pomegranate/operator`;
2. add Go tests for provider identity and callback validation, plus wrapper/client tests where the
   public contract changes;
3. add a concise entry to `localPatches` in `pomegranate.lock.json`;
4. intentionally refresh the affected checksums in `pomegranate.sha256`; and
5. pass the vendored Go suite, container suite, and live protocol smoke test.

Do not add provider client secrets, access/refresh tokens, callback payloads, or test identities to
the vendor tree, lock file, tests, or logs.
