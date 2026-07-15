# Melee as an Attack Intent: Design

*Status: approved 2026-07-15 (follow-up brainstorm with the maintainer,
after #104/#113). Tracks issue #116. Implementation follows a separate
plan after spec review (standard milestone-slice pause).*

## Goal

Retire the player's bump: **attacking never moves you.** Today a melee click
submits a *move* intent that the server converts into a swing
(`collectMeleeAttacksLocked`), and the move half leaks into gameplay twice:
the retained path walks the player onto the vacated hex after a kill (a move
nobody asked for), and one click auto-swings every turn — unlike ranged,
where one click is one shot. The maintainer's rule: *"it should only attack,
not move."*

Decisions (from the brainstorm):

1. **Melee is an entity-targeted attack intent** — the bow's existing shape,
   at distance 1, resolved with the attacker's melee weapons.
2. **One click = one swing** — full cadence parity with ranged; no standing
   auto-swing.
3. **Attacking never moves you** — after a kill the player stays put;
   stepping onto the corpse hex is a separate walk click.
4. **A distant-monster click walks and stops adjacent** — engaging is then
   an explicit attack click.
5. **Monsters are unchanged** — their AI keeps attacking by pathing onto
   players; move-conversion becomes a monster-only rule.

## Server

### Intent validation — `queueAttackLocked` (`internal/game/world.go`)

The wire shape is unchanged (`IntentRequest.Kind = attack` +
`TargetEntityID`); validation widens to accept melee:

- The `rangedDefFor(e) == nil → ErrNoRangedWeapon` pre-gate moves into the
  **ground-targeted** branch only (a hex-targeted attack still requires a
  ranged/magic weapon).
- **Entity-targeted** branch: victim checks unchanged (exists+alive →
  `ErrAttackTargetNotFound`; opposing → `ErrAttackTargetNotHostile`). The
  reach check becomes: `HexDistance(e.hex, victim.hex) == 1` (melee — every
  entity is melee-armed, `meleeDefsFor` falls back to fists) **or** at least
  one held ranged/magic weapon reaches (today's rule). Neither →
  `ErrOutOfRange`.

### Resolution — `resolveEntityTargetedLocked`

At resolution, if the victim is **adjacent** (`HexDistance == 1` — positions
are pre-move, #104, and nothing moves between submit and the attack phase),
the attack is a **melee swing**: every def in `meleeDefsFor(attacker)` lands
its own hit on the named victim (dual-wield parity with today's conversion
path — `attackLocked`'s per-weapon loop), each through `rollDamageLocked`.
Melee is **exclusive** at distance 1: ranged/magic defs do not also fire
(the weapon-by-distance identity — a rogue swings the dagger adjacent,
shoots the bow at range). At distance ≥ 2, ranged resolution is unchanged.

The mage is untouched by this branch: an adjacent blast is already a
**ground-targeted** AoE intent (client `aoeReaches` routing) and stays one.

**Behavior change on stacks:** a melee attack on a stacked hex hits the
*named* victim, like a bow shot — the conversion path's seeded stack-victim
pick no longer applies to players. (Monsters keep the rng pick — see below.)

### Move-conversion becomes monster-only — `collectMeleeAttacksLocked`

Scope the pre-move scan to `e.kind == protocol.EntityMonster`. A **player**
mover whose next step is hostile-held no longer converts — it is simply
blocked by `movePhaseLocked`'s existing `hasOpposing` check (waits, path
retained). Consequence: an auto-walk intercepted by a monster stepping into
the path never swings by accident; the player is stopped next to the
monster and chooses.

Monster behavior is byte-identical: the AI paths onto players and its
conversion, per-weapon claws loop, and stacked-hex rng victim pick all stay.

### Distant clicks stop adjacent — `queueMoveLocked`

After `Pathfind`, if the **destination** hex holds an opposing entity, trim
the final step (`path = path[:len(path)-1]`) — the walk stops on the
adjacent hex. A trim to an empty path is a no-op move (already adjacent —
the client routes that click as an attack anyway; the trim covers distant
clicks and the defensive case). The trim reads the board at submit time
only; if the monster wanders during the walk, the path just ends one hex
short of where it stood — the player re-clicks, same as any stale route.

## Client (`client/src/main.ts`)

- **`meleeAt` submits `IntentAttack` + `targetEntityID`** (via
  `hostileIdAt`) instead of `IntentMove`. Its #113 feedback (attack flash,
  committed crosshair, no destination bookkeeping) is already correct for
  this model and does not change. One-click-one-swing needs no client
  bookkeeping: attack intents already clear at resolution.
- **Keyboard steps route through `clickTarget`**, not straight to `walkTo`
  (`bindMovementKeys`' `onStep`, main.ts ~1463). Today a key-step into an
  adjacent monster is a bump — under the new server rules it would silently
  become a blocked move and **keyboard melee would be lost**. Routing
  through `clickTarget` keeps the roguelike idiom (stepping into an enemy
  attacks it) while sending the honest intent; it also gives key-steps the
  same in-combat reach filter clicks already have. SPACE/wait is untouched
  (already routed through `clickTarget`).
- No `window.game` shape changes; `combatMoves`/`lastAttackFlash`/
  `committedAction` semantics survive as-is (the melee-feedback e2e spec's
  assertions hold unchanged).

## Consequences to encode in docs (implementation PR, per the FEATURES rule)

- Melee reads: *click (or key-step into) an adjacent enemy to swing; one
  click per swing; you never move by attacking.* The "classic roguelike
  bump-to-attack" gloss in `design.md`/`FEATURES.md` becomes historical
  ("monsters still fight this way; the player's melee is an attack intent").
- The #103 bubble-entry carve-out ("a single remaining step survives — in
  particular the standing melee intent") loses its melee justification: a
  single surviving step is now just a deliberate adjacent *move*
  (`internal/game/bubble.go` comment + FEATURES wording).
- `design.md` §5's melee bullet (move-conversion) gets the player/monster
  split.

## Determinism note (for the implementation plan)

Player melee moves from the conversion pass (`attackLocked`'s list, with
its stack-victim rng pick) into the entity-targeted pass
(`resolveRangedLocked`, id-sorted, no victim pick). Turn-rng consumption
shifts wherever a player melee lands this turn: seeded expectations
**re-derive, never weaken**. Monster-only turns are unaffected.

## Test migration (sketch — the plan owns the details)

- Unit tests that drive *player* melee via a move-onto-monster
  (`submitOK(w, me, monsterHex)` in `combat_test.go`, `species_test.go`,
  `melee_damage_test.go`, `glance_test.go`, `kinds_combat_test.go`, …)
  switch to entity-targeted attack intents
  (`entity_targeted_ranged_test.go`'s `entityAttackIntent` pattern).
  Monster-side melee keeps `SetPathForTest` + conversion.
- New coverage: a player move intent onto a hostile-held hex **blocks**
  (no damage, no movement, path retained); a melee swing resolves with
  EVERY held melee weapon (dual-wield parity); a fighter — who holds no
  ranged weapon — can submit an adjacent entity-targeted attack (the old
  `ErrNoRangedWeapon` pre-gate must not fire); a non-adjacent
  entity-targeted attack with no reaching ranged weapon still rejects
  with `ErrOutOfRange`.
- e2e survives structurally: `combat.spec.ts`'s chase and
  `melee-feedback.spec.ts` click through `clickTarget`, which sends the new
  intent; assertions are on HP and feedback, not intent kind. The
  melee-feedback spec's "standing melee intent" cancel step becomes
  redundant (one-shot intents) — keep or simplify in the plan.

## Scope

- **This spec + PR:** the spec document only.
- **Implementation PR (after review):** `internal/game` (validation,
  resolution branch, conversion scoping, pathfind trim), client routing
  (`meleeAt` intent kind, keyboard `onStep` → `clickTarget`), test
  migration, docs/FEATURES alignment. No protocol shape changes, no new
  constants, no snapshot version bump (no state-shape change).
- **Out of scope:** monster AI changes; melee auto-repeat (rejected);
  attack-move hybrids (#93 stays cut); crit/glance client feedback (#114).
