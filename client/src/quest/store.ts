import { createSignal } from "solid-js";

import type { QuestView } from "../protocol.gen";

// The whole board plus MY active quests (item 14, playtest batch 2: a
// player may hold several personal quests concurrently, plus at most one
// party quest — so this is a list, not a single quest). main.ts refreshes
// both each turn from the bundle.
const [board, setBoardSignal] = createSignal<QuestView[]>([]);
const [myQuests, setMyQuestsSignal] = createSignal<QuestView[]>([]);

export { board, myQuests };

export function setQuests(quests: QuestView[], myEntityID: number, myPartyID: number): void {
  setBoardSignal(quests);
  setMyQuestsSignal(
    quests.filter(
      (q) =>
        q.state === "taken" &&
        (q.holderEntityId === myEntityID || (myPartyID !== 0 && q.holderPartyId === myPartyID)),
    ),
  );
}
