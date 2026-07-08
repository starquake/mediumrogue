import { defineConfig } from "@playwright/test";

// E2E drives the real production artifact: the Go binary with the client bundle
// embedded (built by `make e2e`). A fast TURN_INTERVAL lets a browser test
// observe several world turns in under a second.
//
// Two servers, because the e2e server never releases entities (there is no
// disconnect cleanup — see the "entities never leave the world" placeholder in
// docs/STATUS.md): players joined by one spec linger for the whole run. Monsters
// hunt and cluster on the nearest player, so on a monster server that
// accumulation can push a hex to StackCap and block an unrelated movement spec's
// walk. So core specs run against a monster-FREE server, and only the
// monster/combat specs get a server with MONSTER_COUNT set.
const corePort = 8123;
const combatPort = 8124;

// Specs that need monsters present. Everything else is a "core" spec and must
// run against the monster-free server.
const combatSpecs = /(monsters|combat)\.spec\.ts$/;

const serverEnv = (port: number, extra: Record<string, string> = {}): Record<string, string> => ({
  LISTEN_ADDR: `:${port}`,
  TURN_INTERVAL: "250ms",
  // Fast heartbeat so a browser test observes named heartbeat events within its
  // short run (default is 15s — never seen in a fast e2e).
  HEARTBEAT_INTERVAL: "500ms",
  ...extra,
});

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  projects: [
    {
      name: "core",
      testIgnore: combatSpecs,
      use: { baseURL: `http://127.0.0.1:${corePort}` },
    },
    {
      name: "combat",
      testMatch: combatSpecs,
      use: { baseURL: `http://127.0.0.1:${combatPort}` },
    },
  ],
  webServer: [
    {
      command: "../build/bin/rogue",
      url: `http://127.0.0.1:${corePort}/healthz`,
      reuseExistingServer: false,
      env: serverEnv(corePort),
    },
    {
      command: "../build/bin/rogue",
      url: `http://127.0.0.1:${combatPort}/healthz`,
      reuseExistingServer: false,
      env: serverEnv(combatPort, { MONSTER_COUNT: "3" }),
    },
  ],
});
