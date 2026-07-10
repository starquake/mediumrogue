import { createSignal } from "solid-js";

import type { ItemView } from "../protocol.gen";

// My inventory, refreshed each turn from the bundle by main.ts.
const [inventory, setInventorySignal] = createSignal<ItemView[]>([]);

export { inventory };

export function setInventory(items: ItemView[]): void {
  setInventorySignal(items);
}
