import { createSignal, For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { ItemTypeWeapon, SlotMainHand, SlotOffHand } from "../protocol.gen";
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

function CmpDelta(props: { label: string; value: number }): JSXElement {
  return (
    <span class="tt-cmp-delta" classList={{ up: props.value > 0, down: props.value < 0 }}>
      {`${props.value > 0 ? "+" : ""}${props.value} ${props.label}`}
    </span>
  );
}

function StatTooltip(): JSXElement {
  return (
    <Show when={hover()}>
      {(h) => {
        const item = (): HoverItem => h().item;
        // What this item would be weighed against. A WEAPON compares against
        // BOTH hands — you can dual-wield two 1H weapons, and a 2H weapon
        // replaces both — while a shield/armor/jewelry compares against its one
        // slot (targetSlotFor). Never against itself (hovering an equipped hex).
        const targets = (): SlotItem[] => {
          const it = item();
          const slots = it.type === ItemTypeWeapon ? [SlotMainHand, SlotOffHand] : [targetSlotFor(it)];
          const eq = equipped();
          const out: SlotItem[] = [];
          for (const s of slots) {
            const c = eq[s];
            if (c !== undefined && c.id !== it.id) {
              out.push(c);
            }
          }
          return out;
        };
        const hasCombat = (): boolean =>
          item().damage > 0 || item().rangeHex > 0 || item().aoeRadius > 0 || targets().some((c) => c.damage > 0);
        const showRange = (): boolean => item().rangeHex > 0 || targets().some((c) => c.rangeHex > 0);
        const showAoe = (): boolean => item().aoeRadius > 0 || targets().some((c) => c.aoeRadius > 0);
        // With ONE thing to compare against, keep the inline (+delta) format;
        // with two (dual-wield), the deltas move to a per-weapon block below the
        // plain stats — you can't put two deltas on one stat row.
        const solo = (): SlotItem | undefined => (targets().length === 1 ? targets()[0] : undefined);
        const delta = (pick: (s: ItemStats) => number): number | undefined => {
          const c = solo();
          return c !== undefined ? pick(item()) - pick(c) : undefined;
        };
        return (
          <div class="stat-tip" style={{ left: `${h().x}px`, top: `${h().y}px` }}>
            <div class="tt-name">{item().name}</div>
            <Show when={solo() !== undefined}>
              <div class="tt-cmp">vs equipped: {solo()!.name}</div>
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
              <Show when={showRange()}>
                <StatRow label="Range" value={item().rangeHex} delta={delta((s) => s.rangeHex)} />
              </Show>
              <Show when={showAoe()}>
                <StatRow label="AoE" value={item().aoeRadius} delta={delta((s) => s.aoeRadius)} />
              </Show>
              <Show when={targets().length > 1}>
                <div class="tt-cmp">vs equipped</div>
                <For each={targets()}>
                  {(c) => (
                    <div class="tt-cmp-row">
                      <span class="tt-cmp-name">{c.name}</span>
                      <span class="tt-cmp-deltas">
                        <CmpDelta label="dmg" value={item().damage - c.damage} />
                        <Show when={showRange()}>
                          <CmpDelta label="rng" value={item().rangeHex - c.rangeHex} />
                        </Show>
                        <Show when={showAoe()}>
                          <CmpDelta label="aoe" value={item().aoeRadius - c.aoeRadius} />
                        </Show>
                      </span>
                    </div>
                  )}
                </For>
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
