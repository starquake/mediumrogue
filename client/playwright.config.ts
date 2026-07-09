import { defineConfig } from "@playwright/test";

// E2E drives the real production artifact: the Go binary with the client bundle
// embedded (built by `make e2e`). A fast TURN_INTERVAL lets a browser test
// observe several world turns in under a second.
//
// Four servers, because the e2e server never releases entities (there is no
// disconnect cleanup — see the "entities never leave the world" placeholder in
// docs/STATUS.md): players joined by one spec linger for the whole run. Monsters
// hunt and cluster on the nearest player, so on a monster server that
// accumulation can push a hex to StackCap and block an unrelated movement spec's
// walk. So core specs run against a monster-FREE server, and each spec that
// actually needs monsters gets its own private one rather than sharing:
//
// A combat bubble is player-anchored (bubble.go), so a new player whose bubble
// ends up connected — via a shared monster — to an EARLIER spec's already-
// closed page becomes stuck waiting on a lock-in that page can never submit
// again (only the COMBAT_PATIENCE AFK fallback, 60s by default, would ever
// free it) — a wedge hit in practice both between combat.spec.ts's own
// (well-behaved) tests and a would-be third spec sharing its server, AND
// between combat.spec.ts and monsters.spec.ts (whose idle player never moves
// or locks in, so a monster wandering into its CombatRadius before it
// disconnects leaves that bubble stuck too — a PRE-EXISTING flake, reproduces
// on main with just those two files, not introduced by this milestone).
// Tuning COMBAT_PATIENCE down instead (tried during this milestone, not kept)
// was a losing trade: short enough to unstick a wedged bubble within any
// test's own timeout was also short enough to let combat.spec.ts's freeze
// test's own residual queued path get walked mid-assertion. Giving every
// monster-needing spec file its own single-consumer server removes the
// possibility of the wedge entirely, with zero changes to any spec's test
// logic.
const corePort = 8123;
const combatPort = 8124;
const rangedPort = 8125;
const monstersPort = 8126;

// Specs matched by name to their private monster server. Everything else is a
// "core" spec and runs against the monster-free server.
const combatSpecs = /combat\.spec\.ts$/;
const rangedSpecs = /ranged\.spec\.ts$/;
const monstersSpecs = /monsters\.spec\.ts$/;

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
      testIgnore: [combatSpecs, rangedSpecs, monstersSpecs],
      use: { baseURL: `http://127.0.0.1:${corePort}` },
    },
    {
      name: "combat",
      testMatch: combatSpecs,
      use: { baseURL: `http://127.0.0.1:${combatPort}` },
    },
    {
      name: "ranged",
      testMatch: rangedSpecs,
      use: { baseURL: `http://127.0.0.1:${rangedPort}` },
    },
    {
      name: "monsters",
      testMatch: monstersSpecs,
      use: { baseURL: `http://127.0.0.1:${monstersPort}` },
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
    {
      command: "../build/bin/rogue",
      url: `http://127.0.0.1:${rangedPort}/healthz`,
      reuseExistingServer: false,
      env: serverEnv(rangedPort, { MONSTER_COUNT: "3" }),
    },
    {
      command: "../build/bin/rogue",
      url: `http://127.0.0.1:${monstersPort}/healthz`,
      reuseExistingServer: false,
      env: serverEnv(monstersPort, { MONSTER_COUNT: "3" }),
    },
  ],
});
