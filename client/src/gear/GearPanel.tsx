import { For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import type { ItemView } from "../protocol.gen";
import { inventory } from "./store";

// stats renders an item's numbers compactly: "dmg 5 · rng 4 · aoe 1".
function stats(it: ItemView): string {
  const parts = [`dmg ${it.damage}`];
  if (it.rangeHex > 0) parts.push(`rng ${it.rangeHex}`);
  if (it.aoeRadius > 0) parts.push(`aoe ${it.aoeRadius}`);

  return parts.join(" · ");
}

function GearPanel(props: { equip: (itemId: number) => void }): JSXElement {
  return (
    <div id="gear-panel">
      <Show when={inventory().length > 0}>
        <div id="gear-title">Gear</div>
        <For each={inventory()}>
          {(it) => (
            <div class="gear-row" classList={{ "gear-equipped": it.equipped }}>
              <button type="button" disabled={it.equipped} onClick={() => props.equip(Number(it.id))}>
                {it.equipped ? "worn" : "equip"}
              </button>
              <span class="gear-name">{it.name}</span>
              <span class="gear-stats">{stats(it)}</span>
              <Show when={it.desc !== ""}>
                <span class="gear-desc">{it.desc}</span>
              </Show>
            </div>
          )}
        </For>
      </Show>
    </div>
  );
}

/** Mount the gear panel into `root`. Keeps JSX in this .tsx file. */
export function mountGear(root: HTMLElement, equip: (itemId: number) => void): void {
  render(() => <GearPanel equip={equip} />, root);
}
