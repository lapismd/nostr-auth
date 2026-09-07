import { assert, assertEquals, assertFalse, assertRejects } from "@std/assert";
import {
  finalizeEvent,
  generateSecretKey,
  getPublicKey,
  type NostrEvent,
  verifyEvent,
} from "@nostr/tools";
import { PomegranateClient } from "../src/pomegranate.ts";
import type { PomegranateSession, PopupHandle, PopupMessage, PopupRuntime } from "../src/types.ts";

const NOW = Date.UTC(2026, 8, 3, 12, 0, 0);

class FakePopup implements PopupHandle {
  closed = false;
  close(): void {
    this.closed = true;
  }
}

class FakePopupRuntime implements PopupRuntime {
  popup = new FakePopup();
  listener?: (event: PopupMessage) => void;
  openedUrl?: string;

  open(url: string): PopupHandle {
    this.openedUrl = url;
    return this.popup;
  }

  subscribe(listener: (event: PopupMessage) => void): () => void {
    this.listener = listener;
    return () => {
      this.listener = undefined;
    };
  }

  emit(origin: string, data: unknown, source: unknown = this.popup): void {
    this.listener?.({ origin, data, source });
  }
}

Deno.test("Google login accepts only the opened popup at the central origin", async () => {
  const centralSecret = generateSecretKey();
  const tokenEvent = finalizeEvent({
    kind: 20443,
    created_at: Math.floor(NOW / 1000),
    content: "",
    tags: [["email", "USER@Example.COM"]],
  }, centralSecret);
  const token = btoa(JSON.stringify(tokenEvent));
  const runtime = new FakePopupRuntime();
  const client = clientWith({
    popupRuntime: runtime,
    fetch: () => Promise.resolve(Response.json({ self: getPublicKey(centralSecret) })),
  });

  const login = client.loginWithGoogle({ timeoutMs: 1_000 });
  await Promise.resolve();
  runtime.emit("https://evil.example", { token });
  runtime.emit("http://central:5033", { token }, {});
  assertFalse(runtime.popup.closed);
  runtime.emit("http://central:5033", { token });

  const session = await login;
  assertEquals(runtime.openedUrl, "http://central:5033/login/google");
  assertEquals(session.email, "user@example.com");
  assertEquals(session.oauthProvider, "google");
  assertEquals(session.expiresAt.getTime(), NOW + 24 * 60 * 60 * 1000);
  assert(runtime.popup.closed);
  centralSecret.fill(0);
});

Deno.test("GitHub login preserves its provider for operator recovery", async () => {
  const centralSecret = generateSecretKey();
  const tokenEvent = finalizeEvent({
    kind: 20443,
    created_at: Math.floor(NOW / 1000),
    content: "",
    tags: [["email", "github-user@example.com"]],
  }, centralSecret);
  const runtime = new FakePopupRuntime();
  const client = clientWith({
    popupRuntime: runtime,
    fetch: () => Promise.resolve(Response.json({ self: getPublicKey(centralSecret) })),
  });

  const login = client.loginWithGitHub({ timeoutMs: 1_000 });
  await Promise.resolve();
  runtime.emit("http://central:5033", { token: btoa(JSON.stringify(tokenEvent)) });

  const session = await login;
  assertEquals(runtime.openedUrl, "http://central:5033/login/github");
  assertEquals(session.email, "github-user@example.com");
  assertEquals(session.oauthProvider, "github");
  centralSecret.fill(0);
});

Deno.test("GitHub recovery uses the selected operator's provider route", async () => {
  const runtime = new FakePopupRuntime();
  const client = clientWith({ popupRuntime: runtime });

  const recovery = client.recoverShardWithGitHub("http://operator-1:5041", {
    timeoutMs: 1_000,
  });
  await Promise.resolve();
  assertEquals(runtime.openedUrl, "http://operator-1:5041/po/recover/github");
  runtime.emit("http://operator-1:5041", "01ab");
  assertEquals(await recovery, "01ab");
});

Deno.test("Google login rejects a token not signed by central NIP-11 self", async () => {
  const signer = generateSecretKey();
  const other = generateSecretKey();
  const event = finalizeEvent({
    kind: 20443,
    created_at: Math.floor(NOW / 1000),
    content: "",
    tags: [["email", "user@example.com"]],
  }, signer);
  const runtime = new FakePopupRuntime();
  const client = clientWith({
    popupRuntime: runtime,
    fetch: () => Promise.resolve(Response.json({ self: getPublicKey(other) })),
  });
  const login = client.loginWithGoogle({ timeoutMs: 1_000 });
  await Promise.resolve();
  runtime.emit("http://central:5033", { token: btoa(JSON.stringify(event)) });
  await assertRejects(() => login, Error, "does not match");
  signer.fill(0);
  other.fill(0);
});

Deno.test("Google login reports blocked, closed, and timed-out popups", async () => {
  const blocked: PopupRuntime = {
    open: () => null,
    subscribe: () => () => undefined,
  };
  await assertRejects(
    () => clientWith({ popupRuntime: blocked }).loginWithGoogle(),
    Error,
    "blocked",
  );

  const closed = new FakePopupRuntime();
  closed.popup.closed = true;
  await assertRejects(
    () => clientWith({ popupRuntime: closed }).loginWithGoogle({ timeoutMs: 1_000 }),
    Error,
    "closed",
  );

  const timedOut = new FakePopupRuntime();
  await assertRejects(
    () => clientWith({ popupRuntime: timedOut }).loginWithGoogle({ timeoutMs: 5 }),
    Error,
    "timed out",
  );
});

Deno.test("central tokens older than the upstream lifetime are rejected", async () => {
  const centralSecret = generateSecretKey();
  const event = finalizeEvent({
    kind: 20443,
    created_at: Math.floor((NOW - 25 * 60 * 60 * 1000) / 1000),
    content: "",
    tags: [["email", "user@example.com"]],
  }, centralSecret);
  const client = clientWith({
    fetch: () => Promise.resolve(Response.json({ self: getPublicKey(centralSecret) })),
  });

  await assertRejects(
    () => client.verifySessionToken(btoa(JSON.stringify(event)), "google"),
    Error,
    "expired",
  );
  centralSecret.fill(0);
});

Deno.test("registration uses exact upstream event and header contracts", async () => {
  const requests: Array<{ url: string; init: RequestInit }> = [];
  let activeOperators = 0;
  let maximumActiveOperators = 0;
  const client = clientWith({
    randomUUID: () => "registration-session",
    fetch: async (input, init = {}) => {
      const url = String(input);
      requests.push({ url, init });
      if (url.includes("/po/register")) {
        activeOperators++;
        maximumActiveOperators = Math.max(maximumActiveOperators, activeOperators);
        await new Promise((resolve) => setTimeout(resolve, 5));
        activeOperators--;
      }
      return new Response(null, { status: 200 });
    },
  });
  const secret = generateSecretKey();
  const session = fakeSession();
  const registration = client.prepareRegistration(secret);

  await client.registerAccount(session, registration, secret);
  await client.registerOperators(session, registration, secret);

  assertEquals(maximumActiveOperators, 3);
  const central = requests[0];
  const centralEvent = JSON.parse(String(central.init.body)) as NostrEvent;
  assertEquals(central.url, "http://central:5033/register");
  assertEquals(new Headers(central.init.headers).get("Authorization"), "Token test-token");
  assertEquals(
    new Headers(central.init.headers).get("X-Pomegranate-Session"),
    "registration-session",
  );
  assertEquals(centralEvent.kind, 20445);
  assert(verifyEvent(centralEvent));
  assertEquals(centralEvent.tags[0], ["threshold", "2"]);
  assertEquals(centralEvent.tags.filter((tag) => tag[0] === "operator").length, 3);
  for (const material of registration.operators) {
    assertFalse(String(central.init.body).includes(material.privateShard));
  }

  for (const request of requests.slice(1)) {
    const event = JSON.parse(String(request.init.body)) as NostrEvent;
    const headers = new Headers(request.init.headers);
    assertEquals(event.kind, 20444);
    assert(verifyEvent(event));
    assertEquals(event.pubkey, getPublicKey(secret));
    assertEquals(event.tags, [
      ["email", "smoke-test@example.invalid"],
      ["central", "http://central:5033"],
      ["oauth", "google"],
    ]);
    const operator = registration.operators.find((candidate) =>
      `${candidate.url}/po/register` === request.url
    )!;
    assertEquals(event.content, operator.privateShard);
    assertEquals(
      headers.get("X-Pomegranate-Operator-Token"),
      await digestHex(`registration-session:${operator.url}`),
    );
  }
  secret.fill(0);
});

Deno.test("GitHub sessions register GitHub as the operator recovery provider", async () => {
  const events: NostrEvent[] = [];
  const client = clientWith({
    fetch: (_input, init = {}) => {
      events.push(JSON.parse(String(init.body)) as NostrEvent);
      return Promise.resolve(new Response(null, { status: 200 }));
    },
  });
  const secret = generateSecretKey();
  const registration = client.prepareRegistration(secret);

  await client.registerOperators(fakeSession("github"), registration, secret);

  assertEquals(events.length, 3);
  for (const event of events) {
    assertEquals(event.tags.find((tag) => tag[0] === "oauth"), ["oauth", "github"]);
  }
  secret.fill(0);
});

Deno.test("onboarding confirms account, creates filter profile, and clears caller key", async () => {
  const secret = generateSecretKey();
  const pubkey = getPublicKey(secret);
  let accountReads = 0;
  let profileRequest: Record<string, unknown> | undefined;
  const client = clientWith({
    fetch: async (input, init = {}) => {
      await Promise.resolve();
      const url = String(input);
      if (url.endsWith("/account") && (!init.method || init.method === "GET")) {
        accountReads++;
        if (accountReads === 1) return Response.json({ error: "missing" }, { status: 404 });
        return Response.json({
          pubkey,
          threshold: 2,
          operators: [
            { url: "http://operator-1:5041", pubshard: "01" },
            { url: "http://operator-2:5041", pubshard: "02" },
            { url: "http://operator-3:5041", pubshard: "03" },
          ],
        });
      }
      if (url.endsWith("/profiles") && (!init.method || init.method === "GET")) {
        return Response.json([]);
      }
      if (url.endsWith("/profiles") && init.method === "POST") {
        profileRequest = JSON.parse(String(init.body));
        return Response.json({
          handler_pubkey: "f".repeat(64),
          name: "default",
          filter: (profileRequest as { filter: unknown }).filter,
          email: "smoke-test@example.invalid",
        }, { status: 201 });
      }
      return new Response(null, { status: 200 });
    },
  });

  const result = await client.onboardAccount(fakeSession(), {
    secretKey: secret,
    accountTimeoutMs: 500,
  });
  assertFalse(result.existing);
  assertEquals(result.account.pubkey, pubkey);
  assertEquals(
    result.profile.bunkerUrl,
    `bunker://${"f".repeat(64)}?relay=ws%3A%2F%2Fcentral%3A5033`,
  );
  assertEquals(Object.keys(profileRequest!).sort(), ["filter", "name"]);
  assertEquals(profileRequest!.name, "default");
  assert(secret.every((byte) => byte === 0));
});

function clientWith(
  dependencies: ConstructorParameters<typeof PomegranateClient>[1],
): PomegranateClient {
  return new PomegranateClient({
    centralUrl: "http://central:5033",
    operators: [
      "http://operator-1:5041",
      "http://operator-2:5041",
      "http://operator-3:5041",
    ],
    threshold: 2,
  }, { now: () => NOW, ...dependencies });
}

function fakeSession(oauthProvider: "google" | "github" = "google"): PomegranateSession {
  return {
    token: "test-token",
    email: "smoke-test@example.invalid",
    oauthProvider,
    centralUrl: "http://central:5033",
    createdAt: new Date(NOW),
    expiresAt: new Date(NOW + 60_000),
  };
}

async function digestHex(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}
