import type {
  PomegranateConfig,
  PomegranateProfileConfig,
  ValidatedPomegranateConfig,
} from "./types.ts";

export const DEFAULT_PROFILE_FILTER = Object.freeze({
  kinds: Object.freeze([
    0,
    5,
    7,
    9,
    11,
    13,
    1059,
    1111,
    9000,
    9001,
    9002,
    9005,
    9007,
    9008,
    9021,
    9022,
    22242,
    30030,
    30078,
    30315,
  ]) as unknown as number[],
});

export const DEFAULT_PROFILE: PomegranateProfileConfig = Object.freeze({
  name: "default",
  filter: DEFAULT_PROFILE_FILTER,
});

export interface ValidateConfigOptions {
  production?: boolean;
}

export function validatePomegranateConfig(
  input: PomegranateConfig,
  options: ValidateConfigOptions = {},
): ValidatedPomegranateConfig {
  const centralUrl = normalizeServiceUrl(input.centralUrl, options);
  const operators = input.operators.map((value) => normalizeServiceUrl(value, options));
  const uniqueOperators = new Set(operators);

  if (operators.length < 2) {
    throw new TypeError("Pomegranate requires at least two operators");
  }
  if (uniqueOperators.size !== operators.length) {
    throw new TypeError("operator URLs must be unique after normalization");
  }
  if (!Number.isSafeInteger(input.threshold) || input.threshold < 1) {
    throw new TypeError("threshold must be a positive integer");
  }
  if (input.threshold > operators.length) {
    throw new TypeError("threshold cannot exceed the operator count");
  }

  const profile = input.defaultProfile ?? DEFAULT_PROFILE;
  const name = profile.name.trim();
  if (!name) {
    throw new TypeError("default profile name is required");
  }
  if (!profile.filter || typeof profile.filter !== "object") {
    throw new TypeError("default profile filter is required");
  }

  return {
    centralUrl,
    operators,
    threshold: input.threshold,
    defaultProfile: {
      name,
      filter: structuredClone(profile.filter),
    },
  };
}

export function normalizeServiceUrl(
  value: string,
  options: ValidateConfigOptions = {},
): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new TypeError(`invalid service URL: ${value}`);
  }

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new TypeError(`service URL must use HTTP or HTTPS: ${value}`);
  }
  if (options.production && url.protocol !== "https:") {
    throw new TypeError(`production service URL must use HTTPS: ${value}`);
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new TypeError(`service URL cannot contain credentials, query, or fragment: ${value}`);
  }
  if (url.pathname !== "/") {
    throw new TypeError(`service URL must be an origin without a path: ${value}`);
  }

  return url.origin;
}

export function toWebSocketUrl(serviceUrl: string): string {
  const url = new URL(serviceUrl);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.origin;
}
