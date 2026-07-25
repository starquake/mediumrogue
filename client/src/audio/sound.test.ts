import { beforeEach, describe, expect, it, vi } from "vitest";

// Howler touches the Web Audio API, which does not exist under vitest — stub it
// to the surface sound.ts actually uses. The point of these tests is the RULES
// (budget, mute, unlock, falloff), not that a browser makes a noise.
const played: string[] = [];
vi.mock("howler", () => ({
  Howl: class {
    constructor(public opts: { src: string[] }) {}
    volume(): void {}
    play(): void {
      played.push(this.opts.src[0] ?? "");
    }
  },
  Howler: { mute: (): void => {}, ctx: { resume: async (): Promise<void> => {} } },
}));

// vitest runs in node, which has no localStorage. sound.ts guards every access
// in try/catch (private-mode browsers throw), so the module works without one —
// but the persistence assertion needs somewhere for the setting to land.
const store = new Map<string, string>();
vi.stubGlobal("localStorage", {
  getItem: (k: string): string | null => store.get(k) ?? null,
  setItem: (k: string, v: string): void => void store.set(k, v),
});

vi.stubGlobal("window", { addEventListener: (): void => {} });

const { debug, isMuted, load, play, setMuted, startTurn, volumeForDistance } = await import("./sound");
const { InterestRadius } = await import("../protocol.gen");

describe("sound", () => {
  beforeEach(() => {
    played.length = 0;
    load();
    startTurn();
    setMuted(false);
    debug.unlocked = true;
    debug.played = 0;
  });

  it("plays nothing until a gesture unlocks it", () => {
    debug.unlocked = false;

    expect(play("hit")).toBe(false);
    expect(debug.played).toBe(0);
  });

  it("plays nothing while muted, and resumes when unmuted", () => {
    setMuted(true);
    expect(play("hit")).toBe(false);

    setMuted(false);
    expect(play("hit")).toBe(true);
  });

  it("persists the mute setting", () => {
    setMuted(true);
    expect(isMuted()).toBe(true);
    expect(localStorage.getItem("mediumrogue.muted")).toBe("1");
  });

  it("caps how many sounds one turn may start", () => {
    // A six-way bubble resolution must not fire six clips in a frame.
    const results = Array.from({ length: 6 }, () => play("hit"));

    expect(results.filter(Boolean)).toHaveLength(4);
    expect(debug.played).toBe(4);
  });

  it("refills the budget each turn", () => {
    Array.from({ length: 6 }, () => play("hit"));
    startTurn();

    expect(play("hit")).toBe(true);
  });

  it("records the last sound for the debug surface", () => {
    play("crit");

    expect(debug.last).toBe("crit");
  });

  it("falls off with distance, full volume at the viewer", () => {
    expect(volumeForDistance(0)).toBe(1);
    expect(volumeForDistance(InterestRadius)).toBeLessThan(volumeForDistance(1));
    // Never silent inside the radius — a hit you were sent is a hit you can hear.
    expect(volumeForDistance(InterestRadius)).toBeGreaterThan(0);
  });

  it("clamps distances beyond the interest radius", () => {
    expect(volumeForDistance(InterestRadius * 3)).toBe(volumeForDistance(InterestRadius));
  });

  it("picks among a sound's variants", () => {
    // footstep has five clips; over many plays more than one must appear, or
    // walking becomes a metronome.
    const seen = new Set<string>();
    for (let i = 0; i < 60; i++) {
      startTurn();
      play("footstep");
      seen.add(played[played.length - 1] ?? "");
    }

    expect(seen.size).toBeGreaterThan(1);
  });
});
