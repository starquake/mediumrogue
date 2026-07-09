import { createEffect, For } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { messages, sendChat } from "./store";

function ChatPanel(): JSXElement {
  let listEl: HTMLDivElement | undefined;
  let inputEl: HTMLInputElement | undefined;

  // Auto-scroll to newest when the log changes.
  createEffect(() => {
    messages();
    if (listEl) {
      listEl.scrollTop = listEl.scrollHeight;
    }
  });

  const submit = (e: Event): void => {
    e.preventDefault();
    if (!inputEl) {
      return;
    }
    const text = inputEl.value;
    inputEl.value = "";
    void sendChat(text);
  };

  return (
    <div id="chat-panel">
      <div id="chat-messages" ref={listEl}>
        <For each={messages()}>
          {(m) => (
            <div class="chat-line" classList={{ "chat-system": m.sender === "system" }}>
              <span class="chat-sender">{m.sender}</span>
              <span class="chat-text">{m.text}</span>
            </div>
          )}
        </For>
      </div>
      <form id="chat-form" onSubmit={submit}>
        <input
          id="chat-input"
          ref={inputEl}
          type="text"
          autocomplete="off"
          placeholder="Say something… (/here to share location)"
          maxlength={500}
        />
        <button type="submit">Send</button>
      </form>
    </div>
  );
}

/** Mount the chat panel into `root`. Keeps all JSX in this .tsx file. */
export function mountChat(root: HTMLElement): void {
  render(() => <ChatPanel />, root);
}
