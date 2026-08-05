# Skill tree design principles

The governing design record for the skill system. Written by @starquake on
2026-07-12 (originally issue #61), with decisions appended as they were settled.
Implemented by #124 (skill system v1); moved here and the issue closed on
2026-08-05, because a reference record belongs in `docs/`, not in an open ticket.

**The numbered principles below are the maintainer's original text, unedited.**
Everything after them is the decision log and the implementation status.

---

1. The skill tree consists of 3 parts; Class, Adventure, Survival
2. The class tree is unique to the class (the skill therein, not per se)
3. The adventure tree is the same for all classes
4. The survival tree is the same for all classes
5. Progression in one tree (or the lack thereof) may never block progression in another tree. So, a Mage skill in the Mage Skill Tree may never have the prerequisite of a skill from another tree than the Mage tree.
6. The Class tree deals with class specific buffs and new mechanics
7. The Adventure tree deals with the map, the surroundings, the loot, fog of war
8. The Survival tree deals with (non-magical) healing, camp making, resource gathering, etc.
9. A skill can not be in multiple trees (of the same character) at the same time
10. Have skills be a prerequisite for a future skill. Combine WITHIN the tree (like, have the skill "forager" and "scout" to unlock "wayfarer")
11. Only used combined prerequisites when it's a real boost or capstone. Put thought in WHY
12. Never have "being a certain level" be a prerequisite
13. Promote branching of the tree
14. If different skills buff the same stat, it stacks. If the effects are expressed in percentages, add all the percentages together, do not compound the percentages. 


When in doubt ask yourself this question:
Will this help the character to traverse the map and/or find objects? yes: Adventure no: Survival
Or
Will this help the character survive longer, respawn quicker, or create items from (natural) resources? yes: Survival no: Adventure

Class specific skills should be very clear to which class they belong.


Question:
Do we want a visual tree, and can you look ahead? Or do we want the the possible skill choices to populate a list if all requirements have been met, and you're essentially choosing your skills in the moment in stead of planning for that one capstone you've seen in the distance?
I propose "near sighted" levelling; it makes for a good experience to stumble upon cool capstones that are (if all goes well) in line with your previous choices. It improves replayability. It counters the meta of min-maxing.



---

## Decided so far (2026-07-13 — appended, original text above untouched)
- **The 3-tree structure (Class / Adventure / Survival) is the governing skill model** (design-decisions Q10) — the master plan's earlier class-agnostic life skills (First Aid, Make Camp) become the Adventure/Survival trees; the Class tree delivers #56's class-identity-via-skills.
- **Principle 14 is engine-endorsed** (Q8): percentages **add** within one event's fold; stages (deal-damage → take-damage → crit-check) compose across events, so crit/reduction stay true multipliers.
- **Level-up = one bankable skill point** (Q4, recorded on #60).
- **Open:** the principle-5 scoping for subclass capstone gates awaits @NGB1024's nod (Q11 — see the comment below).
- **Settled 2026-07-18 (#124 Q7): NEAR-SIGHTED.** The original question at the top of this issue — visual look-ahead tree vs a list of what you can learn now — is decided in favour of the proposal made there: you see your learned skills and the ones you can learn next, and nothing else. No locked skill renders at all, and it is enforced server-side so the client cannot leak the tree. Mockup approved on #124.
- **ARPG note (2026-07-18):** principle 14 (percentages add, never compound) is the grammar the rest of the content follows too — see #154, where flat `−N` mitigation is being reconsidered precisely because it *doesn't* behave like principle 14 does.

---

## Q11 resolved — principle 5 versus subclass gating

*(@NGB1024, 2026-07-14, answering the scoping question raised on the issue)*

> Agreed. The intent is for the main 3 trees (class, survival and adventure) to
> not block each other's progress. The subclass tree is intentionally gated
> behind main-class tree progress.
>
> how to implement the subclasses (as a skill choice in the main skill tree that
> opens up a new tree, or as a subclass (very big) branch of the class tree, or
> something totally different is a choice we can make later).

So principle 5 governs the **three main trees**. A subclass tree gated behind
class progress is a deliberate exception, not a violation.

**The second half is still unanswered**, and it is the one open question this
document carries: *how* subclasses attach. It is not urgent — subclasses were
subsequently **cut** (`design-decisions.md`, "Cut — won't build": *"Subclasses /
hybrids — SU1 cross-class skill access (#58). Far-future, downstream of the
whole skill system"*) — so the question returns only if that cut is reversed.

## What is enforced in code

| principle | where |
|---|---|
| **1** — three trees: Class, Adventure, Survival | `treeClass` / `treeAdventure` / `treeSurvival`, `validTree` (`internal/game/skills.go`) |
| **5** — no cross-tree prerequisites | `validateSkillDefs` panics at content load; a cross-tree prereq is a **build failure**, not a runtime bug |
| **10/11** — prerequisites combine within a tree | `skillDef.prereqs`, validated for cycles and cross-tree links at load |
| **14** — percentages add, never compound | the modifier pipeline folds `effMulPct` additively within one event; stages compose across events, so crit and reduction stay true multipliers |
| the near-sighted question | settled **near-sighted** in #124: you see learned skills and what you can learn next, nothing else — enforced **server-side** so the client cannot leak the tree |

Principle 12 ("never have being a certain level be a prerequisite") is satisfied
by construction: `skillDef` has no level field, so a level gate cannot be
expressed.

## What is NOT built: the Adventure tree is nearly empty

The principles are implemented. The **content is not evenly distributed**:

| tree | skills | principle 6/7/8 scope |
|---|---|---|
| Class | 9 | class buffs and new mechanics |
| Survival | 5 | healing, camp making, resource gathering |
| **Adventure** | **1** (Scouting) | the map, surroundings, loot, fog of war |

Principle 7's whole subject area — map, surroundings, loot, fog of war — rests
on a single skill. `FEATURES.md` lists "the rest of the skill trees" as
outstanding, and `design-decisions.md` still marks Skills (#57, #61, #62) as
deferred. Anyone asking "is the skill system built?" should read that as **the
engine and the rules yes, the content no**.

## Related

- `docs/design-decisions.md` — the shipped-decision record, including the
  subclass cut.
- `docs/content-authoring.md` — how to write a skill as registry data.
- `docs/FEATURES.md` — the near-sighted panel, and skill-point mechanics.
