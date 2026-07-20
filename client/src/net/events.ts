// SSE world stream with a liveness watchdog. EventSource auto-reconnects on
// errors it *detects*, but a silently half-open connection (network away, socket
// not reset) can leave the stream stalled without ever firing `error` — the
// client would keep believing it is connected. The watchdog treats "no data
// within a turn-scaled window" as a dead stream: it reports disconnected and
// opens a fresh EventSource, which resyncs to the latest snapshot on reconnect.
import { EventChat, EventHeartbeat, EventTurn, type ChatMessage, type TurnEvent } from "../protocol.gen";

export interface EventsCallbacks {
  onTurn: (turn: TurnEvent) => void;
  onConnectionChange: (connected: boolean) => void;
  onHeartbeat?: () => void;
  onChat?: (msg: ChatMessage) => void;
}

/** Handle returned by connectEvents: tear down, or force a fresh connection. */
export interface EventsController {
  /** Stops the watchdog and closes the stream for good. */
  close: () => void;
  /**
   * Closes the current stream and opens a new one immediately, re-reading
   * `getToken()` — for when the identity (and therefore the token) just
   * changed, e.g. after a re-join minted a fresh entity.
   */
  reconnect: () => void;
}

// Liveness window: a stream is dead after this long with no data. Turn-scaled so
// a slow production cadence (4s turns → 16s window) is never mistaken for a
// drop; floored for the pre-first-bundle window.
const LIVENESS_FLOOR_MS = 3_000;
const LIVENESS_TURNS = 4;

function livenessWindow(intervalMs: number): number {
  return Math.max(LIVENESS_FLOOR_MS, intervalMs * LIVENESS_TURNS);
}

/**
 * Opens the world stream at `/api/events`, carrying the current token (from
 * `getToken`, re-read on every connect so a later re-join's new token is
 * picked up) as a query param — `/api/events?token=<token>` — so the server
 * can attribute the stream to an entity for disconnect-grace bookkeeping. An
 * empty token (no identity yet) connects to plain `/api/events` — watch-only
 * until join.
 *
 * Returns a controller: `close` stops the watchdog and closes the stream for
 * good; `reconnect` closes the current stream and opens a fresh one right
 * away (re-reading `getToken`), for when the identity/token just changed.
 * EventSource handles Last-Event-ID on its own auto-retry; a watchdog- or
 * reconnect()-driven reconnect is a fresh connection that resyncs to latest.
 */
export function connectEvents(getToken: () => string, callbacks: EventsCallbacks): EventsController {
  let source: EventSource;
  let watchdog: ReturnType<typeof setTimeout> | undefined;
  let windowMs = LIVENESS_FLOOR_MS;
  let torndown = false;

  const arm = (): void => {
    if (watchdog !== undefined) {
      clearTimeout(watchdog);
    }

    watchdog = setTimeout(() => {
      // No data in the window: the stream is dead even if EventSource has not
      // noticed. Report disconnected and reconnect from scratch.
      callbacks.onConnectionChange(false);
      source.close();
      if (!torndown) {
        connect();
      }
    }, windowMs);
  };

  const connect = (): void => {
    const token = getToken();
    const url = token === "" ? "/api/events" : `/api/events?token=${encodeURIComponent(token)}`;
    source = new EventSource(url);

    source.addEventListener(EventTurn, (event: MessageEvent<string>) => {
      const turn = JSON.parse(event.data) as TurnEvent;
      windowMs = livenessWindow(turn.intervalMs);
      callbacks.onConnectionChange(true);
      arm();
      callbacks.onTurn(turn);
    });

    source.addEventListener("open", () => {
      callbacks.onConnectionChange(true);
      arm();
    });

    // A heartbeat proves liveness even when turns stop (frozen combat clock):
    // report connected and re-arm the watchdog, same as a turn minus the payload.
    source.addEventListener(EventHeartbeat, () => {
      callbacks.onConnectionChange(true);
      arm();
      callbacks.onHeartbeat?.();
    });

    // Chat carries no `id:` (see EventChat's doc comment) — it must not
    // advance Last-Event-ID — so it doesn't touch the liveness watchdog either.
    source.addEventListener(EventChat, (event: MessageEvent<string>) => {
      const msg = JSON.parse(event.data) as ChatMessage;
      callbacks.onChat?.(msg);
    });

    // EventSource retries on its own after an error it detects; just report the
    // state. The watchdog covers the errors it never detects.
    source.addEventListener("error", () => callbacks.onConnectionChange(false));

    // Deliberately NOT armed here (before `open`): under heavy CPU load a
    // connection can take longer than windowMs to establish, and a connect-time
    // arm would close it before `open` ever fires — a close-before-open loop
    // that starves reconnection. The watchdog is armed on open/turn/heartbeat,
    // i.e. only once the stream is actually alive; a stream that never opens is
    // retried by EventSource's own auto-reconnect (the `error` path above).
  };

  connect();

  return {
    close: (): void => {
      torndown = true;
      if (watchdog !== undefined) {
        clearTimeout(watchdog);
      }

      source.close();
    },
    reconnect: (): void => {
      if (torndown) {
        return;
      }
      // Disarm the OLD stream's watchdog before handing over (#208): armed by
      // the old stream's open/turn/heartbeat, it would otherwise survive into
      // the new stream's handshake and could fire before `open` — closing the
      // fresh connection mid-handshake, the exact close-before-open pattern
      // connect() documents avoiding. connect() re-arms only once the new
      // stream is actually alive.
      if (watchdog !== undefined) {
        clearTimeout(watchdog);
        watchdog = undefined;
      }
      source.close();
      connect();
    },
  };
}
