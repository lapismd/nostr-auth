import {
  aggregateSecretKeyShards,
  hexPubShard,
  hexShard,
  keyShardFromHex,
  trustedKeyDeal,
} from "@fiatjaf/promenade-trusted-dealer";
import {
  finalizeEvent,
  generateSecretKey,
  getPublicKey,
  type NostrEvent,
  verifyEvent,
} from "@nostr/tools";
import { toWebSocketUrl, type ValidateConfigOptions, validatePomegranateConfig } from "./config.ts";
import type {
  LoginOptions,
  OnboardingOptions,
  OnboardingResult,
  PomegranateAccount,
  PomegranateClientDependencies,
  PomegranateConfig,
  PomegranateProfile,
  PomegranateProfileFilter,
  PomegranateSession,
  PopupHandle,
  PopupMessage,
  PopupRuntime,
  PreparedRegistration,
  RegisterOperatorResult,
  ValidatedPomegranateConfig,
} from "./types.ts";

const CENTRAL_TOKEN_KIND = 20443;
const OPERATOR_REGISTRATION_KIND = 20444;
const CENTRAL_REGISTRATION_KIND = 20445;
const TOKEN_LIFETIME_MS = 24 * 60 * 60 * 1000;
const DEFAULT_POPUP_TIMEOUT_MS = 5 * 60 * 1000;
const DEFAULT_ACCOUNT_TIMEOUT_MS = 55 * 1000;

interface RawProfile {
  handler_pubkey: string;
  name: string;
  filter?: PomegranateProfileFilter;
  email: string;
}

export class PomegranateHttpError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly body: string,
  ) {
    super(message);
    this.name = "PomegranateHttpError";
  }
}

export class PomegranateClient {
  readonly config: ValidatedPomegranateConfig;
  readonly #fetch: typeof fetch;
  readonly #now: () => number;
  readonly #popupRuntime?: PopupRuntime;
  readonly #randomUUID: () => string;

  constructor(
    config: PomegranateConfig,
    dependencies: PomegranateClientDependencies = {},
    validation: ValidateConfigOptions = {},
  ) {
    this.config = validatePomegranateConfig(config, validation);
    this.#fetch = dependencies.fetch ?? fetch;
    this.#now = dependencies.now ?? Date.now;
    this.#popupRuntime = dependencies.popupRuntime;
    this.#randomUUID = dependencies.randomUUID ?? (() => crypto.randomUUID());
  }

  async loginWithGoogle(options: LoginOptions = {}): Promise<PomegranateSession> {
    const token = await this.#waitForPopupValue(
      `${this.config.centralUrl}/login/google`,
      this.config.centralUrl,
      (data) => {
        if (!isRecord(data) || typeof data.token !== "string" || !data.token) return undefined;
        return data.token;
      },
      options,
    );
    return await this.verifySessionToken(token);
  }

  async verifySessionToken(token: string): Promise<PomegranateSession> {
    const event = decodeCentralToken(token);
    if (!verifyEvent(event)) throw new Error("central token signature is invalid");
    if (event.kind !== CENTRAL_TOKEN_KIND) throw new Error("central token kind is invalid");

    const response = await this.#fetch(this.config.centralUrl, {
      headers: { Accept: "application/nostr+json" },
    });
    if (!response.ok) {
      throw await httpError(response, "failed to load central NIP-11 document");
    }
    const document = await response.json();
    if (
      !isRecord(document) || typeof document.self !== "string" || document.self !== event.pubkey
    ) {
      throw new Error("central token signer does not match NIP-11 self");
    }

    const emailTag = event.tags.find((tag) => tag[0] === "email" && tag.length > 1);
    const email = emailTag?.[1]?.trim().toLowerCase();
    if (!email) throw new Error("central token has no email tag");

    const createdAtMs = event.created_at * 1000;
    const now = this.#now();
    if (!Number.isSafeInteger(createdAtMs) || createdAtMs > now + 5 * 60 * 1000) {
      throw new Error("central token creation time is invalid");
    }
    if (createdAtMs < now - TOKEN_LIFETIME_MS) throw new Error("central token has expired");

    return {
      token,
      email,
      centralUrl: this.config.centralUrl,
      createdAt: new Date(createdAtMs),
      expiresAt: new Date(createdAtMs + TOKEN_LIFETIME_MS),
    };
  }

  async getAccount(session: PomegranateSession): Promise<PomegranateAccount | null> {
    const response = await this.#request(session, "/account");
    if (response.status === 404) return null;
    if (!response.ok) throw await httpError(response, "failed to load Pomegranate account");
    return parseAccount(await response.json());
  }

  prepareRegistration(secretKey: Uint8Array): PreparedRegistration {
    assertSecretKey(secretKey);
    const secret = bytesToBigInt(secretKey);
    const deal = trustedKeyDeal(secret, this.config.threshold, this.config.operators.length);

    for (const shard of deal.shards) {
      shard.pubShard.vssCommit = deal.commits;
    }

    return {
      sessionId: this.#randomUUID(),
      pubkey: pointXHex(deal.pubkey.x),
      threshold: this.config.threshold,
      operators: this.config.operators.map((url, index) => ({
        url,
        pubShard: hexPubShard(deal.shards[index].pubShard),
        privateShard: hexShard(deal.shards[index]),
      })),
    };
  }

  async registerAccount(
    session: PomegranateSession,
    registration: PreparedRegistration,
    secretKey: Uint8Array,
  ): Promise<void> {
    this.#assertRegistration(registration, secretKey);
    const event = finalizeEvent({
      kind: CENTRAL_REGISTRATION_KIND,
      created_at: Math.floor(this.#now() / 1000),
      content: "",
      tags: [
        ["threshold", String(registration.threshold)],
        ...registration.operators.map((operator) => [
          "operator",
          operator.url,
          operator.pubShard,
        ]),
      ],
    }, secretKey);

    const response = await this.#request(session, "/register", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Pomegranate-Session": registration.sessionId,
      },
      body: JSON.stringify(event),
    });
    if (!response.ok) throw await httpError(response, "central registration failed");
  }

  async registerOperators(
    session: PomegranateSession,
    registration: PreparedRegistration,
    secretKey: Uint8Array,
  ): Promise<RegisterOperatorResult[]> {
    this.#assertRegistration(registration, secretKey);

    return await Promise.all(registration.operators.map(async (operator) => {
      const event = finalizeEvent({
        kind: OPERATOR_REGISTRATION_KIND,
        created_at: Math.floor(this.#now() / 1000),
        content: operator.privateShard,
        tags: [
          ["email", session.email],
          ["central", this.config.centralUrl],
          ["oauth", "google"],
        ],
      }, secretKey);
      const operatorToken = await sha256Hex(`${registration.sessionId}:${operator.url}`);
      const response = await this.#fetch(`${operator.url}/po/register`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Pomegranate-Operator-Token": operatorToken,
        },
        body: JSON.stringify(event),
      });
      if (!response.ok) {
        throw await httpError(response, `operator registration failed for ${operator.url}`);
      }
      return { url: operator.url, status: response.status };
    }));
  }

  async waitForAccount(
    session: PomegranateSession,
    timeoutMs = DEFAULT_ACCOUNT_TIMEOUT_MS,
  ): Promise<PomegranateAccount> {
    const deadline = this.#now() + timeoutMs;
    let lastError: unknown;
    do {
      try {
        const account = await this.getAccount(session);
        if (account) return account;
      } catch (error) {
        lastError = error;
      }
      await delay(250);
    } while (this.#now() < deadline);

    throw new Error("Pomegranate account did not become operational before registration expired", {
      cause: lastError,
    });
  }

  async deleteAccount(session: PomegranateSession): Promise<void> {
    const response = await this.#request(session, "/account", { method: "DELETE" });
    if (!response.ok) throw await httpError(response, "failed to delete central account");
  }

  async listProfiles(session: PomegranateSession): Promise<PomegranateProfile[]> {
    const response = await this.#request(session, "/profiles");
    if (!response.ok) throw await httpError(response, "failed to list profiles");
    const value = await response.json();
    if (!Array.isArray(value)) throw new TypeError("central returned an invalid profile list");
    return value.map((profile) => this.#parseProfile(profile));
  }

  async createProfile(
    session: PomegranateSession,
    name: string,
    filter: PomegranateProfileFilter,
  ): Promise<PomegranateProfile> {
    const response = await this.#request(session, "/profiles", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, filter }),
    });
    if (!response.ok) throw await httpError(response, "failed to create profile");
    return this.#parseProfile(await response.json());
  }

  async getDefaultBunker(session: PomegranateSession): Promise<PomegranateProfile> {
    const profiles = await this.listProfiles(session);
    const existing = profiles.find((profile) => profile.name === this.config.defaultProfile.name);
    if (existing) return existing;
    return await this.createProfile(
      session,
      this.config.defaultProfile.name,
      this.config.defaultProfile.filter,
    );
  }

  async onboardAccount(
    session: PomegranateSession,
    options: OnboardingOptions = {},
  ): Promise<OnboardingResult> {
    this.#assertSession(session);
    const existingAccount = await this.getAccount(session);
    if (existingAccount) {
      try {
        return {
          account: existingAccount,
          profile: await this.getDefaultBunker(session),
          existing: true,
        };
      } finally {
        options.secretKey?.fill(0);
      }
    }

    const secretKey = options.secretKey ?? generateSecretKey();
    let registration: PreparedRegistration | undefined;
    try {
      registration = this.prepareRegistration(secretKey);
      await this.registerAccount(session, registration, secretKey);
      await this.registerOperators(session, registration, secretKey);
      const account = await this.waitForAccount(session, options.accountTimeoutMs);
      if (account.pubkey !== getPublicKey(secretKey)) {
        throw new Error("registered account pubkey does not match the client key");
      }
      return {
        account,
        profile: await this.getDefaultBunker(session),
        existing: false,
      };
    } finally {
      secretKey.fill(0);
      if (registration) clearRegistration(registration);
    }
  }

  async recoverShardWithGoogle(
    operatorUrl: string,
    options: LoginOptions = {},
  ): Promise<string> {
    const normalized = this.config.operators.find((url) => url === new URL(operatorUrl).origin);
    if (!normalized) throw new Error("recovery operator is not in the configured operator set");
    return await this.#waitForPopupValue(
      `${normalized}/po/recover/google`,
      normalized,
      (data) => typeof data === "string" && /^[0-9a-f]+$/.test(data) ? data : undefined,
      options,
    );
  }

  recoverSecret(shardHexes: readonly string[], expectedPubkey: string): Uint8Array {
    if (shardHexes.length < this.config.threshold) {
      throw new Error(`recovery requires at least ${this.config.threshold} shares`);
    }
    const shards = shardHexes.map(keyShardFromHex);
    const secret = aggregateSecretKeyShards(shards);
    const recovered = bigIntTo32Bytes(secret);
    if (getPublicKey(recovered) !== expectedPubkey) {
      recovered.fill(0);
      throw new Error("recovered secret does not match the account pubkey");
    }
    return recovered;
  }

  async recoverAndReshard(
    session: PomegranateSession,
    shardHexes: readonly string[],
    replacementConfig: PomegranateConfig = this.config,
  ): Promise<OnboardingResult> {
    const account = await this.getAccount(session);
    if (!account) throw new Error("cannot recover an account that central does not know");
    const replacement = new PomegranateClient(replacementConfig, {
      fetch: this.#fetch,
      now: this.#now,
      popupRuntime: this.#popupRuntime,
      randomUUID: this.#randomUUID,
    });
    if (replacement.config.centralUrl !== this.config.centralUrl) {
      throw new Error("replacement operators must use the existing central service");
    }
    const recovered = this.recoverSecret(shardHexes, account.pubkey);
    try {
      await this.deleteAccount(session);
      return await replacement.onboardAccount(session, { secretKey: recovered });
    } finally {
      recovered.fill(0);
    }
  }

  async #request(
    session: PomegranateSession,
    path: string,
    init: RequestInit = {},
  ): Promise<Response> {
    this.#assertSession(session);
    const headers = new Headers(init.headers);
    headers.set("Authorization", `Token ${session.token}`);
    return await this.#fetch(`${this.config.centralUrl}${path}`, { ...init, headers });
  }

  #assertSession(session: PomegranateSession): void {
    if (session.centralUrl !== this.config.centralUrl) {
      throw new Error("session belongs to a different central service");
    }
    if (session.expiresAt.getTime() <= this.#now()) throw new Error("session has expired");
  }

  #assertRegistration(registration: PreparedRegistration, secretKey: Uint8Array): void {
    assertSecretKey(secretKey);
    if (registration.pubkey !== getPublicKey(secretKey)) {
      throw new Error("registration does not belong to the supplied secret key");
    }
    if (
      registration.threshold !== this.config.threshold ||
      registration.operators.length !== this.config.operators.length ||
      registration.operators.some((operator, index) =>
        operator.url !== this.config.operators[index]
      )
    ) {
      throw new Error("registration does not match the client configuration");
    }
  }

  #parseProfile(value: unknown): PomegranateProfile {
    if (
      !isRecord(value) || typeof value.handler_pubkey !== "string" ||
      typeof value.name !== "string" || typeof value.email !== "string"
    ) {
      throw new TypeError("central returned an invalid profile");
    }
    const raw = value as unknown as RawProfile;
    const relay = toWebSocketUrl(this.config.centralUrl);
    return {
      name: raw.name,
      handlerPubkey: raw.handler_pubkey,
      filter: raw.filter,
      email: raw.email,
      bunkerUrl: `bunker://${raw.handler_pubkey}?relay=${encodeURIComponent(relay)}`,
    };
  }

  async #waitForPopupValue<T>(
    url: string,
    expectedOrigin: string,
    select: (data: unknown) => T | undefined,
    options: LoginOptions,
  ): Promise<T> {
    const runtime = this.#popupRuntime ?? browserPopupRuntime();
    const popup = runtime.open(url, "pomegranate-oauth", "popup,width=520,height=720");
    if (!popup) throw new Error("OAuth popup was blocked");

    return await new Promise<T>((resolve, reject) => {
      let settled = false;
      const finish = (operation: () => void) => {
        if (settled) return;
        settled = true;
        unsubscribe();
        clearInterval(closedTimer);
        clearTimeout(timeout);
        options.signal?.removeEventListener("abort", abort);
        popup.close();
        operation();
      };
      const listener = (event: PopupMessage) => {
        if (event.source !== popup || event.origin !== expectedOrigin) return;
        const selected = select(event.data);
        if (selected !== undefined) finish(() => resolve(selected));
      };
      const unsubscribe = runtime.subscribe(listener);
      const timeout = setTimeout(
        () => finish(() => reject(new Error("OAuth popup timed out"))),
        options.timeoutMs ?? DEFAULT_POPUP_TIMEOUT_MS,
      );
      const closedTimer = setInterval(() => {
        if (popup.closed) finish(() => reject(new Error("OAuth popup closed before completion")));
      }, 200);
      const abort = () =>
        finish(() => reject(options.signal?.reason ?? new DOMException("Aborted", "AbortError")));
      options.signal?.addEventListener("abort", abort, { once: true });
      if (options.signal?.aborted) abort();
    });
  }
}

export function decodeCentralToken(token: string): NostrEvent {
  let value: unknown;
  try {
    const bytes = Uint8Array.from(atob(token), (character) => character.charCodeAt(0));
    value = JSON.parse(new TextDecoder().decode(bytes));
  } catch (error) {
    throw new Error("central token is not valid base64 JSON", { cause: error });
  }
  if (
    !isRecord(value) || typeof value.id !== "string" || typeof value.pubkey !== "string" ||
    typeof value.created_at !== "number" || typeof value.kind !== "number" ||
    !Array.isArray(value.tags) || typeof value.content !== "string" || typeof value.sig !== "string"
  ) {
    throw new TypeError("central token is not a Nostr event");
  }
  return value as unknown as NostrEvent;
}

export function clearRegistration(registration: PreparedRegistration): void {
  for (const operator of registration.operators) operator.privateShard = "";
}

function browserPopupRuntime(): PopupRuntime {
  const browser = globalThis as typeof globalThis & {
    open?: (url?: string | URL, target?: string, features?: string) => WindowProxy | null;
  };
  if (typeof browser.open !== "function") {
    throw new Error("a popup runtime is required outside a browser");
  }
  return {
    open: (url, target, features) => browser.open!(url, target, features) as PopupHandle | null,
    subscribe: (listener) => {
      const handler = (event: MessageEvent) =>
        listener({
          data: event.data,
          origin: event.origin,
          source: event.source,
        });
      browser.addEventListener("message", handler);
      return () => browser.removeEventListener("message", handler);
    },
  };
}

function parseAccount(value: unknown): PomegranateAccount {
  if (
    !isRecord(value) || typeof value.pubkey !== "string" ||
    !Number.isSafeInteger(value.threshold) || !Array.isArray(value.operators)
  ) {
    throw new TypeError("central returned an invalid account");
  }
  const operators = value.operators.map((operator) => {
    if (
      !isRecord(operator) || typeof operator.url !== "string" ||
      typeof operator.pubshard !== "string"
    ) {
      throw new TypeError("central returned an invalid account operator");
    }
    return { url: operator.url, pubshard: operator.pubshard };
  });
  return {
    email: typeof value.email === "string" ? value.email : undefined,
    pubkey: value.pubkey,
    threshold: value.threshold as number,
    operators,
  };
}

function assertSecretKey(secretKey: Uint8Array): void {
  if (secretKey.length !== 32 || secretKey.every((byte) => byte === 0)) {
    throw new TypeError("secret key must be a non-zero 32-byte array");
  }
}

function bytesToBigInt(value: Uint8Array): bigint {
  return BigInt(`0x${bytesToHex(value)}`);
}

function bigIntTo32Bytes(value: bigint): Uint8Array {
  return hexToBytes(value.toString(16).padStart(64, "0"));
}

function bytesToHex(value: Uint8Array): string {
  return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function hexToBytes(value: string): Uint8Array {
  if (!/^[0-9a-f]{64}$/.test(value)) throw new TypeError("expected 32-byte lowercase hex");
  return Uint8Array.from(value.match(/../g)!, (byte) => Number.parseInt(byte, 16));
}

function pointXHex(value: bigint): string {
  return value.toString(16).padStart(64, "0");
}

async function sha256Hex(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return bytesToHex(new Uint8Array(digest));
}

async function httpError(response: Response, message: string): Promise<PomegranateHttpError> {
  const body = (await response.text()).slice(0, 1024);
  return new PomegranateHttpError(`${message} (${response.status})`, response.status, body);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
