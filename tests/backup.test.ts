import { assertRejects } from "@std/assert";
import { validateBackupManifest } from "../scripts/backup-check.ts";

Deno.test("backup checker accepts split operator destinations", async () => {
  const directory = await Deno.makeTempDir();
  try {
    const paths = await createArchives(directory, 3);
    await validateBackupManifest({
      threshold: 2,
      backups: [
        { role: "central", path: paths[0], destination: "central-vault" },
        { role: "operator", operatorId: "one", path: paths[1], destination: "vault-one" },
        { role: "operator", operatorId: "two", path: paths[2], destination: "vault-two" },
      ],
    });
  } finally {
    await Deno.remove(directory, { recursive: true });
  }
});

Deno.test("backup checker rejects threshold operator archives in one destination", async () => {
  const directory = await Deno.makeTempDir();
  try {
    const paths = await createArchives(directory, 2);
    await assertRejects(
      () =>
        validateBackupManifest({
          threshold: 2,
          backups: [
            { role: "operator", operatorId: "one", path: paths[0], destination: "shared-vault" },
            { role: "operator", operatorId: "two", path: paths[1], destination: "shared-vault" },
          ],
        }),
      Error,
      "enough distinct operator archives",
    );
  } finally {
    await Deno.remove(directory, { recursive: true });
  }
});

async function createArchives(directory: string, count: number): Promise<string[]> {
  const paths: string[] = [];
  for (let index = 0; index < count; index++) {
    const path = `${directory}/${index}.archive`;
    await Deno.writeTextFile(path, `archive-${index}`);
    paths.push(path);
  }
  return paths;
}
