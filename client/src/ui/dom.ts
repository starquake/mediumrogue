// Shared DOM lookup helpers: the HTML-over-canvas UI (the HUD, the panels, the
// turn timer) reaches into index.html by id, and every reach is fail-loud — a
// missing element is a build/markup bug that should throw at wire-up, never
// silently no-op later. Previously mustGet was copy-pasted in main.ts and
// ui/timer.ts; one home keeps them identical.

/** The element with the given id, or throws — index.html is the contract. */
export function mustGet(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (el === null) {
    throw new Error(`required element #${id} missing from index.html`);
  }

  return el;
}

/** The first element matching `selector` under `root`, or throws. */
export function mustQuery(root: HTMLElement, selector: string): HTMLElement {
  const el = root.querySelector<HTMLElement>(selector);
  if (el === null) {
    throw new Error(`required element ${selector} missing under #${root.id}`);
  }

  return el;
}
