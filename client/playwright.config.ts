import { defineConfig } from "@playwright/test";

// E2E drives the real production artifact: the Go binary with the client
// bundle embedded (built by `make e2e`). A fast TURN_INTERVAL lets a browser
// test observe several world turns in under a second.
const port = 8123;

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
  },
  webServer: {
    command: "../build/bin/rogue",
    url: `http://127.0.0.1:${port}/healthz`,
    reuseExistingServer: false,
    env: {
      LISTEN_ADDR: `:${port}`,
      TURN_INTERVAL: "250ms",
      // Fast heartbeat so a browser test observes named heartbeat events within
      // its short run (default is 15s — never seen in a fast e2e).
      HEARTBEAT_INTERVAL: "500ms",
    },
  },
});
