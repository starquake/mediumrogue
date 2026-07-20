import { afterEach, describe, expect, test, vi } from "vitest";

import { onIntentFeedback, submitDrop, submitUseSkill } from "./session";

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
