import { assert, assertEquals, assertRejects } from "@std/assert";
import { generateSecretKey, getPublicKey, nip44, verifyEvent } from "@nostr/tools";
import { BunkerSigner, parseBunkerInput } from "@nostr/tools/nip46";
import { clearRegistration, PomegranateClient } from "../src/pomegranate.ts";
import type { PreparedRegistration } from "../src/types.ts";

const centralUrl = requiredEnv("POMEGRANATE_CENTRAL_URL");
const operatorUrls = requiredEnv("POMEGRANATE_OPERATOR_URLS").split(",").map((value) =>
  value.trim()
);
const threshold = Number(requiredEnv("POMEGRANATE_THRESHOLD"));
const tokenFile = requiredEnv("POMEGRANATE_TEST_TOKEN_FILE");
const composeProject = requiredEnv("COMPOSE_PROJECT_NAME");
const canary = `NIP46-PLAINTEXT-${crypto.randomUUID()}`;

const client = new PomegranateClient({ centralUrl, operators: operatorUrls, threshold });
const token = (await Deno.readTextFile(tokenFile)).trim();
const session = await client.verifySessionToken(token, "google");
const userSecret = generateSecretKey();
const userSecretHex = bytesToHex(userSecret);
let registration: PreparedRegistration | undefined;
let clientSecret: Uint8Array | undefined;
let counterpartySecret: Uint8Array | undefined;
const sensitiveCanaries = [token, userSecretHex, canary];

try {
  await assertGitHubOAuthRoutes();
  registration = client.prepareRegistration(userSecret);
  sensitiveCanaries.push(...registration.operators.map((operator) => operator.privateShard));
  await client.registerAccount(session, registration, userSecret);
  await client.registerOperators(session, registration, userSecret);
  const account = await client.waitForAccount(session);
  assertEquals(account.pubkey, getPublicKey(userSecret));
  assertEquals(account.threshold, threshold);
  assertEquals(account.operators.length, operatorUrls.length);

  const profile = await client.getDefaultBunker(session);
  assert(profile.bunkerUrl.startsWith("bunker://"));
  clientSecret = generateSecretKey();
  let signer = await signerFrom(profile.bunkerUrl, clientSecret);
  assertEquals(await signer.getPublicKey(), account.pubkey);

  const signed = await signer.signEvent({
    kind: 9,
    created_at: Math.floor(Date.now() / 1000),
    tags: [],
    content: `smoke-${crypto.randomUUID()}`,
  });
  assert(verifyEvent(signed));
  assertEquals(signed.pubkey, account.pubkey);

  counterpartySecret = generateSecretKey();
  const counterpartyPubkey = getPublicKey(counterpartySecret);
  const conversationKey = nip44.getConversationKey(counterpartySecret, account.pubkey);
  const remoteCiphertext = await signer.nip44Encrypt(counterpartyPubkey, canary);
  assertEquals(nip44.decrypt(remoteCiphertext, conversationKey), canary);
  const ciphertext = nip44.encrypt(canary, conversationKey);
  assertEquals(await signer.nip44Decrypt(counterpartyPubkey, ciphertext), canary);
  await signer.close();

  const central = await containerId("central");
  const first = await containerId("operator-1");
  const second = await containerId("operator-2");
  const third = await containerId("operator-3");
  await docker("stop", first);
  await retry("sign with one operator stopped", 150_000, async () => {
    const retrySigner = await signerFrom(profile.bunkerUrl, clientSecret!);
    try {
      const event = await withTimeout(
        retrySigner.signEvent({
          kind: 9,
          created_at: Math.floor(Date.now() / 1000),
          tags: [],
          content: "one-operator-unavailable",
        }),
        20_000,
      );
      assert(verifyEvent(event));
    } finally {
      await retrySigner.close();
    }
  });

  await docker("stop", second);
  signer = await signerFrom(profile.bunkerUrl, clientSecret);
  try {
    await assertRejects(
      () =>
        withTimeout(
          signer.signEvent({
            kind: 9,
            created_at: Math.floor(Date.now() / 1000),
            tags: [],
            content: "below-threshold-must-fail",
          }),
          30_000,
        ),
    );
  } finally {
    await signer.close();
  }

  await docker("restart", central, first, second, third);
  await retry("operators restart", 60_000, async () => {
    for (const url of [centralUrl, ...operatorUrls]) {
      const response = await fetch(url);
      if (!response.ok) throw new Error(`${url} is not healthy`);
    }
  });

  const persisted = await client.getAccount(session);
  assertEquals(persisted?.pubkey, account.pubkey);
  const persistedProfile = (await client.listProfiles(session)).find((value) =>
    value.handlerPubkey === profile.handlerPubkey
  );
  assert(persistedProfile);

  await retry("signing after restart and health-cache expiry", 150_000, async () => {
    const resumedSigner = await signerFrom(profile.bunkerUrl, clientSecret!);
    try {
      assertEquals(await resumedSigner.getPublicKey(), account.pubkey);
      const resumed = await withTimeout(
        resumedSigner.signEvent({
          kind: 9,
          created_at: Math.floor(Date.now() / 1000),
          tags: [],
          content: "signing-resumed-after-restart",
        }),
        20_000,
      );
      assert(verifyEvent(resumed));
    } finally {
      await resumedSigner.close();
    }
  });

  const recovered = client.recoverSecret(
    registration.operators.slice(0, threshold).map((operator) => operator.privateShard),
    account.pubkey,
  );
  try {
    const replacement = client.prepareRegistration(recovered);
    try {
      assertEquals(replacement.pubkey, account.pubkey);
      assertEquals(replacement.threshold, threshold);
      sensitiveCanaries.push(
        ...replacement.operators.map((operator) => operator.privateShard),
      );

      await client.deleteAccount(session);
      await client.registerAccount(session, replacement, recovered);
      await client.registerOperators(session, replacement, recovered);
      const recoveredAccount = await client.waitForAccount(session);
      assertEquals(recoveredAccount.pubkey, account.pubkey);

      const recoveredProfile = await client.getDefaultBunker(session);
      const recoveredClientSecret = generateSecretKey();
      try {
        const recoveredSigner = await signerFrom(
          recoveredProfile.bunkerUrl,
          recoveredClientSecret,
        );
        try {
          assertEquals(await recoveredSigner.getPublicKey(), account.pubkey);
          const recoveredEvent = await recoveredSigner.signEvent({
            kind: 9,
            created_at: Math.floor(Date.now() / 1000),
            tags: [],
            content: "signing-after-recovery-reshard",
          });
          assert(verifyEvent(recoveredEvent));
        } finally {
          await recoveredSigner.close();
        }
      } finally {
        recoveredClientSecret.fill(0);
      }
    } finally {
      clearRegistration(replacement);
    }
  } finally {
    recovered.fill(0);
  }

  await assertLogsClean(sensitiveCanaries);
  console.log("Pomegranate 2-of-3 smoke test passed.");
} finally {
  userSecret.fill(0);
  clientSecret?.fill(0);
  counterpartySecret?.fill(0);
  if (registration) clearRegistration(registration);
  await restartStoppedOperators();
}

async function assertGitHubOAuthRoutes(): Promise<void> {
  await assertGitHubAuthorizationRedirect(
    `${centralUrl}/login/github`,
    `${centralUrl}/callback/github`,
  );

  for (const operatorUrl of operatorUrls) {
    const recoveryUrl = `${operatorUrl}/po/recover/github`;
    const recovery = await fetch(recoveryUrl, { redirect: "manual" });
    assertEquals(recovery.status, 302);
    const actionLocation = recovery.headers.get("location");
    assert(actionLocation);
    const actionUrl = new URL(actionLocation, recoveryUrl);
    assertEquals(actionUrl.href, `${operatorUrl}/po/action/github?intent=recover`);
    await assertGitHubAuthorizationRedirect(
      actionUrl.href,
      `${operatorUrl}/po/callback/github`,
    );
  }
}

async function assertGitHubAuthorizationRedirect(
  route: string,
  expectedCallback: string,
): Promise<void> {
  const response = await fetch(route, { redirect: "manual" });
  assertEquals(response.status, 302);
  const location = response.headers.get("location");
  assert(location);
  const authorization = new URL(location);
  assertEquals(authorization.origin, "https://github.com");
  assertEquals(authorization.pathname, "/login/oauth/authorize");
  assertEquals(authorization.searchParams.get("client_id"), "deterministic-test-client");
  assertEquals(authorization.searchParams.get("redirect_uri"), expectedCallback);
  assertEquals(
    authorization.searchParams.get("scope")?.split(" ").sort(),
    ["read:user", "user:email"],
  );
  assert(authorization.searchParams.get("state"));
}

async function signerFrom(bunkerUrl: string, secretKey: Uint8Array): Promise<BunkerSigner> {
  const pointer = await parseBunkerInput(bunkerUrl);
  if (!pointer) throw new Error("profile returned an invalid bunker URI");
  return BunkerSigner.fromBunker(secretKey, pointer);
}

async function containerId(service: string): Promise<string> {
  const result = await dockerOutput(
    "ps",
    "--filter",
    `label=com.docker.compose.project=${composeProject}`,
    "--filter",
    `label=com.docker.compose.service=${service}`,
    "-q",
  );
  const id = result.trim();
  if (!id) throw new Error(`no container found for ${service}`);
  return id;
}

async function restartStoppedOperators(): Promise<void> {
  for (const service of ["operator-1", "operator-2"]) {
    try {
      const result = await dockerOutput(
        "ps",
        "-a",
        "--filter",
        `label=com.docker.compose.project=${composeProject}`,
        "--filter",
        `label=com.docker.compose.service=${service}`,
        "-q",
      );
      if (result.trim()) await docker("start", result.trim());
    } catch {
      // Preserve the primary smoke failure while making a best-effort cleanup.
    }
  }
}

async function assertLogsClean(canaries: string[]): Promise<void> {
  const ids = (await dockerOutput(
    "ps",
    "-a",
    "--filter",
    `label=com.docker.compose.project=${composeProject}`,
    "-q",
  )).trim().split(/\s+/).filter(Boolean);
  for (const id of ids) {
    const logs = await dockerOutput("logs", id);
    for (const value of canaries) {
      if (value && logs.includes(value)) {
        throw new Error("sensitive canary was found in container logs");
      }
    }
  }
}

async function retry(
  label: string,
  timeoutMs: number,
  operation: () => Promise<void>,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  do {
    try {
      await operation();
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 3_000));
    }
  } while (Date.now() < deadline);
  throw new Error(`${label} did not succeed before timeout`, { cause: lastError });
}

async function withTimeout<T>(promise: Promise<T>, timeoutMs: number): Promise<T> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await Promise.race([
      promise,
      new Promise<T>((_, reject) =>
        controller.signal.addEventListener(
          "abort",
          () => reject(new Error("operation timed out")),
          {
            once: true,
          },
        )
      ),
    ]);
  } finally {
    clearTimeout(timeout);
  }
}

async function docker(...args: string[]): Promise<void> {
  const command = new Deno.Command("docker", { args, stdout: "null", stderr: "piped" });
  const result = await command.output();
  if (!result.success) throw new Error(new TextDecoder().decode(result.stderr));
}

async function dockerOutput(...args: string[]): Promise<string> {
  const command = new Deno.Command("docker", { args, stdout: "piped", stderr: "piped" });
  const result = await command.output();
  if (!result.success) throw new Error(new TextDecoder().decode(result.stderr));
  return `${new TextDecoder().decode(result.stdout)}${new TextDecoder().decode(result.stderr)}`;
}

function bytesToHex(value: Uint8Array): string {
  return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function requiredEnv(name: string): string {
  const value = Deno.env.get(name);
  if (!value) throw new Error(`${name} is required`);
  return value;
}
