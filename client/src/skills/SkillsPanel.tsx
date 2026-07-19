import { Index, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import type { SkillView } from "../protocol.gen";
import { panelOpen, points, skills, TREES } from "./store";

// The near-sighted skills panel (#124, mockup approved 2026-07-18): you see
// what you know and what you can learn next, and nothing else. There is no
// locked-skill rendering path here BY DESIGN — the server sends only learned
// and currently-learnable rows, so a locked skill cannot leak even if this
// component were wrong.

/** Rows for one tree, in the order the server sent them (registry order). */
function rowsFor(tree: string): SkillView[] {
  return skills().filter((s) => s.tree === tree);
}

function SkillsPanel(props: { learn: (id: string) => void }): JSXElement {
  return (
    <Show when={panelOpen()}>
      <div id="skills-panel">
        <div class="skills-head">
          <span class="skills-title">Skills</span>
          <span class="skills-points" classList={{ none: points() === 0 }}>
            {points()} {points() === 1 ? "point" : "points"}
          </span>
        </div>

        {/* <Index>, never <For>: these rows are rebuilt from a full turn
            bundle every turn, so <For>'s reference keying would remount the
            DOM each time and eat an in-flight Learn click. */}
        <Index each={TREES}>
          {(tree) => (
            <div class="skill-tree">
              <div class="skill-tree-name">{tree().label}</div>
              <Show
                when={rowsFor(tree().id).length > 0}
                fallback={<div class="skill-empty">nothing available yet</div>}
              >
                <Index each={rowsFor(tree().id)}>
                  {(skill) => (
                    <div class="skill-row" classList={{ learned: skill().learned }}>
                      <span class="skill-mark">{skill().learned ? "✓" : "●"}</span>
                      <span class="skill-body">
                        <span class="skill-name">{skill().name}</span>
                        <span class="skill-desc">{skill().desc}</span>
                      </span>
                      <Show
                        when={!skill().learned}
                        fallback={<span class="skill-tag">Learned</span>}
                      >
                        <button
                          class="skill-learn"
                          disabled={points() === 0}
                          onClick={() => props.learn(skill().id)}
                        >
                          Learn
                        </button>
                      </Show>
                    </div>
                  )}
                </Index>
              </Show>
            </div>
          )}
        </Index>

        <div class="skills-foot">
          You see what you know, and what you can learn next. The rest of each tree stays hidden until you get there.
        </div>
      </div>
    </Show>
  );
}

/** Mount the skills panel into `root`. Keeps JSX in this .tsx file. */
export function mountSkills(root: HTMLElement, learn: (id: string) => void): void {
  render(() => <SkillsPanel learn={learn} />, root);
}
