import { describe, expect, test } from "vitest";

import type { PartyInviteView } from "../protocol.gen";

import { pendingInvite, setPendingInvite } from "./store";

// store.test.ts covers the invite signal's own contract (#385). The panel's
// RENDERING is covered end-to-end in client/e2e/party.spec.ts, which drives
// two real browser contexts; what a unit test adds is the clearing behaviour,
// where the failure mode is a prompt that lingers after it has been answered
// and invites a click on an invite that no longer exists.

function invite(overrides: Partial<PartyInviteView> = {}): PartyInviteView {
  return { inviterId: 7, inviterName: "alice", members: [], ...overrides };
}

describe("pendingInvite", () => {
  test("starts empty", () => {
    setPendingInvite(null);
    expect(pendingInvite()).toBeNull();
  });

  test("holds the invite it is given", () => {
    setPendingInvite(invite({ inviterName: "starquake" }));

    expect(pendingInvite()?.inviterName).toBe("starquake");
  });

  test("clears when the bundle stops carrying one", () => {
    setPendingInvite(invite());
    setPendingInvite(null);

    // The bundle is a full snapshot every turn, so "no invite" arrives as an
    // absent field rather than an explicit clear — the prompt has to vanish on
    // that, not wait for something to tell it to.
    expect(pendingInvite()).toBeNull();
  });

  test("carries the inviter's party roster", () => {
    setPendingInvite(
      invite({
        members: [
          { id: 1, name: "alice" },
          { id: 2, name: "bob" },
        ],
      }),
    );

    expect(pendingInvite()?.members?.map((m) => m.name)).toEqual(["alice", "bob"]);
  });

  test("a solo inviter carries no roster", () => {
    setPendingInvite(invite({ members: [] }));

    expect(pendingInvite()?.members).toEqual([]);
  });
});
