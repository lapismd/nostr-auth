import { validateVendorTree } from "../scripts/vendor-check.ts";

Deno.test("vendored Pomegranate source matches its audited manifest", async () => {
  await validateVendorTree();
});
