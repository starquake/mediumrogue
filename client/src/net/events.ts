// SSE world stream. EventSource handles reconnection itself (sending
// Last-Event-ID, which the server will use for turn replay in a later
// milestone); this module only parses frames into typed callbacks.
import { EventTurn, type TurnEvent } from "../protocol.gen";

export interface EventsCallbacks {
  onTurn: (turn: TurnEvent) => void;
  onConnectionChange: (connected: boolean) => void;
}

export function connectEvents(callbacks: EventsCallbacks): EventSource {
  const source = new EventSource("/api/events");

  source.addEventListener(EventTurn, (event: MessageEvent<string>) => {
    callbacks.onTurn(JSON.parse(event.data) as TurnEvent);
  });
  source.addEventListener("open", () => callbacks.onConnectionChange(true));
  // EventSource fires "error" on any drop, then retries on its own; report
  // the state so the UI can show "reconnecting".
  source.addEventListener("error", () => callbacks.onConnectionChange(false));

  return source;
}
