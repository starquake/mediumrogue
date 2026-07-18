import { createSignal, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import type { ItemStats, SlotItem } from "./store";
import { equipped, targetSlotFor } from "./store";

// A hovered item is anything with an id/name/type carrying combat stats — an
// equipped slot item, a backpack entry, or a ground item in the pickup modal
// (#139). The tooltip reads only these fields, so all three qualify.
export type HoverItem = ItemStats & { id: number; name: string; type: string };

// One shared hover signal drives ONE globally-mounted tooltip (mountStatTooltip),
// so the character panel and the pickup modal show the same stats-on-hover — and
// the pickup modal keeps working when the panel is closed (it isn't rendered
// inside either panel's tree).
const [hover, setHover] = createSignal<{ item: HoverItem; x: number; y: number } | null>(null);

/** Show the stat tooltip for `item`, anchored beside `el`. */
export function showHover(item: HoverItem, el: HTMLElement): void {
  const r = el.getBoundingClientRect();
  const width = 224; // matches .stat-tip; flip to the anchor's left near the edge
  const x = r.right + 10 + width > window.innerWidth ? r.left - 10 - width : r.right + 10;
  const y = Math.min(r.top, window.innerHeight - 240);
  setHover({ item, x, y });
}

/** Hide the stat tooltip (on mouse-leave). */
export function hideHover(): void {
  setHover(null);
}

function StatRow(props: { label: string; value: number; delta: number | undefined }): JSXElement {
  return (
    <div class="tt-row">
      <span class="tt-label">{props.label}</span>
      <span class="tt-val">
        {props.value}
        <Show when={props.delta !== undefined && props.delta !== 0}>
          <span class="tt-delta" classList={{ up: props.delta! > 0, down: props.delta! < 0 }}>
            {` (${props.delta! > 0 ? "+" : ""}${props.delta})`}
          </span>
        </Show>
      </span>
    </div>
  );
}

function StatTooltip(): JSXElement {
  return (
    <Show when={hover()}>
      {(h) => {
        const item = (): HoverItem => h().item;
        // The item in the slot this item occupies OR WOULD occupy — a
        // backpack/ground weapon compares against the hand targetSlotFor picks
        // (main if free/two-handed, else off if free, else main), mirroring
        // the server's weaponTargetSlot placement — unless that IS the
        // hovered item (hovering the equipped hex itself → nothing to
        // compare against). A ground item's id never matches an equipped one,
        // so for the pickup modal this is always the "vs what I'm holding".
        const current = (): SlotItem | undefined => {
          const c = equipped()[targetSlotFor(item())];
          return c !== undefined && c.id !== item().id ? c : undefined;
        };
        const delta = (pick: (s: ItemStats) => number): number | undefined => {
          const c = current();
          return c !== undefined ? pick(item()) - pick(c) : undefined;
        };
        const hasCombat = (): boolean =>
          item().damage > 0 || item().rangeHex > 0 || item().aoeRadius > 0 || (current()?.damage ?? 0) > 0;
        return (
          <div class="stat-tip" style={{ left: `${h().x}px`, top: `${h().y}px` }}>
            <div class="tt-name">{item().name}</div>
            <Show when={current() !== undefined}>
              <div class="tt-cmp">vs equipped: {current()!.name}</div>
            </Show>
            <Show
              when={hasCombat()}
              fallback={
                <Show when={item().desc === ""}>
                  <div class="tt-none">No combat stats.</div>
                </Show>
              }
            >
              <StatRow label="Damage" value={item().damage} delta={delta((s) => s.damage)} />
              <Show when={item().rangeHex > 0 || (current()?.rangeHex ?? 0) > 0}>
                <StatRow label="Range" value={item().rangeHex} delta={delta((s) => s.rangeHex)} />
              </Show>
              <Show when={item().aoeRadius > 0 || (current()?.aoeRadius ?? 0) > 0}>
                <StatRow label="AoE" value={item().aoeRadius} delta={delta((s) => s.aoeRadius)} />
              </Show>
            </Show>
            <Show when={item().desc !== ""}>
              <div class="tt-effect">{item().desc}</div>
            </Show>
            <Show when={item().flavor !== ""}>
              <div class="tt-flavor">{item().flavor}</div>
            </Show>
          </div>
        );
      }}
    </Show>
  );
}

/** Mount the single shared stat tooltip into `root` (a fixed-position overlay,
 *  so it can live anywhere in the DOM). */
export function mountStatTooltip(root: HTMLElement): void {
  render(() => <StatTooltip />, root);
}
