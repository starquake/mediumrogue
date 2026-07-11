import { defineConfig } from "@playwright/test";

import identityStorageStateTemplate from "./e2e/identity-storage-state.json" with { type: "json" };

// E2E drives the real production artifact: the Go binary with the client bundle
// embedded (built by `make e2e`). A fast TURN_INTERVAL lets a browser test
// observe several world turns in under a second.
//
// ONE private server per spec file. The e2e server never releases entities
// (there is no disconnect cleanup — see the "entities never leave the world"
// placeholder in docs/STATUS.md), so specs that SHARE a server accumulate each
// other's players for the whole run. On CI's higher worker parallelism that
// reliably pushes a hex to StackCap and blocks an unrelated movement spec's walk
// (e.g. walk.spec — reproduced under `--workers=12`), and on a monster server a
// player-anchored combat bubble can wedge on an already-closed page (only the
// 60s COMBAT_PATIENCE AFK fallback would free it). Giving every spec its own
// single-consumer server removes cross-spec state sharing entirely, with zero
// changes to any spec's test logic. Tracked as issue #21; the real product fix
// is disconnect cleanup. MONSTER_COUNT is set only for the specs that need it.
const BASE_PORT = 8123;

// Each e2e spec file gets its own server; `monsters` = how many that server
// spawns (omitted → monster-free).
const specs: { name: string; monsters?: number }[] = [
  { name: "hex" },
  { name: "turn" },
  { name: "move" },
  { name: "walk" },
  { name: "heartbeat" },
  { name: "multiplayer" },
  { name: "class" },
  { name: "species" },
  { name: "procgen" },
  { name: "chat" },
  { name: "parties" },
  { name: "quests" },
  { name: "gear" },
  { name: "combat", monsters: 3 },
  { name: "ranged", monsters: 3 },
  { name: "monsters", monsters: 3 },
  // kinds needs several distinct monster kinds actually spawned to prove
  // per-kind rendering (milestone 6c). WORLD_SEED only drives map/quest
  // generation, not SpawnMonsters' kind pick (still crypto/random per
  // server start — see the drop-roll determinism note in
  // test/integration/gear_test.go), so there is no env knob to force
  // specific kinds; a large count instead makes "at least 2 distinct
  // kinds among them" a near-certainty rather than a coin flip.
  { name: "kinds", monsters: 30 },
];

const portFor = (i: number): number => BASE_PORT + i;

const serverEnv = (port: number, monsters?: number): Record<string, string> => ({
  LISTEN_ADDR: `:${port}`,
  TURN_INTERVAL: "250ms",
  // Fast heartbeat so a browser test observes named heartbeat events within its
  // short run (default is 15s — never seen in a fast e2e).
  HEARTBEAT_INTERVAL: "500ms",
  ...(monsters ? { MONSTER_COUNT: String(monsters) } : {}),
});

// Every project gets its browser context pre-seeded (via storageState) with a
// "remembered" identity — fighter/human/traveler, no token — so the new
// character-creation start screen (src/main.ts's isNewPlayer) never appears
// and every existing spec keeps auto-joining exactly as it did before that
// screen existed. identity-storage-state.json is the committed template;
// each project's origin must match its own baseURL (a distinct port per spec
// — see BASE_PORT/portFor above), so the origin is substituted per project
// rather than shared verbatim. class.spec.ts (rewritten as the start-screen
// spec) overrides this per-test with an explicitly cleared storageState;
// ranged/gear/species/quests seed their own identity via addInitScript,
// which runs after context creation and simply overwrites this default.
const storageStateFor = (port: number) => ({
  cookies: identityStorageStateTemplate.cookies,
  origins: identityStorageStateTemplate.origins.map((o) => ({
    ...o,
    origin: `http://127.0.0.1:${port}`,
  })),
});

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  projects: specs.map((s, i) => ({
    name: s.name,
    testMatch: new RegExp(`${s.name}\\.spec\\.ts$`),
    use: { baseURL: `http://127.0.0.1:${portFor(i)}`, storageState: storageStateFor(portFor(i)) },
  })),
  webServer: specs.map((s, i) => ({
    command: "../build/bin/rogue",
    url: `http://127.0.0.1:${portFor(i)}/healthz`,
    reuseExistingServer: false,
    env: serverEnv(portFor(i), s.monsters),
  })),
});
