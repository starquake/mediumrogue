import { afterEach, describe, expect, test, vi } from "vitest";

import { onIntentFeedback, submitDrop, submitPickup, submitUseSkill } from "./session";

const identity = { entityId: 1, token: "t" };

afterEach(() => {
  vi.restoreAllMocks();
  onIntentFeedback(() => {}); // reset the sink
});

describe("intent rejection feedback (#193)", () => {
  test("surfaces the server's 422 reason to the feedback sink", async () => {
    const seen: string[] = [];
    onIntentFeedback((m) => seen.push(m));

    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "backpack full" }), { status: 422 }),
    );

    const ok = await submitDrop(identity, 7);

    expect(ok).toBe(false);
    expect(seen).toEqual(["backpack full"]);
  });

  test("a network failure surfaces a transient message and does NOT throw", async () => {
    const seen: string[] = [];
    onIntentFeedback((m) => seen.push(m));

    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("failed to fetch"));

    // Must resolve false, never reject — a rejection would reach the global
    // unhandledrejection handler and raise the false "client died" banner.
    const ok = await submitDrop(identity, 7);

    expect(ok).toBe(false);
    expect(seen).toHaveLength(1);
    expect(seen[0]).toMatch(/server/i);
  });

  test("a 202 accept notifies nothing and returns true", async () => {
    const seen: string[] = [];
    onIntentFeedback((m) => seen.push(m));

    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 202 }));

    const ok = await submitDrop(identity, 7);

    expect(ok).toBe(true);
    expect(seen).toEqual([]);
  });
});

describe("submitPickup (#193)", () => {
  test("returns the server's reason and does NOT toast — the modal surfaces it inline", async () => {
    const seen: string[] = [];
    onIntentFeedback((m) => seen.push(m));

    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "backpack full" }), { status: 422 }),
    );

    const outcome = await submitPickup(identity, 42);

    expect(outcome).toEqual({ ok: false, reason: "backpack full" });
    expect(seen).toEqual([]); // suppressed: the pickup modal shows the reason, no double toast
  });

  test("a network failure still toasts a transient blip and never throws", async () => {
    const seen: string[] = [];
    onIntentFeedback((m) => seen.push(m));

    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("failed to fetch"));

    const outcome = await submitPickup(identity, 42);

    expect(outcome).toEqual({ ok: false, reason: "" });
    expect(seen).toHaveLength(1); // a network blip isn't a per-row reason, so it toasts
  });
});

describe("submitUseSkill (#185)", () => {
  test("posts a use-skill intent carrying the skill id and target hex", async () => {
    let body: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation((_url, init) => {
      body = JSON.parse(String((init as RequestInit).body));
      return Promise.resolve(new Response(null, { status: 202 }));
    });

    const ok = await submitUseSkill({ entityId: 1, token: "t" }, "blink", { q: 2, r: -3 });

    expect(ok).toBe(true);
    expect(body).toMatchObject({ kind: "use-skill", skillId: "blink", target: { q: 2, r: -3 } });
  });
});
