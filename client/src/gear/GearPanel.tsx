import { For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import type { ItemView } from "../protocol.gen";
import { inventory, markEquipPending, pendingEquip } from "./store";

// stats renders an item's numbers compactly: "dmg 5 · rng 4 · aoe 1".
function stats(it: ItemView): string {
  const parts = [`dmg ${it.damage}`];
  if (it.rangeHex > 0) parts.push(`rng ${it.rangeHex}`);
  if (it.aoeRadius > 0) parts.push(`aoe ${it.aoeRadius}`);

  return parts.join(" · ");
}

function GearPanel(props: { equip: (itemId: number) => void }): JSXElement {
  // An equip click flips its button to "…" immediately (the intent is on the
  // wire; in a bubble the swap waits for the turn) — see the store's
  // pendingEquip for when it clears.
  const equipClick = (itemId: number): void => {
    markEquipPending(itemId);
    props.equip(itemId);
  };

  return (
    <div id="gear-panel">
      <Show when={inventory().length > 0}>
        <div id="gear-title">Gear</div>
        <For each={inventory()}>
          {(it) => (
            <div class="gear-row" classList={{ "gear-equipped": it.equipped }}>
              <button
                type="button"
                disabled={it.equipped || pendingEquip() === Number(it.id)}
                onClick={() => equipClick(Number(it.id))}
              >
                {it.equipped ? "equipped" : pendingEquip() === Number(it.id) ? "…" : "equip"}
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
