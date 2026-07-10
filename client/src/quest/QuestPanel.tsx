import { For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import type { QuestView } from "../protocol.gen";
import { board, myQuest } from "./store";

// objective renders a quest's goal + progress: "slay 3 — 2/3" or "reach (9, -4)".
function objective(q: QuestView): string {
  if (q.kind === "kill") {
    return `slay ${q.targetN} — ${q.progress}/${q.targetN}`;
  }

  return `reach (${q.goalHex.q}, ${q.goalHex.r})`;
}

function QuestPanel(): JSXElement {
  const available = (): QuestView[] => board().filter((q) => q.state === "available");

  return (
    <div id="quest-panel">
      <Show when={myQuest()}>
        {(q) => (
          <div id="quest-mine">
            <div class="quest-title">
              #{q().id} {q().name}
            </div>
            <div class="quest-objective">
              {objective(q())} · {q().rewardXp} XP
            </div>
          </div>
        )}
      </Show>
      <Show when={available().length > 0}>
        <div id="quest-board">
          <div id="quest-board-title">Quest board — /quest &lt;id&gt;</div>
          <For each={available()}>
            {(q) => (
              <div class="quest-row">
                #{q.id} {q.name}: {objective(q)} · {q.rewardXp} XP
              </div>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}

/** Mount the quest panel into `root`. Keeps JSX in this .tsx file. */
export function mountQuests(root: HTMLElement): void {
  render(() => <QuestPanel />, root);
}
