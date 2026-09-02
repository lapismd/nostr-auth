const EXPECTED_REPOSITORY =
  "https://basspistol.org/npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6/pomegranate.git";
const EXPECTED_COMMIT = "ca0e7a9d697a7db0fc918f9b9335bc5b58d2ac3f";
const VENDOR_ROOT = new URL("../vendor/pomegranate/", import.meta.url);
const LOCK_URL = new URL("../vendor/pomegranate.lock.json", import.meta.url);
const MANIFEST_URL = new URL("../vendor/pomegranate.sha256", import.meta.url);

interface VendorLock {
  repository: string;
  commit: string;
  license: string;
  manifest: string;
  localPatches: string[];
}

export async function validateVendorTree(): Promise<void> {
  const lock = JSON.parse(await Deno.readTextFile(LOCK_URL)) as VendorLock;
  if (lock.repository !== EXPECTED_REPOSITORY) throw new Error("vendor repository changed");
  if (lock.commit !== EXPECTED_COMMIT || !/^[0-9a-f]{40}$/.test(lock.commit)) {
    throw new Error("vendor commit is not the approved full SHA");
  }
  if (lock.license !== "Unlicense" || lock.manifest !== "pomegranate.sha256") {
    throw new Error("vendor license or manifest metadata is invalid");
  }
  if (!Array.isArray(lock.localPatches) || !lock.localPatches.every((value) => value.trim())) {
    throw new Error("localPatches must contain non-empty descriptions");
  }

  const expected = parseManifest(await Deno.readTextFile(MANIFEST_URL));
  const actualFiles = await listFiles(VENDOR_ROOT);
  if (expected.size !== actualFiles.length) {
    throw new Error("vendor manifest does not cover the complete source tree");
  }

  for (const path of actualFiles) {
    const expectedHash = expected.get(path);
    if (!expectedHash) throw new Error(`vendor manifest is missing ${path}`);
    const actualHash = await sha256Hex(await Deno.readFile(new URL(path, VENDOR_ROOT)));
    if (actualHash !== expectedHash) throw new Error(`vendor checksum mismatch: ${path}`);
  }

  const goMod = await Deno.readTextFile(new URL("go.mod", VENDOR_ROOT));
  for (
    const pin of [
      "go 1.26.2",
      "fiatjaf.com/promenade v0.4.4-0.20260511003220-ce69ab3c4a4d",
      "github.com/a-h/templ v0.3.1020",
    ]
  ) {
    if (!goMod.includes(pin)) throw new Error(`vendored go.mod is missing pin: ${pin}`);
  }

  const license = await Deno.readTextFile(new URL("LICENSE", VENDOR_ROOT));
  if (!license.includes("free and unencumbered software released into the public domain")) {
    throw new Error("vendored Pomegranate license is missing or changed");
  }
}

function parseManifest(value: string): Map<string, string> {
  const manifest = new Map<string, string>();
  for (const line of value.trim().split("\n")) {
    const match = /^([0-9a-f]{64}) {2}\.\/(.+)$/.exec(line);
    if (!match || match[2].includes("..") || manifest.has(match[2])) {
      throw new Error(`invalid vendor manifest line: ${line}`);
    }
    manifest.set(match[2], match[1]);
  }
  return manifest;
}

async function listFiles(directory: URL, prefix = ""): Promise<string[]> {
  const files: string[] = [];
  for await (const entry of Deno.readDir(directory)) {
    const path = `${prefix}${entry.name}`;
    if (entry.isFile) {
      files.push(path);
    } else if (entry.isDirectory) {
      files.push(...await listFiles(new URL(`${entry.name}/`, directory), `${path}/`));
    } else {
      throw new Error(`vendor tree cannot contain links or special files: ${path}`);
    }
  }
  return files.sort();
}

async function sha256Hex(value: Uint8Array): Promise<string> {
  const buffer = new ArrayBuffer(value.byteLength);
  new Uint8Array(buffer).set(value);
  const digest = await crypto.subtle.digest("SHA-256", buffer);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

if (import.meta.main) {
  await validateVendorTree();
  console.log("vendored Pomegranate source passed integrity and pin checks");
}
