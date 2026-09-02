const specDirectory = new URL("../spec/src/", import.meta.url);
const requiredFiles = [
  "index.md",
  "architecture.md",
  "protocol.md",
  "operations.md",
  "verification.md",
];
const requirementPattern = /NA-(?:ARCH|SEC|API|OPS|TEST)-\d{3}/g;

const contents = new Map<string, string>();
for (const file of requiredFiles) {
  contents.set(file, await Deno.readTextFile(new URL(file, specDirectory)));
}

const indexRequirements = new Set(contents.get("index.md")!.match(requirementPattern) ?? []);
if (indexRequirements.size === 0) throw new Error("spec index declares no requirements");

const verification = contents.get("verification.md")!;
for (const requirement of indexRequirements) {
  if (!verification.includes(requirement)) {
    throw new Error(`verification map is missing ${requirement}`);
  }
  const occurrences = [...contents.values()].filter((content) =>
    content.includes(requirement)
  ).length;
  if (occurrences < 2) throw new Error(`${requirement} has no governing chapter`);
}

console.log(`spec check passed: ${indexRequirements.size} governed requirements`);
