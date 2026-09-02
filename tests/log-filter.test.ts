import { assertEquals, assertFalse, assertStringIncludes } from "@std/assert";

Deno.test("log filter removes NIP-46 responses and secret-bearing records", async () => {
  const canary = "PLAINTEXT-NIP46-CANARY";
  const output = await runFilter([
    "12:00 INF central server listening addr=http://localhost:15033",
    `12:01 INF broadcasting nip46 response response={\"result\":\"${canary}\"}`,
    "12:02 ERR invalid SECRET_KEY value=do-not-print",
  ].join("\n"));

  assertStringIncludes(output, "event=log_filter_started");
  assertStringIncludes(output, "central server listening");
  assertStringIncludes(output, "event=nip46_response response=[REDACTED]");
  assertStringIncludes(output, "event=sensitive_log_redacted");
  assertFalse(output.includes(canary));
  assertFalse(output.includes("do-not-print"));
});

async function runFilter(input: string): Promise<string> {
  const command = new Deno.Command("awk", {
    args: ["-v", "role=central", "-f", "docker/log-filter.awk"],
    stdin: "piped",
    stdout: "piped",
  }).spawn();
  const writer = command.stdin.getWriter();
  await writer.write(new TextEncoder().encode(input));
  await writer.close();
  const result = await command.output();
  assertEquals(result.code, 0);
  return new TextDecoder().decode(result.stdout);
}
