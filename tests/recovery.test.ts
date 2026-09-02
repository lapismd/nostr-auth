import { assert, assertEquals, assertRejects, assertThrows } from "@std/assert";
import { generateSecretKey, getPublicKey } from "@nostr/tools";
import { PomegranateClient } from "../src/pomegranate.ts";
import type { PomegranateSession } from "../src/types.ts";

Deno.test("threshold shares reconstruct the expected secret identity", () => {
  const client = recoveryClient();
  const original = generateSecretKey();
  const expectedPubkey = getPublicKey(original);
  const registration = client.prepareRegistration(original);
  const recovered = client.recoverSecret(
    registration.operators.slice(0, 2).map((operator) => operator.privateShard),
    expectedPubkey,
  );

  assertEquals(getPublicKey(recovered), expectedPubkey);
  recovered.fill(0);
  original.fill(0);
});

Deno.test("recovery rejects too few and mismatched shares", () => {
  const client = recoveryClient();
  const original = generateSecretKey();
  const registration = client.prepareRegistration(original);

  assertThrows(
    () => client.recoverSecret([registration.operators[0].privateShard], getPublicKey(original)),
    Error,
    "at least 2",
  );

  const other = generateSecretKey();
  const otherRegistration = client.prepareRegistration(other);
  assertThrows(
    () =>
      client.recoverSecret([
        registration.operators[0].privateShard,
        otherRegistration.operators[1].privateShard,
      ], getPublicKey(original)),
    Error,
  );
  assert(original.some((byte) => byte !== 0));
  original.fill(0);
  other.fill(0);
});

Deno.test("resharding validates the replacement central before deleting the account", async () => {
  const now = Date.now();
  let deleteAttempted = false;
  const client = new PomegranateClient({
    centralUrl: "http://central:5033",
    operators: ["http://operator-1:5041", "http://operator-2:5041"],
    threshold: 2,
  }, {
    now: () => now,
    fetch: (input, init = {}) => {
      if (init.method === "DELETE") deleteAttempted = true;
      if (String(input).endsWith("/account")) {
        return Promise.resolve(Response.json({
          pubkey: "a".repeat(64),
          threshold: 2,
          operators: [],
        }));
      }
      return Promise.resolve(new Response(null, { status: 200 }));
    },
  });
  const session: PomegranateSession = {
    token: "test-token",
    email: "recovery@lapis.invalid",
    centralUrl: "http://central:5033",
    createdAt: new Date(now),
    expiresAt: new Date(now + 60_000),
  };

  await assertRejects(
    () =>
      client.recoverAndReshard(session, [], {
        centralUrl: "http://other-central:5033",
        operators: ["http://operator-3:5041", "http://operator-4:5041"],
        threshold: 2,
      }),
    Error,
    "existing central",
  );
  assertEquals(deleteAttempted, false);
});

function recoveryClient(): PomegranateClient {
  return new PomegranateClient({
    centralUrl: "http://central:5033",
    operators: ["http://operator-1:5041", "http://operator-2:5041", "http://operator-3:5041"],
    threshold: 2,
  }, { randomUUID: () => "recovery-test" });
}
