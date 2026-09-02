import { finalizeEvent, generateSecretKey } from "@nostr/tools";

export interface GenerateSecretsOptions {
  centralKeyPath: string;
  testTokenPath?: string;
  email?: string;
  now?: () => number;
}

export async function generateSecrets(options: GenerateSecretsOptions): Promise<void> {
  const centralKey = await loadOrCreateSecretKey(options.centralKeyPath);
  try {
    if (options.testTokenPath) {
      const email = options.email?.trim().toLowerCase();
      if (!email) throw new Error("--email is required when --test-token is used");
      const event = finalizeEvent({
        kind: 20443,
        created_at: Math.floor((options.now ?? Date.now)() / 1000),
        content: "",
        tags: [["email", email]],
      }, centralKey);
      const token = encodeBase64(new TextEncoder().encode(JSON.stringify(event)));
      await writeSecret(options.testTokenPath, token);
    }
  } finally {
    centralKey.fill(0);
  }
}

async function loadOrCreateSecretKey(path: string): Promise<Uint8Array> {
  try {
    const value = (await Deno.readTextFile(path)).trim();
    if (!/^[0-9a-f]{64}$/.test(value)) throw new Error("existing central key is invalid");
    return Uint8Array.from(value.match(/../g)!, (byte) => Number.parseInt(byte, 16));
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) throw error;
  }

  const secretKey = generateSecretKey();
  await writeSecret(path, bytesToHex(secretKey));
  return secretKey;
}

async function writeSecret(path: string, value: string): Promise<void> {
  const slash = path.lastIndexOf("/");
  if (slash > 0) await Deno.mkdir(path.slice(0, slash), { recursive: true, mode: 0o700 });
  await Deno.writeTextFile(path, `${value}\n`, { mode: 0o600 });
  await Deno.chmod(path, 0o600);
}

function encodeBase64(value: Uint8Array): string {
  let binary = "";
  for (const byte of value) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function bytesToHex(value: Uint8Array): string {
  return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function parseArgs(args: string[]): GenerateSecretsOptions {
  const values = new Map<string, string>();
  for (const argument of args) {
    const match = /^--([^=]+)=(.+)$/.exec(argument);
    if (!match) throw new Error(`invalid argument: ${argument}`);
    values.set(match[1], match[2]);
  }
  const centralKeyPath = values.get("central-key");
  if (!centralKeyPath) throw new Error("--central-key=<path> is required");
  return {
    centralKeyPath,
    testTokenPath: values.get("test-token"),
    email: values.get("email"),
  };
}

if (import.meta.main) {
  await generateSecrets(parseArgs(Deno.args));
  console.log("Pomegranate secret files are ready (values withheld).");
}
