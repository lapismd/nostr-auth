export interface BackupEntry {
  role: "central" | "operator";
  operatorId?: string;
  path: string;
  destination: string;
}

export interface BackupManifest {
  threshold: number;
  backups: BackupEntry[];
}

export async function validateBackupManifest(manifest: BackupManifest): Promise<void> {
  if (!Number.isSafeInteger(manifest.threshold) || manifest.threshold < 1) {
    throw new TypeError("backup threshold must be a positive integer");
  }
  if (!Array.isArray(manifest.backups) || manifest.backups.length === 0) {
    throw new TypeError("backup manifest must contain entries");
  }

  const operatorsByDestination = new Map<string, Set<string>>();
  for (const [index, backup] of manifest.backups.entries()) {
    if (!backup.path || !backup.destination) throw new TypeError(`backup ${index} is incomplete`);
    const stat = await Deno.stat(backup.path);
    if (!stat.isFile || stat.size === 0) throw new Error(`backup ${index} is not a non-empty file`);

    if (backup.role === "operator") {
      if (!backup.operatorId?.trim()) {
        throw new TypeError(`operator backup ${index} needs operatorId`);
      }
      const operators = operatorsByDestination.get(backup.destination) ?? new Set<string>();
      operators.add(backup.operatorId);
      operatorsByDestination.set(backup.destination, operators);
    } else if (backup.role !== "central") {
      throw new TypeError(`backup ${index} has an invalid role`);
    }
  }

  for (const operators of operatorsByDestination.values()) {
    if (operators.size >= manifest.threshold) {
      throw new Error(
        "a backup destination contains enough distinct operator archives to meet threshold",
      );
    }
  }
}

if (import.meta.main) {
  const path = Deno.args[0];
  if (!path) throw new Error("usage: backup-check.ts <backup-manifest.json>");
  const manifest = JSON.parse(await Deno.readTextFile(path)) as BackupManifest;
  await validateBackupManifest(manifest);
  console.log(`backup manifest passed: ${manifest.backups.length} archives`);
}
