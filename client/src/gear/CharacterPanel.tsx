import { createSignal, For, Index, Show } from "solid-js";
import type { Accessor, JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { ItemTypeConsumable, SlotOffHand } from "../protocol.gen";
import type { BackpackEntry, ItemStats, SlotItem } from "./store";
import { backpack, equipped, offHandLocked, panelOpen, pending, SLOT_LABELS, SLOT_ORDER, targetSlotFor } from "./store";

/** Callbacks the panel needs — each posts the matching inventory intent. */
export interface CharacterActions {
  equip: (itemId: number) => void;
  unequip: (itemId: number) => void;
  drop: (itemId: number) => void;
  drink: (itemId: number) => void;
  /** Close the panel (its own × affordance; the `i` key / HUD button also toggle it). */
  close: () => void;
}

// positionClass is a slot's CSS position class on the paper-doll (the
// approved ARPG mockup's geometry — index.html's .hex.helmet, .hex.mainhand,
// …): the slot name with its hyphen stripped (main-hand -> mainhand, the
// mockup's class names), all other slots unchanged. Derived from the slot
// name rather than kept as a parallel table, so SLOT_ORDER (store.ts) stays
// the single source of slot names; only index.html's position rules pair
// with it.
function positionClass(slot: string): string {
  return slot.replace("-", "");
}

// isPending mirrors an item's in-flight action mark.
function isPending(id: number): boolean {
  return pending().has(id);
}

// --- Hover stat tooltip -----------------------------------------------------
// Hovering an item (an equipped hex or a backpack cell) shows its stats and,
// when a DIFFERENT item is equipped in the same slot, the delta against it —
// so you can weigh a pickup before equipping it.
type HoverItem = SlotItem | BackpackEntry;
const [hover, setHover] = createSignal<{ item: HoverItem; x: number; y: number } | null>(null);

function showHover(item: HoverItem, el: HTMLElement): void {
  const r = el.getBoundingClientRect();
  const width = 224; // matches .stat-tip; flip to the anchor's left near the edge
  const x = r.right + 10 + width > window.innerWidth ? r.left - 10 - width : r.right + 10;
  const y = Math.min(r.top, window.innerHeight - 240);
  setHover({ item, x, y });
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
        // backpack weapon compares against the hand targetSlotFor picks
        // (main if free/two-handed, else off if free, else main), mirroring
        // the server's weaponTargetSlot placement — unless that IS the
        // hovered item (hovering the equipped hex itself → nothing to
        // compare against).
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

// HexSlot renders one of the eight paper-doll slots. The off-hand hex greys
// out and shows "two-handed grip" (the approved mockup) while offHandLocked()
// — a two-handed main-hand weapon locks it, so it's never clickable then (the
// item is always undefined in that state too — toggleEquip clears off-hand
// before a 2H equip — but `locked` is checked explicitly so the disabled/grey
// state doesn't depend on that implementation detail holding).
function HexSlot(props: { slot: string; cls: string; actions: CharacterActions }): JSXElement {
  const item = (): SlotItem | undefined => equipped()[props.slot];
  const locked = (): boolean => props.slot === SlotOffHand && offHandLocked();

  return (
    <button
      type="button"
      class={`hex ${props.cls}`}
      classList={{ filled: item() !== undefined, greyed: locked() }}
      data-slot={props.slot}
      disabled={locked() || item() === undefined || isPending(item()!.id)}
      onClick={() => {
        const it = item();
        if (it !== undefined) props.actions.unequip(it.id);
      }}
      onMouseEnter={(e) => {
        const it = item();
        if (it !== undefined) showHover(it, e.currentTarget);
      }}
      onMouseLeave={() => setHover(null)}
    >
      <span class="slotname">{SLOT_LABELS[props.slot]}</span>
      <Show
        when={!locked()}
        fallback={<span class="ghost">two-handed grip</span>}
      >
        <Show when={item() !== undefined} fallback={<span class="empty">—</span>}>
          <span class="itemname">{isPending(item()!.id) ? "…" : item()!.name}</span>
        </Show>
      </Show>
    </button>
  );
}

// BackpackCell takes an ACCESSOR (not a plain value) because it's rendered
// under <Index>: main.ts rebuilds a fresh backpack array every turn bundle
// (250ms in e2e), so <For> — which keys by reference — would remount every
// cell's DOM every bundle, detaching the equip/drop buttons mid-click
// ("element is not stable"). <Index> keys by POSITION: the cell DOM is
// stable across bundles, only its content updates via this accessor. (The
// old GearPanel used the same Index-not-For discipline for the same reason.)
function BackpackCell(props: { entry: Accessor<BackpackEntry | null>; actions: CharacterActions }): JSXElement {
  const entry = (): BackpackEntry | null => props.entry();
  const isConsumable = (): boolean => entry()?.type === ItemTypeConsumable;

  const cellClick = (): void => {
    const e = entry();
    if (e === null) return;
    if (isConsumable()) props.actions.drink(e.id);
    else props.actions.equip(e.id);
  };

  return (
    <div
      class="cell"
      classList={{ filled: entry() !== null }}
      onMouseEnter={(e) => {
        const en = entry();
        if (en !== null) showHover(en, e.currentTarget);
      }}
      onMouseLeave={() => setHover(null)}
    >
      <Show when={entry() !== null} fallback={<span class="empty">—</span>}>
        <button
          type="button"
          class="cell-use"
          data-def={entry()!.defId}
          disabled={isPending(entry()!.id)}
          title={isConsumable() ? "drink" : "equip"}
          onClick={cellClick}
        >
          {isPending(entry()!.id) ? "…" : entry()!.name}
        </button>
        <Show when={entry()!.count > 1}>
          <span class="count">×{entry()!.count}</span>
        </Show>
        <button
          type="button"
          class="drop"
          disabled={isPending(entry()!.id)}
          onClick={() => props.actions.drop(entry()!.id)}
        >
          drop
        </button>
      </Show>
    </div>
  );
}

function CharacterPanel(props: { actions: CharacterActions }): JSXElement {
  return (
    <Show when={panelOpen()}>
      <div id="character-panel">
        <div class="title panel-head">
          <span>Character</span>
          <span class="keyhint">C / I toggles · Esc closes</span>
          <button type="button" class="panel-close" title="close (C / I / Esc)" onClick={() => props.actions.close()}>
            ×
          </button>
        </div>
        <div class="doll">
          <For each={SLOT_ORDER}>
            {(slot) => <HexSlot slot={slot} cls={positionClass(slot)} actions={props.actions} />}
          </For>
        </div>
        <div class="title" style="margin-top:1.1rem">
          Backpack
        </div>
        <div class="backpack">
          <Index each={backpack()}>{(entry) => <BackpackCell entry={entry} actions={props.actions} />}</Index>
        </div>
        <StatTooltip />
      </div>
    </Show>
  );
}

/** Mount the character/inventory panel into `root`. */
export function mountCharacter(root: HTMLElement, actions: CharacterActions): void {
  render(() => <CharacterPanel actions={actions} />, root);
}
