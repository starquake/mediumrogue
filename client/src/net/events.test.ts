import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { connectEvents, type EventsCallbacks } from "./events";

// Minimal controllable EventSource stand-in: records instances so a test can
// address "the stream before reconnect" vs "the fresh one", and fires
// listeners only when told — a stream that has not `emit`ted "open" is still
// mid-handshake, exactly the window the watchdog bug lived in (#208).
class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  closed = false;
  private readonly listeners = new Map<string, ((event: unknown) => void)[]>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: unknown) => void): void {
    const list = this.listeners.get(type) ?? [];
    list.push(listener);
    this.listeners.set(type, list);
  }

  close(): void {
    this.closed = true;
  }

  emit(type: string, data?: string): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener({ data });
    }
  }
}

// Well past the liveness floor (3s) — if any watchdog is armed, advancing
// this far fires it.
const PAST_ANY_WINDOW_MS = 60_000;

// Checked index into the instance list (noUncheckedIndexedAccess).
function stream(i: number): FakeEventSource {
  const inst = FakeEventSource.instances[i];
  if (inst === undefined) {
    throw new Error(`no EventSource instance ${i} (have ${FakeEventSource.instances.length})`);
  }

  return inst;
}

function noopCallbacks(overrides: Partial<EventsCallbacks> = {}): EventsCallbacks {
  return {
    onTurn: () => {},
    onConnectionChange: () => {},
    ...overrides,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("liveness watchdog", () => {
  // Baseline: proves this harness actually exercises the watchdog — the
  // reconnect test below would pass vacuously if the timer never fired here.
  test("a stream that opens and then goes silent is closed and replaced", () => {
    const changes: boolean[] = [];
    connectEvents(
      () => "",
      noopCallbacks({ onConnectionChange: (c) => changes.push(c) }),
    );

    expect(FakeEventSource.instances).toHaveLength(1);
    const first = stream(0);
    first.emit("open"); // arms the watchdog

    vi.advanceTimersByTime(PAST_ANY_WINDOW_MS);

    expect(first.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(2); // watchdog reconnected
    expect(changes).toEqual([true, false]); // open, then declared dead
  });

  test("reconnect() disarms the old stream's watchdog — it must not fire mid-handshake and close the new stream before open (#208)", () => {
    const controller = connectEvents(() => "", noopCallbacks());

    const first = stream(0);
    first.emit("open"); // old stream alive → its watchdog is armed

    controller.reconnect();
    expect(first.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(2);
    const second = stream(1);

    // The new stream is still mid-handshake (no "open" yet — e.g. a slow
    // connect under load). The OLD watchdog, were it still armed, would fire
    // in this window and close it — the close-before-open loop connect()'s
    // deliberately-not-armed-here comment documents avoiding.
    vi.advanceTimersByTime(PAST_ANY_WINDOW_MS);

    expect(second.closed).toBe(false);
    expect(FakeEventSource.instances).toHaveLength(2); // no spurious reconnect cycle
  });

  test("close() after reconnect() tears down for good", () => {
    const controller = connectEvents(() => "", noopCallbacks());
    stream(0).emit("open");
    controller.reconnect();

    const second = stream(1);
    second.emit("open"); // new stream alive → its own watchdog armed

    controller.close();
    expect(second.closed).toBe(true);

    vi.advanceTimersByTime(PAST_ANY_WINDOW_MS);
    expect(FakeEventSource.instances).toHaveLength(2); // torndown: no revival
  });
});
