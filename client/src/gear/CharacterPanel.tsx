import { For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import {
  ItemTypeAmulet,
  ItemTypeBody,
  ItemTypeConsumable,
  ItemTypeFeet,
  ItemTypeHands,
  ItemTypeHead,
  ItemTypeRing,
} from "../protocol.gen";
import type { BackpackEntry, SlotItem } from "./store";
import { backpack, equipped, panelOpen, pending, slotLabel, weaponSlots } from "./store";

/** Callbacks the panel needs — each posts the matching inventory intent. */
export interface CharacterActions {
  equip: (itemId: number) => void;
  unequip: (itemId: number) => void;
  drop: (itemId: number) => void;
  drink: (itemId: number) => void;
  /** Close the panel (its own × affordance; the `i` key / HUD button also toggle it). */
  close: () => void;
}

// The six universal body slots, positioned on the Vitruvian paper-doll per
// the approved mockup. The two class-shaped weapon slots (weap1/weap2) are
// added around the body from weaponSlots().
const BODY_SLOTS: { type: string; cls: string }[] = [
  { type: ItemTypeHead, cls: "head" },
  { type: ItemTypeHands, cls: "hands" },
  { type: ItemTypeRing, cls: "ring" },
  { type: ItemTypeAmulet, cls: "amulet" },
  { type: ItemTypeBody, cls: "body" },
  { type: ItemTypeFeet, cls: "feet" },
];

// isPending mirrors an item's in-flight action mark.
function isPending(id: number): boolean {
  return pending().has(id);
}

function HexSlot(props: { type: string; cls: string; actions: CharacterActions }): JSXElement {
  const item = (): SlotItem | undefined => equipped()[props.type];

  return (
    <button
      type="button"
      class={`hex ${props.cls}`}
      classList={{ filled: item() !== undefined }}
      data-slot={props.type}
      disabled={item() === undefined || isPending(item()!.id)}
      onClick={() => {
        const it = item();
        if (it !== undefined) props.actions.unequip(it.id);
      }}
    >
      <span class="slotname">{slotLabel(props.type)}</span>
      <Show when={item() !== undefined} fallback={<span class="empty">—</span>}>
        <span class="itemname">{isPending(item()!.id) ? "…" : item()!.name}</span>
      </Show>
    </button>
  );
}

function BackpackCell(props: { entry: BackpackEntry | null; actions: CharacterActions }): JSXElement {
  const entry = (): BackpackEntry | null => props.entry;
  const isConsumable = (): boolean => entry()?.type === ItemTypeConsumable;

  const cellClick = (): void => {
    const e = entry();
    if (e === null) return;
    if (isConsumable()) props.actions.drink(e.id);
    else props.actions.equip(e.id);
  };

  return (
    <div class="cell" classList={{ filled: entry() !== null }}>
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
  // The two class-shaped weapon slots flank the body: index 0 (close-ish) on
  // the left, index 1 (ranged-ish) on the right, labels per class.
  const weap1 = (): string => weaponSlots()[0];
  const weap2 = (): string => weaponSlots()[1];

  return (
    <Show when={panelOpen()}>
      <div id="character-panel">
        <div class="title panel-head">
          <span>Character</span>
          <button type="button" class="panel-close" title="close (i)" onClick={() => props.actions.close()}>
            ×
          </button>
        </div>
        <div class="doll">
          <HexSlot type={weap1()} cls="weap1" actions={props.actions} />
          <HexSlot type={weap2()} cls="weap2" actions={props.actions} />
          <For each={BODY_SLOTS}>{(s) => <HexSlot type={s.type} cls={s.cls} actions={props.actions} />}</For>
        </div>
        <div class="title" style="margin-top:1.1rem">
          Backpack
        </div>
        <div class="backpack">
          <For each={backpack()}>{(entry) => <BackpackCell entry={entry} actions={props.actions} />}</For>
        </div>
      </div>
    </Show>
  );
}

/** Mount the character/inventory panel into `root`. */
export function mountCharacter(root: HTMLElement, actions: CharacterActions): void {
  render(() => <CharacterPanel actions={actions} />, root);
}
