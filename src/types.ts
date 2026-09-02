import type { Filter, NostrEvent } from "@nostr/tools";

export type PomegranateProfileFilter = Filter;

export interface PomegranateProfileConfig {
  name: string;
  filter: PomegranateProfileFilter;
}

export interface PomegranateConfig {
  centralUrl: string;
  operators: string[];
  threshold: number;
  defaultProfile?: PomegranateProfileConfig;
}

export interface ValidatedPomegranateConfig {
  centralUrl: string;
  operators: string[];
  threshold: number;
  defaultProfile: PomegranateProfileConfig;
}

export interface PomegranateSession {
  token: string;
  email: string;
  centralUrl: string;
  createdAt: Date;
  expiresAt: Date;
}

export interface PomegranateAccountOperator {
  url: string;
  pubshard: string;
}

export interface PomegranateAccount {
  email?: string;
  pubkey: string;
  operators: PomegranateAccountOperator[];
  threshold: number;
}

export interface PomegranateProfile {
  name: string;
  handlerPubkey: string;
  filter?: PomegranateProfileFilter;
  email: string;
  bunkerUrl: string;
}

export interface PreparedOperatorRegistration {
  url: string;
  pubShard: string;
  /** Sensitive transient material. Cleared by onboarding after use. */
  privateShard: string;
}

export interface PreparedRegistration {
  sessionId: string;
  pubkey: string;
  threshold: number;
  operators: PreparedOperatorRegistration[];
}

export interface OnboardingResult {
  account: PomegranateAccount;
  profile: PomegranateProfile;
  existing: boolean;
}

export interface PopupHandle {
  readonly closed?: boolean;
  close(): void;
}

export interface PopupMessage {
  data: unknown;
  origin: string;
  source: unknown;
}

export interface PopupRuntime {
  open(url: string, target: string, features: string): PopupHandle | null;
  subscribe(listener: (event: PopupMessage) => void): () => void;
}

export interface PomegranateClientDependencies {
  fetch?: typeof fetch;
  now?: () => number;
  popupRuntime?: PopupRuntime;
  randomUUID?: () => string;
}

export interface LoginOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
}

export interface OnboardingOptions {
  /** Ownership transfers to onboarding and the supplied array is zero-filled. */
  secretKey?: Uint8Array;
  accountTimeoutMs?: number;
}

export interface RegisterOperatorResult {
  url: string;
  status: number;
}

export type SignedRegistrationEvent = NostrEvent;
