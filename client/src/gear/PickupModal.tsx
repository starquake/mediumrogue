import { Index, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { dismissPickup, modalOpen, pickupRows, taking, typeLabel } from "./store";

/** Callbacks the pickup modal needs. */
export interface PickupActions {
  take: (groundItemId: number) => void;
}

function PickupModal(props: { actions: PickupActions }): JSXElement {
  return (
    <Show when={modalOpen()}>
      <div id="pickup-modal" class="panel prompt">
        <div class="title">On the ground — pick what you want</div>
        <div class="rowlist">
          {/* Index, not For: main.ts rebuilds a fresh rows array every turn
              bundle, so For (keyed by reference) would remount every row —
              detaching the "take" button mid-click under load. Index keys by
              position; the row DOM stays stable, content updates via the
              accessor. */}
          <Index each={pickupRows()}>
            {(row) => (
              <div class="grow" classList={{ rejected: row().rejected }} data-ground={row().id}>
                <div>
                  <span class="itemline">{row().count > 1 ? `${row().name} ×${row().count}` : row().name}</span>
                  <span class="typeline"> · {typeLabel(row().type)}</span>
                  <Show when={row().rejected}>
                    <div class="full">⚠ backpack full — drop something first</div>
                  </Show>
                </div>
                <button
                  type="button"
                  class="yes"
                  classList={{ taking: taking().has(row().id) }}
                  disabled={row().rejected || taking().has(row().id)}
                  onClick={() => props.actions.take(row().id)}
                >
                  <Show when={taking().has(row().id)} fallback={"take"}>
                    <span class="spinner" />
                  </Show>
                </button>
              </div>
            )}
          </Index>
        </div>
        <div class="buttons" style="margin-top:.9rem">
          <button type="button" class="pickup-close" onClick={() => dismissPickup()}>
            Close — leave the rest
          </button>
        </div>
      </div>
    </Show>
  );
}

/** Mount the per-hex pickup modal into `root`. */
export function mountPickup(root: HTMLElement, actions: PickupActions): void {
  render(() => <PickupModal actions={actions} />, root);
}
