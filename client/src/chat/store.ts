import { createSignal } from "solid-js";

import { sendChat as postChat } from "../net/session";
import type { ChatMessage } from "../protocol.gen";

// One global channel. messages() is the reactive log the panel renders;
// capped to the last CAP for memory hygiene (NOT history — purely client-side).
const CAP = 200;

const [messages, setMessages] = createSignal<ChatMessage[]>([]);
let nextLocalSeq = -1; // local system lines use negative seqs (never collide)

export { messages };

// window.game.chat stays in sync via a getter (defined in main.ts) that reads
// `messages()` directly on each access — no separate mirror-write needed here.

/** Append a server chat message (from the SSE stream). */
export function appendChat(msg: ChatMessage): void {
  setMessages((prev) => [...prev, msg].slice(-CAP));
}

/** Append a local-only system line (e.g. a rejected /command). Not broadcast. */
export function appendSystem(text: string): void {
  setMessages((prev) => [...prev, { seq: nextLocalSeq--, sender: "system", text }].slice(-CAP));
}

// The current identity token; updated on join/re-join so sendChat authenticates.
let currentToken = "";
export function setChatToken(token: string): void {
  currentToken = token;
}

/** Send a line; on a command-error (422) echo a local system line. */
export async function sendChat(text: string): Promise<void> {
  const trimmed = text.trim();
  if (trimmed === "" || currentToken === "") {
    return;
  }

  try {
    await postChat(currentToken, trimmed);
  } catch (err) {
    appendSystem(err instanceof Error ? err.message : "chat send failed");
  }
}
