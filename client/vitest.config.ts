import { defineConfig } from "vitest/config";

// Unit tests for pure client logic (currently: the gear store's slot-
// placement rules). Scoped to src/**/*.test.ts only — client/e2e/*.spec.ts
// are Playwright specs (a different runner/harness entirely, driven by
// `make e2e`) and must never be picked up here, so this narrows vitest's
// default "**/*.{test,spec}.*" glob rather than relying on exclusion.
export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
