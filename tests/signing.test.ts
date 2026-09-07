import { assertEquals } from "@std/assert";
import { PomegranateClient } from "../src/pomegranate.ts";
import type { PomegranateSession } from "../src/types.ts";

Deno.test("default profile produces a websocket bunker URI", async () => {
  const client = new PomegranateClient({
    centralUrl: "https://auth.example",
    operators: ["https://one.example", "https://two.example"],
    threshold: 2,
  }, {
    fetch: () =>
      Promise.resolve(Response.json([{
        handler_pubkey: "a".repeat(64),
        name: "default",
        filter: { kinds: [9, 1059] },
        email: "user@example.com",
      }])),
  }, { production: true });
  const profile = await client.getDefaultBunker(session());
  assertEquals(profile.bunkerUrl, `bunker://${"a".repeat(64)}?relay=wss%3A%2F%2Fauth.example`);
});

function session(): PomegranateSession {
  return {
    token: "opaque",
    email: "user@example.com",
    oauthProvider: "google",
    centralUrl: "https://auth.example",
    createdAt: new Date(Date.now() - 1_000),
    expiresAt: new Date(Date.now() + 60_000),
  };
}
