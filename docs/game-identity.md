# What mediumrogue is — and isn't

*A one-page identity anchor. mediumrogue borrows vocabulary from several
genres but is none of them, so feature requests tend to drift toward whichever
label is nearest ("add an auction house", "make combat real-time", "roll to
hit"). This note names what the game actually is, so a drifting idea has
something concrete to check against. Design rationale — and the combat-model
reasoning (its appendix) — lives in `design.md`.*

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
  rule, not a stat; damage-reduction and decoupled `crit%` (shipped) /
  `evasion%` (the intended identity; not yet built — #91).
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
  `evasion%` / `crit%` on purpose (see `design.md`).
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
  character-link token, disconnect archive, snapshot). The *bed / home-spawn*
  model is decided (roadmap Q9: sanctuary-scatter first spawn, then
  last-visited bed) but the bed slice is not yet built — it's what makes the
  "persistent world" promise feel real per player.
- **Snappy solo/async tempo** — a lone player's combat should resolve as fast
  as they click (the bubble's action-gating mostly delivers this); it keeps
  playing while friends are offline from feeling punishingly slow.

## How the Game Plays — a plain-language summary

*For someone who knows games but doesn't care about the tech. The gist of every decision in this document, described as the game you'd actually experience.*

**Getting in.** There's nothing to install. You get a link in the group chat, open it in your browser, claim your character — picking one of three classes (**rogue, fighter, or mage**) and one of three species (**human** learns faster, **elf** lands critical hits more often, **dwarf** shrugs off part of every hit) — and you're standing in the world. When the game gets updated, you just refresh — everyone is always on the same version.

**The world.** A shared fantasy world built on hexagon tiles, with deliberately simple, chunky retro graphics under a CRT-style filter — think old-school roguelike charm rather than modern polish. All ~15 of us are in the same world at the same time, each with our own character.

**How time works — the heartbeat.** The world moves in shared beats: every 4 seconds, one "turn" happens for everyone at once. During the first 2 seconds you can choose an action; then everything everyone chose plays out together in a short animation. In practice you barely notice the rhythm while exploring: you click somewhere on the map and your character walks there on their own, beat by beat, while you chat. You never have to be quick — if you do nothing, you simply stand still. Nobody has an advantage for having fast reflexes or a better internet connection; the game is closer to a board game where everyone moves their piece simultaneously than to an action game.

**When danger appears — time stops (for you).** The moment you and a monster spot each other within 6 tiles, the clock freezes *locally* — for you, the monster, and anyone standing nearby. The fight now plays like a proper turn-based tactics game: you can stare at the battlefield and think as long as you like. The turn advances when everyone in the fight has locked in their choice (with a patience limit of 30 seconds, so one distracted player can't stall the fight forever).

Here's the fun part: **the rest of the world keeps running at normal speed.** Your friends elsewhere on the map see your fight frozen mid-swing, marked as "in combat" — and they can walk over and *step into it*. Entering the fight area means joining the fight; there's no invite screen or loading transition. Yelling "help, three ghouls, north bridge!" in chat and watching the cavalry arrive is an intended core experience. Escaping works the same way in reverse: break line of sight or get far enough away, and you slip back into normal time.

**Fighting.** Combat is classic roguelike at heart: walk into an enemy to hit them. The three classes fight differently: a **fighter** deals steady melee damage and is tough enough to hold the front; a **mage** is fragile but casts area magic from the back, hitting groups of enemies at once; a **rogue** hits hardest against single targets but can't take a beating, and switches weapons by distance on their own — dagger against something adjacent, bow against something far away. Within each class there's variety to find: weapon types that trade speed against damage against reach, and different kinds of magic. Within a turn, all movement happens first, then all attacks land. Two consequences you'll feel immediately: stepping *away* from an enemy genuinely dodges the swing aimed at you (so retreating is a real tactic, not just delaying death), and two combatants who go for each other can absolutely take each other down on the same turn — those mutual-kill moments are meant to be dramatic, not glitchy.

**Traveling together.** Up to 5 players can stand on the same tile, so a party moves as one stack — one blob on the map heading somewhere with a shared destination. When something attacks the stack, it hits a random member, so a group soaks danger together. And 15 players *can't* all fit on one tile, which quietly encourages the group to split into a few parties instead of one unstoppable death ball.

**Quests.** Two flavors. **Player quests** are yours: you pick them, you pursue them. **Party quests** are shared: someone takes one, pitches it in chat ("who's coming?"), and invites others in. Groups aren't assigned — they form around whoever proposes something interesting, and dissolve just as naturally.

**Progression and death.** Your character earns **XP levels and gear** through play. XP comes from slain enemies, and it's shared generously: the moment an enemy falls, everyone standing in that fight gets the same, full amount — nobody competes for last hits, and running over to help a friend's fight always pays off, kill by kill, from the moment you arrive. Dying is not the end — no roguelike permadeath here — but it has a real sting: **you fall back to the start of the XP level you were in**, losing the progress inside that level. Levels themselves are never taken away, so an evening's real progress survives a bad fight, but "I'm 80% to level 6" is exactly the thing you're gambling when you pick one more battle. Death makes fights tense without ever deleting your character.

**Why it's built this way, in one line each:**
- *Slow shared turns* → fair for everyone, chat-friendly, and lag simply doesn't matter.
- *Frozen local fights* → real tactical thinking without holding up the rest of the world.
- *Walk-in reinforcements* → fights become social events, not private instances.
- *Browser-only* → zero setup for fifteen people with fifteen different computers.
- *Simple hex graphics + filter* → charm over polish, and every hour goes into the game instead of art.

