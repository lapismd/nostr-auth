import { validatePomegranateConfig } from "../src/config.ts";
import type { PomegranateConfig, ValidatedPomegranateConfig } from "../src/types.ts";

export function verifyConfig(value: unknown, production: boolean): ValidatedPomegranateConfig {
  if (typeof value !== "object" || value === null) throw new TypeError("config must be an object");
  return validatePomegranateConfig(value as PomegranateConfig, { production });
}

if (import.meta.main) {
  const production = Deno.args.includes("--production");
  const path = Deno.args.find((argument) => !argument.startsWith("--"));
  if (!path) throw new Error("usage: verify-config.ts [--production] <config.json>");
  const config = verifyConfig(JSON.parse(await Deno.readTextFile(path)), production);
  console.log(JSON.stringify(
    {
      centralUrl: config.centralUrl,
      operatorCount: config.operators.length,
      threshold: config.threshold,
      profile: config.defaultProfile.name,
      production,
    },
    null,
    2,
  ));
}
