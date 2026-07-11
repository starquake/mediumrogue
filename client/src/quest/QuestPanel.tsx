import { For, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import type { QuestView } from "../protocol.gen";
import { board, myQuests } from "./store";

// objective renders a quest's goal: "slay 3" or "reach (9, -4)" — the static
// description used on untaken board rows (no progress: an untaken quest never
// moves, and showing "0/3" reads as a stuck tracker).
function objective(q: QuestView): string {
  if (q.kind === "kill") {
    return `slay ${q.targetN}`;
  }

  return `reach (${q.goalHex.q}, ${q.goalHex.r})`;
}

// myObjective renders MY quest's live goal as a countdown — "slay 3 — 2 left"
// — so the number visibly goes DOWN as monsters fall (the natural reading of
// "how many are left to kill"), rather than a "1/3" fraction that keeps the
// target constant.
function myObjective(q: QuestView): string {
  if (q.kind === "kill") {
    const left = q.targetN - q.progress;

    return `slay ${q.targetN} — ${left} left`;
  }

  return `reach (${q.goalHex.q}, ${q.goalHex.r})`;
}

function QuestPanel(): JSXElement {
  const available = (): QuestView[] => board().filter((q) => q.state === "available");

  return (
    <div id="quest-panel">
      {/* item 14, playtest batch 2: I may hold several quests at once (all my
          personal quests, plus my party's, if any) — one row each, instead
          of a single implicit "my quest". */}
      <Show when={myQuests().length > 0}>
        <div id="quest-mine">
          <For each={myQuests()}>
            {(q) => (
              <div class="quest-mine-row">
                <div class="quest-title">
                  #{q.id} {q.name}
                </div>
                <div class="quest-objective">
                  {myObjective(q)} · {q.rewardXp} XP
                </div>
              </div>
            )}
          </For>
        </div>
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
