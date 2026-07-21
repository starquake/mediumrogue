// Ambient `window.game` type for every e2e spec. Before this file each spec
// re-declared the same `declare global { interface Window { game: GameDebug } }`
// block (27 copies); tsconfig includes `e2e`, so this one ambient augmentation
// types `window.game` across the whole suite. NOTE (#213): the GameDebug shape
// lives in ../src/main and is owned by the client; this file only re-exports its
// existing declaration — it must not change the window.game surface.
import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}
