import { Index, Show } from "solid-js";
import type { JSXElement } from "solid-js";
import { render } from "solid-js/web";

import { sendChat } from "../chat/store";

import { party, pendingInvite } from "./store";

// InvitePrompt (#385): the invite you have to answer, as a panel rather than a
// chat sentence you have to notice and type at.
//
// PASSIVE, not modal (maintainer's call, approved from the mockup): it sits in
// the roster's slot and never takes focus or covers the board, because an
// invite can arrive while you are mid-fight. The accent border is the only
// thing marking it as the one panel on screen you did not open yourself.
//
// There is no countdown: invites do not expire (maintainer's call 2026-08-08 —
// a real /decline makes a TTL unnecessary, and a turn-counted one would lapse
// FASTEST for the player who is mid-fight and least able to answer, since the
// turn counter ticks for every combat bubble too).

/** Answer the pending invite. Both verbs are ordinary chat commands. */
async function answer(accept: boolean): Promise<void> {
  await sendChat(accept ? "/accept" : "/decline");
}

function InvitePrompt(): JSXElement {
  return (
    <Show when={pendingInvite()}>
      {(invite) => (
        <div id="invite-prompt">
          <div class="invite-title">Party invite</div>

          <div class="invite-line">
            <strong>{invite().inviterName}</strong> invited you to a party.
          </div>

          {/* Empty for a solo inviter — accepting is what creates the party,
              so there is no roster to show yet. */}
          <Show when={(invite().members ?? []).length > 0}>
            <div class="invite-roster">
              {/* <Index>, not <For>: the client rebuilds state from a full
                  snapshot every turn, minting fresh objects, so <For> keys by
                  a reference that changes every bundle and remounts the rows —
                  dropping in-flight clicks on the buttons below. */}
              <Index each={invite().members ?? []}>
                {(member) => <div class="invite-member">{member().name}</div>}
              </Index>
            </div>
          </Show>

          {/* Accepting already left your old party before #385; it just did it
              invisibly. Shown only when it actually costs you something. */}
          <Show when={party().length > 0}>
            <div class="invite-warning">This will leave your current party.</div>
          </Show>

          <div class="invite-buttons">
            <button type="button" onClick={() => void answer(true)}>
              Accept <span class="invite-key">Y</span>
            </button>
            <button type="button" onClick={() => void answer(false)}>
              Decline <span class="invite-key">N</span>
            </button>
          </div>
        </div>
      )}
    </Show>
  );
}

/**
 * Mount the invite prompt into `root`. Mounted BEFORE the roster so it stacks
 * above it in the same corner, as approved in the mockup.
 */
export function mountInvitePrompt(root: HTMLElement): void {
  render(() => <InvitePrompt />, root);
}

/**
 * Answer the pending invite from the keyboard (Y/N). Returns whether there was
 * one to answer, so the key handler can fall through when no prompt is up.
 */
export function answerInvite(accept: boolean): boolean {
  if (pendingInvite() === null) {
    return false;
  }

  void answer(accept);

  return true;
}
