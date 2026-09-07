import { assert, assertFalse, assertStringIncludes } from "@std/assert";

const workflows = new URL("../.github/workflows/", import.meta.url);

Deno.test("CI runs the complete checks and live Compose smoke test", async () => {
  const workflow = await Deno.readTextFile(new URL("ci.yml", workflows));

  assertStringIncludes(workflow, "pull_request:");
  assertStringIncludes(workflow, "push:");
  assertStringIncludes(workflow, "workflow_dispatch:");
  assertStringIncludes(workflow, "permissions:\n  contents: read");
  assertStringIncludes(workflow, "task check:all");
  assertStringIncludes(workflow, "task smoke");
  assertStringIncludes(
    workflow,
    'DOCKER_SOCKET_GID="$(stat --format=%g /var/run/docker.sock)"',
  );
  assertRuntimeVulnerabilityGate(workflow);
  assertStringIncludes(workflow, "down --volumes --remove-orphans");
  assertFalse(workflow.includes("pull_request_target:"));
  assertActionsUseImmutableCommits(workflow);
});

Deno.test("trusted publication builds an attested multi-architecture GHCR image", async () => {
  const workflow = await Deno.readTextFile(new URL("publish-image.yml", workflows));

  assertStringIncludes(workflow, "branches: [main]");
  assertStringIncludes(workflow, 'tags: ["v*.*.*"]');
  assertStringIncludes(workflow, "workflow_dispatch:");
  assertFalse(workflow.includes("pull_request:"));
  assertFalse(workflow.includes("pull_request_target:"));
  assertStringIncludes(workflow, "needs: verify");
  assertStringIncludes(workflow, "packages: write");
  assertStringIncludes(workflow, "attestations: write");
  assertStringIncludes(workflow, "artifact-metadata: write");
  assertStringIncludes(workflow, "id-token: write");
  assertStringIncludes(workflow, "ghcr.io/${{ github.repository_owner }}/nostr-auth-pomegranate");
  assertStringIncludes(workflow, 'echo "IMAGE_NAME=${IMAGE_NAME,,}" >> "$GITHUB_ENV"');
  assertStringIncludes(workflow, "platforms: linux/amd64,linux/arm64");
  assertStringIncludes(workflow, "provenance: mode=max");
  assertStringIncludes(
    workflow,
    "sbom: generator=docker/buildkit-syft-scanner:stable-1@sha256:ae4f3b554449e7e25548e7d8ccc029d17357348e30c6e3df01b92bc93654d6a9",
  );
  assertStringIncludes(workflow, "push: true");
  assertStringIncludes(workflow, "push-to-registry: true");
  assertStringIncludes(
    workflow,
    'DOCKER_SOCKET_GID="$(stat --format=%g /var/run/docker.sock)"',
  );
  assertRuntimeVulnerabilityGate(workflow);
  assertStringIncludes(
    workflow,
    "tonistiigi/binfmt:qemu-v10.2.3@sha256:400a4873b838d1b89194d982c45e5fb3cda4593fbfd7e08a02e76b03b21166f0",
  );
  assertStringIncludes(
    workflow,
    "moby/buildkit:v0.33.0@sha256:6c2fa84a6b61ccd72899dde4239f8d5717f05f9a8ca6f3cad185fb1a95a94de3",
  );
  assertStringIncludes(workflow, "docker logout ghcr.io");
  assertStringIncludes(workflow, "docker buildx imagetools inspect");
  assertStringIncludes(
    workflow,
    "org.opencontainers.image.source=https://github.com/${{ github.repository }}",
  );
  assertActionsUseImmutableCommits(workflow);

  const dockerfile = await Deno.readTextFile(new URL("../docker/Dockerfile", import.meta.url));
  assertStringIncludes(
    dockerfile,
    "# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e",
  );
  assertStringIncludes(dockerfile, "libcrypto3=3.5.8-r0");
  assertStringIncludes(dockerfile, "libssl3=3.5.8-r0");
});

function assertRuntimeVulnerabilityGate(workflow: string): void {
  assertStringIncludes(
    workflow,
    "aquasec/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969",
  );
  assertStringIncludes(workflow, "--severity HIGH,CRITICAL");
  assertStringIncludes(workflow, "--ignore-unfixed");
  assertStringIncludes(workflow, "--exit-code 1");
}

function assertActionsUseImmutableCommits(workflow: string): void {
  const actionUses = [...workflow.matchAll(/^\s*-?\s*uses:\s+([^\s#]+)(?:\s+#.*)?$/gm)];
  assert(actionUses.length > 0, "workflow must use at least one action");

  for (const [, action] of actionUses) {
    assert(
      /^[\w.-]+\/[\w.-]+\/[\w./-]+@[0-9a-f]{40}$/.test(action) ||
        /^[\w.-]+\/[\w.-]+@[0-9a-f]{40}$/.test(action),
      `action must be pinned to a full commit: ${action}`,
    );
  }
}
