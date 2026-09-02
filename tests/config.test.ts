import { assertEquals, assertThrows } from "@std/assert";
import {
  DEFAULT_LAPIS_PROFILE_FILTER,
  normalizeServiceUrl,
  toWebSocketUrl,
  validatePomegranateConfig,
} from "../src/config.ts";

Deno.test("configuration normalizes service origins and preserves arbitrary m-of-n", () => {
  const config = validatePomegranateConfig({
    centralUrl: "https://auth.example/",
    operators: ["https://one.example/", "https://two.example", "https://three.example/"],
    threshold: 1,
  }, { production: true });

  assertEquals(config.centralUrl, "https://auth.example");
  assertEquals(config.operators, [
    "https://one.example",
    "https://two.example",
    "https://three.example",
  ]);
  assertEquals(config.threshold, 1);
  assertEquals(config.defaultProfile.filter, DEFAULT_LAPIS_PROFILE_FILTER);
});

Deno.test("configuration rejects duplicate, undersized, and insecure production URLs", () => {
  assertThrows(
    () =>
      validatePomegranateConfig({
        centralUrl: "http://central:5033",
        operators: ["http://operator:5041", "http://operator:5041/"],
        threshold: 2,
      }),
    TypeError,
    "unique",
  );

  assertThrows(
    () =>
      validatePomegranateConfig({
        centralUrl: "http://central:5033",
        operators: ["http://one:5041"],
        threshold: 1,
      }),
    TypeError,
    "at least two",
  );

  assertThrows(
    () =>
      validatePomegranateConfig({
        centralUrl: "http://central:5033",
        operators: ["https://one.example", "https://two.example"],
        threshold: 2,
      }, { production: true }),
    TypeError,
    "HTTPS",
  );

  assertThrows(() => normalizeServiceUrl("https://auth.example/path"), TypeError, "without a path");
});

Deno.test("service origins convert to NIP-46 relay origins", () => {
  assertEquals(toWebSocketUrl("https://auth.example"), "wss://auth.example");
  assertEquals(toWebSocketUrl("http://central:5033"), "ws://central:5033");
});
