import { createSignal } from "solid-js";

import type { QuestView } from "../protocol.gen";

// The whole board plus my active quest (mine personally, or my party's).
// main.ts refreshes both each turn from the bundle.
const [board, setBoardSignal] = createSignal<QuestView[]>([]);
const [myQuest, setMyQuestSignal] = createSignal<QuestView | null>(null);

export { board, myQuest };

export function setQuests(quests: QuestView[], myEntityID: number, myPartyID: number): void {
  setBoardSignal(quests);
  setMyQuestSignal(
    quests.find(
      (q) =>
        q.state === "taken" &&
        (q.holderEntityId === myEntityID || (myPartyID !== 0 && q.holderPartyId === myPartyID)),
    ) ?? null,
  );
}
