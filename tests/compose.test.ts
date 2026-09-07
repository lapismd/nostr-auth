import { assert, assertEquals, assertFalse, assertStringIncludes } from "@std/assert";

const PINNED_REF = "ca0e7a9d697a7db0fc918f9b9335bc5b58d2ac3f";

Deno.test("development Compose renders a hardened, isolated 2-of-3 topology", async () => {
  const config = await renderCompose([
    "-f",
    "compose.yml",
    "-f",
    "compose.dev.yml",
    "--profile",
    "test",
  ], { DOCKER_SOCKET_GID: "123" });
  const services = config.services as Record<string, any>;
  assertEquals(Object.keys(services).sort(), [
    "central",
    "operator-1",
    "operator-2",
    "operator-3",
    "secret-init",
    "test-runner",
  ]);

  const dataVolumes = new Set<string>();
  for (const name of ["operator-1", "operator-2", "operator-3"]) {
    const service = services[name];
    assertEquals(service.environment.POMEGRANATE_ROLE, "operator");
    assertEquals(service.environment.SERVICE_URL, `http://${name}:5041`);
    assertEquals(service.environment.TRUSTED_CENTRAL_URLS, "http://central:5033");
    assertEquals(service.environment.GITHUB_CLIENT_ID, "deterministic-test-client");
    assertEquals(service.environment.GITHUB_CLIENT_SECRET, "deterministic-test-secret");
    assertEquals(service.image, "nostr-auth/pomegranate:ca0e7a9d697a");
    assert(service.read_only);
    assertEquals(service.cap_drop, ["ALL"]);
    assert(service.healthcheck);
    assert(service.restart);
    assert(service.deploy.resources.limits.memory);
    const dataVolume = service.volumes.find((volume: any) =>
      volume.target === "/var/lib/pomegranate"
    );
    dataVolumes.add(dataVolume.source);
  }
  assertEquals(dataVolumes, new Set(["operator-1-data", "operator-2-data", "operator-3-data"]));

  assertEquals(services.central.environment.POMEGRANATE_ROLE, "central");
  assertEquals(services.central.environment.POMEGRANATE_INTERNAL_PORT, "15033");
  assertEquals(services.central.environment.GITHUB_CLIENT_ID, "deterministic-test-client");
  assertEquals(services.central.environment.GITHUB_CLIENT_SECRET, "deterministic-test-secret");
  assertEquals(services.central.build.args.POMEGRANATE_REF, PINNED_REF);
  assert(services.central.volumes.some((volume: any) => volume.source === "central-data"));
  assert(
    services.central.volumes.some((volume: any) =>
      volume.source === "central-secrets" && volume.read_only
    ),
  );
  assert(
    services["test-runner"].volumes.some((volume: any) =>
      volume.source === "test-auth" && volume.read_only
    ),
  );
  assertFalse(
    services["test-runner"].volumes.some((volume: any) => volume.source === "central-secrets"),
  );
  assertEquals(services["test-runner"].group_add, ["123"]);
});

Deno.test("production Compose exposes only central and the optional operator to the proxy", async () => {
  const config = await renderCompose([
    "-f",
    "compose.yml",
    "-f",
    "compose.prod.yml",
    "--profile",
    "operator",
  ], {
    CENTRAL_GOOGLE_CLIENT_ID: "placeholder",
    CENTRAL_GOOGLE_CLIENT_SECRET: "placeholder",
    CENTRAL_GITHUB_CLIENT_ID: "central-github-id",
    CENTRAL_GITHUB_CLIENT_SECRET: "central-github-secret",
    CENTRAL_SECRET_KEY: "1".repeat(64),
    OPERATOR_GOOGLE_CLIENT_ID: "placeholder",
    OPERATOR_GOOGLE_CLIENT_SECRET: "placeholder",
    OPERATOR_GITHUB_CLIENT_ID: "operator-github-id",
    OPERATOR_GITHUB_CLIENT_SECRET: "operator-github-secret",
  });
  const services = config.services as Record<string, any>;
  assertEquals(Object.keys(services).sort(), ["central", "operator"]);
  assertEquals(services.central.environment.SERVICE_URL, "https://auth.example.com");
  assertEquals(services.central.environment.GITHUB_CLIENT_ID, "central-github-id");
  assertEquals(services.central.environment.GITHUB_CLIENT_SECRET, "central-github-secret");
  assertEquals(services.operator.environment.SERVICE_URL, "https://operator.example.com");
  assertEquals(services.operator.environment.GITHUB_CLIENT_ID, "operator-github-id");
  assertEquals(
    services.operator.environment.GITHUB_CLIENT_SECRET,
    "operator-github-secret",
  );
  assertEquals(
    services.operator.environment.TRUSTED_CENTRAL_URLS,
    "https://auth.example.com",
  );
  assertEquals(services.central.expose, ["5033"]);
  assertEquals(services.operator.expose, ["5041"]);
  assertFalse("ports" in services.central);
  assertFalse("ports" in services.operator);
  assertEquals(services.operator.profiles, ["operator"]);

  const rendered = JSON.stringify(config);
  assertFalse(rendered.includes(":latest"));
  assertStringIncludes(rendered, PINNED_REF);
  assertFalse(rendered.includes("operator-a.example"));
  assertFalse(rendered.includes("operator-b.example"));
});

Deno.test("base production topology omits the optional operator by default", async () => {
  const config = await renderCompose(["-f", "compose.yml", "-f", "compose.prod.yml"], {
    CENTRAL_SECRET_KEY: "1".repeat(64),
  });
  assertEquals(Object.keys(config.services as Record<string, unknown>), ["central"]);
});

async function renderCompose(
  arguments_: string[],
  environment: Record<string, string> = {},
): Promise<any> {
  const command = new Deno.Command("docker", {
    args: ["compose", ...arguments_, "config", "--format", "json"],
    env: {
      COMPOSE_PROJECT_NAME: "nostr-auth-test",
      POMEGRANATE_REF: PINNED_REF,
      ...environment,
    },
    stdout: "piped",
    stderr: "piped",
  });
  const result = await command.output();
  if (!result.success) {
    throw new Error(new TextDecoder().decode(result.stderr));
  }
  return JSON.parse(new TextDecoder().decode(result.stdout));
}
