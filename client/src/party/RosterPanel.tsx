import { For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { party } from "./store";

function RosterPanel(): JSXElement {
  return (
    <Show when={party().length > 0}>
      <div id="roster-panel">
        <div id="roster-title">Party</div>
        <For each={party()}>{(name) => <div class="roster-member">{name}</div>}</For>
      </div>
    </Show>
  );
}

/** Mount the roster panel into `root`. Keeps JSX in this .tsx file. */
export function mountRoster(root: HTMLElement): void {
  render(() => <RosterPanel />, root);
}
