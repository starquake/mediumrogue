import { createSignal, For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { ItemTypeWeapon, SlotMainHand, SlotOffHand } from "../protocol.gen";
import type { ItemStats, SlotItem } from "./store";
import { equipped, SLOT_LABELS, targetSlotFor } from "./store";

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
  // Leave room for a comparison stack (candidate + up to two equipped weapons).
  const y = Math.min(r.top, Math.max(8, window.innerHeight - 380));
  setHover({ item, x, y });
}

/** Hide the stat tooltip (on mouse-leave). */
export function hideHover(): void {
  setHover(null);
}

// A text stat line (the damage type, #92) — StatLine renders numbers.
function StatText(props: { label: string; value: string }): JSXElement {
  return (
    <div class="tt-row">
      <span class="tt-label">{props.label}</span>
      <span class="tt-val">{props.value}</span>
    </div>
  );
}

function StatLine(props: { label: string; value: number }): JSXElement {
  return (
    <div class="tt-row">
      <span class="tt-label">{props.label}</span>
      <span class="tt-val">{props.value}</span>
    </div>
  );
}

// One item's own stat block — the hovered candidate, or an equipped weapon shown
// beside it for comparison (slot set → labelled "equipped · Main Hand").
function StatBlock(props: { item: ItemStats & { name: string }; slot?: string }): JSXElement {
  const it = (): ItemStats & { name: string } => props.item;
  const hasCombat = (): boolean => it().damage > 0 || it().rangeHex > 0 || it().aoeRadius > 0;
  return (
    <div class="stat-tip">
      <div class="tt-name">{it().name}</div>
      <Show when={props.slot !== undefined}>
        <div class="tt-cmp">equipped · {SLOT_LABELS[props.slot!]}</div>
      </Show>
      <Show
        when={hasCombat()}
        fallback={
          <Show when={it().desc === ""}>
            <div class="tt-none">No combat stats.</div>
          </Show>
        }
      >
        <StatLine label="Damage" value={it().damage} />
        <Show when={it().damageType !== ""}>
          <StatText label="Type" value={it().damageType} />
        </Show>
        <Show when={it().rangeHex > 0}>
          <StatLine label="Range" value={it().rangeHex} />
        </Show>
        <Show when={it().aoeRadius > 0}>
          <StatLine label="AoE" value={it().aoeRadius} />
        </Show>
      </Show>
      <Show when={it().desc !== ""}>
        <div class="tt-effect">{it().desc}</div>
      </Show>
      <Show when={it().flavor !== ""}>
        <div class="tt-flavor">{it().flavor}</div>
      </Show>
    </div>
  );
}

function StatTooltip(): JSXElement {
  return (
    <Show when={hover()}>
      {(h) => {
        const item = (): HoverItem => h().item;
        const isEquipped = (): boolean => Object.values(equipped()).some((c) => c.id === item().id);
        // A candidate weapon is shown alongside BOTH equipped hands (dual-wield —
        // two 1H weapons, or a 2H weapon that replaces both); other gear alongside
        // its one target slot. Skipped when hovering something already equipped
        // (you're inspecting your own gear, not weighing a swap).
        const compares = (): { item: SlotItem; slot: string }[] => {
          if (isEquipped()) {
            return [];
          }
          const it = item();
          const slots = it.type === ItemTypeWeapon ? [SlotMainHand, SlotOffHand] : [targetSlotFor(it)];
          const eq = equipped();
          const out: { item: SlotItem; slot: string }[] = [];
          for (const s of slots) {
            const c = eq[s];
            if (c !== undefined && c.id !== it.id) {
              out.push({ item: c, slot: s });
            }
          }
          return out;
        };
        return (
          <div class="stat-tip-stack" style={{ left: `${h().x}px`, top: `${h().y}px` }}>
            <StatBlock item={item()} />
            <For each={compares()}>{(c) => <StatBlock item={c.item} slot={c.slot} />}</For>
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
