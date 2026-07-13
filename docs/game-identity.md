# What mediumrogue is — and isn't

*A one-page identity anchor. mediumrogue borrows vocabulary from several
genres but is none of them, so feature requests tend to drift toward whichever
label is nearest ("add an auction house", "make combat real-time", "roll to
hit"). This note names what the game actually is, so a drifting idea has
something concrete to check against. Design rationale lives in
`roguelike-mp-plan.md`; the combat-model reasoning in
`combat-model-notes.md`.*

## In one line

**A synchronous co-op session game in a persistent shared hex world** — WeGo
simultaneous 4-second turns, no twitch/reflexes, combat resolved by ARPG stat
grammar (decoupled `evasion%` / `crit%` folded through a modifier pipeline).
Built for a ~15-friend group who hop on together for an evening.

## It IS

- **Turn-based and simultaneous (WeGo).** The world advances on a shared
  4-second tick; everyone commits an intent and it resolves together.
- **A decision game, not an execution game.** Determinism is load-bearing —
  no reflexes, no skill-shot timing, ever. Skill is *planning*.
- **A persistent shared world** for a small trusted roster (~15 friends),
  server-authoritative, with chat / parties / quests.
- **ARPG in its stat grammar.** Gear = pure-data modifier cards; defence is a
  rule, not a stat; damage-reduction and decoupled `evasion%` / `crit%`.
- **Spatially a hex-grid tactics game** — discrete movement, hex range, ring
  AoE, positioning that matters.

## It ISN'T (the three false-friend labels)

| Looks like… | …but isn't, because |
|---|---|
| **an ARPG** (Diablo/PoE) | No real-time twitch, no random-affix loot slot-machine, no rarity tiers / crafting / currency / market economy, and a deliberately **flat power curve** (no level/gear treadmill). We took ARPG's *stat math*, not its *structure*. |
| **an MMO-lite** | No async come-and-go payoff: the fun (simultaneous co-op combat, XP-by-presence) rewards being online *together*. A global turn clock, a fixed trust-roster, no matchmaking / sharding / economy. It's a **session game**, not a log-in-whenever world. |
| **a TTRPG** (D&D) | No initiative, no sequential turns, no D&D-style multi-action turn (action + bonus + reaction), no coupled `d20`-vs-AC to-hit roll. Combat is decoupled percentage stat-checks resolved simultaneously. *(The roadmap's "combat action economy" (`ACT`) is a different thing — it's about which single action your one WeGo intent can be, e.g. block or heal instead of attack. That fits.)* |

## Why the combination is coherent

The one mechanic that reconciles "turn-based combat" with "shared persistent
world" is the **combat time bubble**: the global clock delivers simultaneity
where it matters (co-op fights), and bubbles decouple local time where it
doesn't (distant players keep moving while a fight takes its WeGo turns).
That's the keystone — invest there before the world grows, because it's what
keeps 15 players in one world from waiting on each other.

## Guardrails — be suspicious of a request that pulls toward…

- **Real-time / twitch / reflex** anything → breaks determinism, the core
  pillar. **Asked and answered (2026-07-13): WeGo stays; realtime rejected.**
  The wire (SSE/REST) is *not* the binding constraint — realtime dies on the
  design: it makes reflexes/ping/APM matter among a mixed group of ~15
  friends, kills seeded determinism (timing becomes an input), makes the
  combat bubble meaningless (nothing left to reconcile), and demands a
  different engine (prediction/reconciliation, per-frame simulation) to
  converge on a browser Diablo-MMO we'd lose on feel. The formula is **ARPG
  stat grammar, WeGo tempo** — we took ARPG's math and refused its clock, on
  purpose. If combat feels slow/static, that's a *feel* problem with
  WeGo-native fixes (playback drama, sound, tighter solo-bubble
  `TURN_INTERVAL`), not a model problem.
- **Initiative or "your turn, then mine"** → breaks WeGo simultaneity (and the
  15-player no-waiting premise).
- **A coupled to-hit roll / Armor-Rating / AC / `d20`** → we chose decoupled
  `evasion%` / `crit%` on purpose (see `combat-model-notes.md`).
- **Random-affix loot, rarity tiers, crafting, currency, a market economy** →
  loot is authored rule-cards; the chase is *designed*, not rolled. *(Simple
  friend-to-friend trading and the decided sanctuary trade-hub / merchant NPC
  (plan §9) are in scope — the guardrail is against auction-house-style
  economy machinery, not against handing a friend a sword.)*
- **A power/gear treadmill, infinite vertical scaling, endgame grind** →
  progression is utility/modifier-based; raw-stat scaling is deliberately cut.
- **Matchmaking, strangers, sharding, anti-grief machinery** → the fixed
  friend-group trust model is the intended scope.

None of these are *forbidden* — but each one trades away part of the identity
above, so it's a deliberate design decision, not a routine feature.

## What DOES support the identity (worth building)

- **Persistent per-player return** — reconnect drops you back *as your
  character, where you left off* (milestone 10a laid the foundation:
  character-link token, disconnect archive, snapshot). Confirm whether the
  *bed / home-spawn* piece still needs building — it's what makes the
  "persistent world" promise feel real per player.
- **Snappy solo/async tempo** — a lone player's combat should resolve as fast
  as they click (the bubble's action-gating mostly delivers this); it keeps
  playing while friends are offline from feeling punishingly slow.
