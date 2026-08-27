// Package protocol is the single source of truth for the wire between the Go
// server and the TypeScript client. Every exported type and constant here is
// mirrored into client/src/protocol.gen.ts by tygo (`make protocol`); the
// generated file must never be edited by hand.
package protocol

// Turn cadence. One world turn every TurnSeconds: an input window, an instant
// server resolution, then a playback window on the client. Inside a combat
// time bubble the cadence is suspended and turns are action-gated instead.
const (
	// TurnSeconds is the full world-turn period out of combat. Lowered 5→4
	// (playtest feedback batch 3, item 1; playtest 2026-07-11: a 3 s input
	// window felt slow) — the plan's §9 "feel-test the cadence" decision
	// landing at 2 s input / 2 s playback.
	TurnSeconds = 4
	// InputWindowSeconds is the slice of the turn in which intents are accepted.
	// Lowered 3→2 alongside TurnSeconds (see above).
	InputWindowSeconds = 2
	// PlaybackSeconds is the client-side animation window after resolution.
	PlaybackSeconds = 2
)

// World rules that both sides need to agree on.
const (
	// CombatRadius is the mutual-line-of-sight distance (in hexes) at which a
	// combat time bubble forms around a player and a hostile.
	CombatRadius = 6
	// MonsterAggroRadius is the hex distance at which a WORLD-domain monster
	// notices a player and starts hunting it; beyond it, a monster stands
	// still (#36 — no wander this slice). It MUST stay strictly greater than
	// CombatRadius: a monster has to notice a player before it can close the
	// distance into a combat bubble, or it would sit frozen just outside
	// aggro range forever. A monster already inside a combat bubble ignores
	// this and keeps chasing its bubble's players unconditionally — a fight
	// is a fight.
	MonsterAggroRadius = 10
	// InterestRadius is how far (in hexes) a player receives live entity data
	// (#289): a turn bundle carries only what lies within it, so the world
	// beyond is known ground with nothing moving on it. It is fog of war, not
	// just a bandwidth cut — the client draws a soft vignette at the edge.
	//
	// It MUST stay strictly greater than the LARGEST per-kind aggro radius —
	// not merely MonsterAggroRadius, which kinds override (the Dragon sits at
	// 12). Otherwise a monster starts hunting from outside what the player can
	// see and then appears already aggressive. TestInterestRadiusExceedsEvery-
	// AggroRadius pins this against every kind, so a new high-aggro monster
	// fails the build instead of quietly eating the margin.
	//
	// 20 is also about the largest radius a player can SEE: at HEX_SIZE 32 the
	// ring reaches 960 px east-west, exactly the edge of a 1920x1080 viewport
	// at the client's default zoom. At 30 it sat off-screen at every zoom,
	// which would have made the fog-of-war edge invisible to everyone.
	InterestRadius = 20
	// MonsterLeashMultiplier sizes a WORLD-domain monster's default leash
	// radius (#102): a monster farther than MonsterLeashMultiplier × its own
	// base aggro radius from its home (spawn) hex drops any chase and walks
	// back home, ignoring players until it arrives. A monster kind can
	// override the derived radius directly (monsterDef.leashRadius), the same
	// way it overrides aggroRadius. Monsters inside a combat bubble ignore
	// the leash entirely — a fight is a fight.
	MonsterLeashMultiplier = 2
	// StackCap is the maximum number of friendly entities on one hex — sized
	// so a full party fits.
	StackCap = 5
	// RepathDetourSlack bounds how far out of its way a PLAYER's queued walk
	// will go around something standing in it (#96): when the next step is
	// blocked, the re-routed path is taken only if it is at most this many
	// hexes longer than the route it replaces — otherwise the walker waits
	// where it is, path retained, exactly as it did before #96.
	//
	// The guard exists because blockers are TRANSIENT: the monster in your way
	// has usually moved on by next turn. Rounding a single blocker on open hex
	// terrain costs about +2, a full StackCap blob about +4; a detour that
	// costs more than that means a real chokepoint, where standing still for a
	// turn beats hiking around the map.
	RepathDetourSlack = 4
	// RingCount is the number of distance-based difficulty rings worldgen
	// bands the map into (milestone 6c): ring 0 (home) through RingCount-1
	// (frontier). Monster-kind registry validation requires every ring to
	// have at least one kind that spawns in it.
	RingCount = 3
	// SanctuaryRadius is the hex distance from the origin within which no
	// hostile monster spawns (milestone 6c) — the seed of a future trade
	// hub (plan §9 recovery entry). Deliberately smaller than CombatRadius:
	// the player-proximity spawn guard (#36) already keeps a fresh spawn
	// clear of an instant fight, so the sanctuary's job is the PERMANENT
	// safe zone, not spawn-moment safety.
	SanctuaryRadius = 5
	// DragonCount is the maximum number of dragons SpawnMonsters places in
	// one world — the rare, ring-2 boss kind.
	DragonCount = 1
	// MaxChatLen caps a chat message length in runes (defence-in-depth; the
	// client also caps input). MaxNameLen caps a player's display name.
	MaxChatLen = 500
	MaxNameLen = 24
)

// SystemSender is the reserved chat-sender label the server uses for its own
// announcements (party ops etc.). The client styles any message from this
// sender as a server line, so a player may not take it as a name (#198).
// Shared here so the reserved-name check and the label cannot drift apart.
const SystemSender = "system"

// _ [MonsterAggroRadius - CombatRadius - 1]struct{} is a compile-time guard
// on the invariant documented on MonsterAggroRadius above: a negative array
// length is a compile error, so the package fails to BUILD (not just fail a
// test) the moment either constant changes to violate
// MonsterAggroRadius > CombatRadius. TestMonsterAggroRadiusExceedsCombatRadius
// (protocol_test.go) asserts the same thing at the test level for a clearer
// failure message.
var _ [MonsterAggroRadius - CombatRadius - 1]struct{}

// Hex is an axial coordinate on the flat-top hex grid. See Red Blob Games'
// hex guide for the coordinate math conventions.
type Hex struct {
	Q int `json:"q"`
	R int `json:"r"`
}

// Terrain is a tile's ground type. Wire values are strings for a readable
// stream; the set is closed — the client renders unknown terrain as rock.
type Terrain string

// The terrain set. Rock is impassable and rings the world edge; water is
// impassable but open; grass, forest and mud are walkable. Mud (#437) is
// mechanically grass — it differs in look, and in what buries there (#436).
const (
	TerrainGrass  Terrain = "grass"
	TerrainForest Terrain = "forest"
	TerrainMud    Terrain = "mud"
	TerrainWater  Terrain = "water"
	TerrainRock   Terrain = "rock"
)

// Entity kinds. Players join; monsters are spawned hostiles. The set is closed;
// a client renders an unknown kind as a monster (safer than as a player).
const (
	EntityPlayer  = "player"
	EntityMonster = "monster"
)

// Classes: the three playable character types.
const (
	ClassFighter = "fighter"
	ClassRogue   = "rogue"
	ClassMage    = "mage"
)

// Species: the three player species with distinct passive bonuses.
const (
	SpeciesHuman = "human"
	SpeciesElf   = "elf"
	SpeciesDwarf = "dwarf"
)

// Intent kinds: the type of an IntentRequest. Kind is required — it must be
// one of the constants below. Every inventory action (equip, unequip, drop,
// pickup, drink) follows one shared rule: outside a combat bubble it applies
// immediately and costs nothing; inside a bubble it is the player's
// committed action for that turn.
const (
	IntentMove   = "move"
	IntentAttack = "attack"
	// IntentEquip equips an owned item (IntentRequest.ItemID) from the
	// backpack into its type-derived slot, swapping any displaced occupant
	// back into the vacated backpack entry. Naming an already-equipped item
	// toggles it OFF (equivalent to IntentUnequip — playtest batch 2's
	// toggle behavior, kept).
	IntentEquip = "equip"
	// IntentUnequip moves an equipped item (IntentRequest.ItemID) back into a
	// free backpack entry; rejected if the backpack is full.
	IntentUnequip = "unequip"
	// IntentDrop drops an owned item (IntentRequest.ItemID) — equipped or in
	// the backpack; a consumable stack drops whole — onto the player's own
	// hex as ground item(s).
	IntentDrop = "drop"
	// IntentPickup picks up one ground item (IntentRequest.GroundItemID) from
	// the player's own hex: merged into a matching consumable stack first,
	// else into a free backpack entry; rejected with a clear error if
	// neither exists. Items never auto-equip on pickup. Replaces walk-over
	// auto-pickup (the inventory-slots milestone).
	IntentPickup = "pickup"
	// IntentLearnSkill spends a banked skill point on a learnable skill
	// (IntentRequest.SkillID) — #124. Unlike every other inventory-ish
	// action this is NOT queueable inside a combat bubble: learning is a
	// between-fights decision, so it is rejected outright in combat rather
	// than costing a bubble turn.
	IntentLearnSkill = "learn-skill"
	// IntentDrink drinks one unit of an owned consumable stack
	// (IntentRequest.ItemID): applies the def's timed-effect payload (a
	// self-buff, a cleanse) and decrements the stack; an emptied stack frees
	// its backpack entry. There is no flat heal — #410 deleted the heal
	// consumables and #415 the field behind them.
	IntentDrink = "drink"
	// IntentUseSkill triggers a learned ACTIVE skill (IntentRequest.SkillID)
	// at IntentRequest.Target — #161. It is the turn's action, exactly like a
	// move: it does not stack with one, and it is not a bonus action.
	IntentUseSkill = "use-skill"
	// IntentQuaffHealth / IntentQuaffEnergy are the two ALWAYS-AVAILABLE
	// draughts (#322): no item, no inventory slot, no scarcity — a cooldown is
	// the whole cost. Separate kinds rather than one with a "which pool" field,
	// so the wire says what it means and the client's E/R keys map one-to-one.
	IntentQuaffHealth = "quaff-health"
	IntentQuaffEnergy = "quaff-energy"
)

// The item taxonomy (gear keystone, #55/#56): one weapon type carrying
// tags, plus armor/jewelry types that each map 1:1 to an equip slot.
const (
	ItemTypeWeapon     = "weapon"
	ItemTypeConsumable = "consumable"
	ItemTypeHelmet     = "helmet"
	ItemTypeChest      = "chest"
	ItemTypeGloves     = "gloves"
	ItemTypeBoots      = "boots"
	ItemTypeRing       = "ring"
	ItemTypeAmulet     = "amulet"
	// ItemTypeShield occupies the off-hand — pure defence, never fires as a
	// hit (#90, S4 of #55).
	ItemTypeShield = "shield"
)

// Weapon tags: which attacks fire the weapon (§3 of the keystone spec).
const (
	WeaponTagMelee  = "melee"
	WeaponTagRanged = "ranged"
	WeaponTagMagic  = "magic"
)

// ForestSightCost is what one forest hex between two entities costs a line of
// sight, in hexes of effective range (#95). Rock blocks sight outright;
// forest SOFTENS it — you see a long way over open grass and only a short way
// into trees. Against CombatRadius that reads: 6 hexes over grass, ~4 through
// one belt of trees, ~2 through two.
const ForestSightCost = 2

// Damage types (#92, DT1): every attack carries exactly one, and resistances
// and vulnerabilities are take-damage rule cards conditioned on it — one
// vocabulary shared by the engine, content, and the client tooltip. Three
// families of two: physical (Sharp/Blunt), elemental (Fire/Ice), and
// metaphysical (Holy/Chaos).
//
// The families and the Holy↔Chaos / Fire↔Ice oppositions are an AUTHORING
// CONVENTION, not machinery: all six types are mechanically flat, and a
// "Chaos monster fears Holy" is a vulnerability card someone wrote, not an
// axis the engine knows about. Promotable to a real axis later if content
// always ends up mirrored.
const (
	DamageTypeSharp = "sharp"
	DamageTypeBlunt = "blunt"
	DamageTypeFire  = "fire"
	DamageTypeIce   = "ice"
	DamageTypeHoly  = "holy"
	DamageTypeChaos = "chaos"
)

// Equip-slot names. Armor slots equal their item type; weapons go to a
// hand (main first, then off; two-handed locks both).
const (
	SlotMainHand = "main-hand"
	SlotOffHand  = "off-hand"
	SlotHelmet   = ItemTypeHelmet
	SlotChest    = ItemTypeChest
	SlotGloves   = ItemTypeGloves
	SlotBoots    = ItemTypeBoots
	SlotRing     = ItemTypeRing
	SlotAmulet   = ItemTypeAmulet
)

// BackpackSize is the fixed number of backpack entries every entity has (the
// inventory-slots milestone). An entry holds one gear instance, or one
// consumable stack (identical defs merge up to ItemStackCap; stacks never
// split).
const BackpackSize = 4

// ItemStackCap is the maximum count of identical consumables in one backpack
// stack. Distinct from StackCap (max FRIENDLY ENTITIES on one hex) — same
// launch value, unrelated invariant, kept as separate named constants so a
// future tuning change to one never accidentally reads as the other.
const ItemStackCap = 5

// MaxPlayers caps concurrent players (#199) — a DoS bound sized with comfortable
// headroom for the target deployment: a shared-network house of ~32 players
// (with room to grow), all potentially joining from one address. A join past it
// is refused 503. The JOIN_MIN_INTERVAL bucket bursts MaxPlayers, so a full
// 64-strong mass reconnect after a restart is admitted at once (intended — the
// cap is a bound, not a throttle on legitimate return).
const MaxPlayers = 64

// Starting/maximum hit points by kind. HP is on the wire from milestone 6.2 so
// the client can show health bars once combat (6.3) starts changing it.
// MonsterMaxHP is superseded by per-kind maxHP (internal/game's monsterDef
// registry, milestone 6c) — wolf's entry carries this exact value forward —
// but stays here as the historical baseline several tests still pin against.
const (
	PlayerMaxHP  = 20
	MonsterMaxHP = 10
)

// Energy is the action currency every active spends (#322). Maxima are the
// INVERSE of the HP curve: the squishy classes are the ones that live on their
// skills, so they carry more of it.
const (
	FighterMaxEnergy = 60
	RogueMaxEnergy   = 80
	MageMaxEnergy    = 100
	// EnergyPerLevel is the flat gain per level above 1. Flat rather than
	// mirroring HP's front-loaded curve — energy wants to feel steady, and one
	// curve to reason about is enough.
	EnergyPerLevel = 5
	// EnergyRegenPerTurn is recovered every turn, IN combat as well as out
	// (#322) — unlike RegenPerTurn, which a bubble suspends. A long fight
	// therefore yields roughly one extra cast rather than draining to nothing.
	EnergyRegenPerTurn = 5
)

// PotionCooldownTurns is the wait between draughts of the same kind (#322).
// Health and energy cool independently, so emptying one does not lock the
// other.
const PotionCooldownTurns = 5

// PotionRestorePercent is how much of the pool's MAXIMUM a draught returns
// (#322) — a percentage rather than a flat amount, so it keeps its value as
// pools grow with level.
//
// Lowered 40 -> 25 in #410. At 40 the draught was most of a solo player's
// answer to danger: the balance sim puts solo deaths at 1.12 per 100 turns
// there and 2.00 at 25, against the 5.83 recorded when the only healing was a
// carried potion. Deleting the heal consumables is what surfaced it — the sim
// had been modelling a 5 HP potion, not the free 40%-of-max button players
// actually use, so the drift had been invisible.
const PotionRestorePercent = 25

// EvadeCooldownTurns is how many turns must pass between evades (#322). Here
// rather than in the skill registry because BOTH sides need it: the server
// gates the intent on it, and the HUD ball's radial sweep is drawn from it.
const EvadeCooldownTurns = 3

// EvadeRangeHex is how far an evade may carry you (#322). Both sides need it:
// the server gates the intent on it, and the client paints the reachable tiles
// so the aim is never a guess.
const EvadeRangeHex = 3

// RegenPerTurn is the HP a player passively recovers on each turn resolution
// while below max HP — the passive recovery layer (plan §9). It kills the
// inverted incentive where dying (a full-HP respawn) was the only way to heal:
// standing around out of a fight now tops you up too, slowly.
//
// It applies in BOTH domains — world turns and combat-bubble turns alike
// (#322 decision 11). Bubbles originally had no healing at all, and the
// comments here said so long after that stopped being true (#398). The value
// stays at 1 precisely BECAUSE it now ticks mid-fight: a trickle that small
// cannot outpace a monster hitting for 3-9, so recovery-in-combat never
// becomes attrition removed rather than managed.
//
// Monsters never regen at all — only the Hydra heals, via its bite card.
const RegenPerTurn = 1

// XP & leveling (milestone 6b.1; curve replaced by a quadratic one in the
// fast-lane batch, XP1). Per-class/species tuning is 6b.2/6b.3. Per-kill XP
// is monster-kind content data since 6c (internal/game's monsterDef.xp) —
// wolf carries the old flat MonsterXP value (20) forward unchanged; there is
// no single flat award anymore.
const (
	// XPCurveBase scales the quadratic XP curve: the total XP required to
	// REACH level L is XPCurveBase * (L-1)^2 (#60, roadmap XP1). Gaps grow
	// linearly: 100, 300, 500, ...
	XPCurveBase = 100
	// QuestKillRewardPerTarget is the flat per-target XP a kill quest's
	// reward is built from (targetN * QuestKillRewardPerTarget), independent
	// of which monster kind actually gets killed toward it — deliberately
	// decoupled from monsterDef.xp (a kind's own combat kill award) since
	// 6c introduced per-kind XP.
	QuestKillRewardPerTarget = 20
)

// Per-class base stats (level 1). Level scaling: MaxHP += the front-loaded
// curve's cumulative bonus (see HPGainBase/HPGainMin below).
// Weapon damage/range/AoE are content data now (internal/game's item
// registry, milestone 6b.4) — see itemDamage there; levels do not scale
// damage (#60, roadmap XP3: no raw-stat scaling — levels give HP and,
// later, skill points).
const (
	// FighterMaxHP is the level-1 max HP for Fighter class (tanky melee).
	FighterMaxHP = 30
	// RogueMaxHP is the level-1 max HP for Rogue class (high single-target damage, squishy).
	RogueMaxHP = 16
	// MageMaxHP is the level-1 max HP for Mage class (AoE ranged, squishy).
	MageMaxHP = 14

	// FistsDamage is level-1 damage for fallback/unarmed attacks (the empty
	// close-slot fallback; see internal/game's fistsDef).
	FistsDamage = 1

	// HPGainBase/HPGainMin shape the front-loaded HP curve (#60, roadmap
	// XP2): the max-HP gain when advancing FROM level n is
	// max(HPGainMin, HPGainBase-(n-1)) — 8,7,6,...,1 then +1 forever.
	HPGainBase = 8
	HPGainMin  = 1
)

// Per-species passive bonuses (tunable, applied per-species in 6b.3+).
const (
	// ElfCritChancePercent is the percent base crit chance for Elf species.
	ElfCritChancePercent = 20
	// ElfCritMultiplier is the damage multiplier for Elf crits.
	ElfCritMultiplier = 2
	// DwarfDamageReduction is the flat damage reduction per attack for Dwarf species.
	DwarfDamageReduction = 1
	// SkillPointsPerLevel is the skill-point bank grant every player earns per
	// level gained (#124). Raised 2 -> 3 alongside SkillPointCost 1 -> 3
	// (#57/#161 tuning, 2026-07-19): at 2/level a 3-point cost would have made
	// the Human +1 worth a third of a skill every level instead of a rounding
	// difference, so raising both keeps the species gap where it was.
	SkillPointsPerLevel = 3
	// SkillPointCost is what learning ONE skill costs from the bank —
	// uniform across passives and actives (maintainer's call, 2026-07-19).
	SkillPointCost = 3
	// HumanBonusSkillPoints is the EXTRA point a Human earns per level — the
	// species perk that replaces the XP multiplier (#123/#124 task 8). Not a
	// rule card: a per-level bank grant is not a fold over a combat value.
	HumanBonusSkillPoints = 1
)

// Per-class passive bonuses (tunable). The Rogue's glance is the first
// class passive: the decoupled defender-side combat chance (#69/#91,
// amended 2026-07-15) — a glancing hit is HALVED, never fully negated (and
// the take-damage fold still floors every landed hit at 1).
const (
	// RogueGlanceChancePercent is the percent chance an incoming hit on a
	// Rogue only glances (GlanceDamagePercent applies).
	RogueGlanceChancePercent = 20
	// GlanceDamagePercent is a glancing hit's damage multiplier in percent
	// (50 = half damage), shared by any future glance-granting content.
	GlanceDamagePercent = 50
)

// Tile is one hex of the world map.
type Tile struct {
	Hex     Hex     `json:"hex"`
	Terrain Terrain `json:"terrain"`
}

// MapResponse is the payload of GET /api/map: the full static world map.
// Fetched once at client startup; entities move in turn bundles, the ground
// does not, so the map is not part of the SSE stream.
type MapResponse struct {
	// Radius is the map's hex radius: every tile satisfies
	// distance(origin, hex) <= Radius.
	Radius int    `json:"radius"`
	Tiles  []Tile `json:"tiles"`
}

// SSE event names on the GET /api/events stream.
const (
	// EventTurn announces a resolved world turn. Its SSE id is the turn
	// number so EventSource reconnection can resume via Last-Event-ID.
	EventTurn = "turn"
	// EventHeartbeat is a keep-alive frame. It carries no id (it is not a turn
	// and must not advance Last-Event-ID) and fires on a fixed HeartbeatInterval
	// so the client's liveness watchdog stays fed even when a frozen combat
	// clock stops turn frames.
	EventHeartbeat = "heartbeat"
	// EventChat announces a chat message. It carries NO id (chat is not a turn
	// and must not advance Last-Event-ID); its data is a JSON ChatMessage.
	EventChat = "chat"
)

// PartyMemberView is one row of the viewer's party roster (#289): identity
// only, no position or stats. Positional and combat state still travel on
// Entity, for the members close enough to appear there.
type PartyMemberView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PartyInviteView is the invite currently waiting on the viewer's answer
// (#385): who asked, so the client can name them in a prompt.
//
// Before this field the invite existed ONLY as a chat sentence
// ("<inviter> invited <target> to a party — <target>: /accept"), which meant
// the one interaction in the game whose whole UI was "read a line and type a
// thing" — and meant a headless client had to pattern-match English prose to
// know it had been invited. The state was always there server-side
// (w.pendingInvites); it simply never reached the wire.
type PartyInviteView struct {
	InviterID   int64  `json:"inviterId"`
	InviterName string `json:"inviterName"`
	// Members is the inviter's party as it stands right now — you are deciding
	// whether to join a GROUP, so the prompt names the group (maintainer's
	// call, 2026-08-08). id-sorted like the roster, so the client's <Index>
	// rows do not shuffle between turns.
	//
	// EMPTY when the inviter is not in a party yet, which is the common case:
	// accepting is what creates it. Empty rather than a one-entry roster
	// naming the inviter, who is already named by InviterName.
	Members []PartyMemberView `json:"members,omitempty"`
}

// BubbleView is a window into a combat bubble: who's in it, which members it's
// still waiting on, and how long until the patience timeout. Every bundle
// carries all active bubbles; a client picks the one whose MemberIDs include
// its own entity to drive its combat HUD.
type BubbleView struct {
	ID                  int64   `json:"id"`
	MemberIDs           []int64 `json:"memberIds"`
	WaitingForIDs       []int64 `json:"waitingForIds"`
	PatienceRemainingMs int64   `json:"patienceRemainingMs"`
}

// TurnEvent is the payload of an EventTurn frame: the world state after a
// resolved turn. A full entity snapshot every turn keeps clients trivially
// resyncable at this player count; deltas are a later optimization if ever
// needed. It will grow (attacks, deaths, chat) as the game develops.
type TurnEvent struct {
	// Turn is a monotonically increasing resolution counter, incremented on
	// every world-domain tick AND every combat-bubble resolution (they advance
	// on independent clocks). Monotonic, so it still serves as the SSE id /
	// Last-Event-ID watermark; it is not a pure world-turn count.
	Turn int64 `json:"turn"`
	// IntervalMs is the runtime turn period in milliseconds (the configured
	// TURN_INTERVAL). The client cannot derive this — TURN_INTERVAL is
	// env-configurable while the cadence constants are fixed — so it rides
	// each bundle and the client re-syncs its playback/input phase clock on
	// every arrival.
	IntervalMs int64 `json:"intervalMs"`
	// Entities is every entity within InterestRadius of the viewer, sorted by
	// ID — plus the viewer's own row unconditionally (#289). NOT the whole
	// world: what lies beyond is known ground with nothing moving on it.
	Entities []Entity `json:"entities"`
	// Bubbles is every active combat time bubble with at least one member the
	// viewer can see; a client filters to the one containing its own entity.
	Bubbles []BubbleView `json:"bubbles"`
	// Quests is the whole quest board, sorted by ID. Quests belong to a holder
	// rather than a hex, so the interest radius does not cull them.
	Quests []QuestView `json:"quests"`
	// GroundItems is every dropped item lying within InterestRadius of the
	// viewer (#289).
	GroundItems []GroundItemView `json:"groundItems"`
	// Party is the viewer's own party roster — every member's id and name,
	// COMPLETE regardless of distance, empty when the viewer is solo.
	//
	// It exists because partymates are culled from Entities like anything else
	// (#289): the client used to derive its roster by filtering entities on
	// partyId, so without this field a party that spread out would watch its
	// own roster shrink with no explanation. Roster membership is identity
	// data, not positional data.
	Party []PartyMemberView `json:"party"`
	// PendingInvite is the party invite awaiting this viewer's answer, or nil
	// when none is (#385). Own-only, for the same reason Party is: it is the
	// viewer's own identity state, and nobody else's business.
	//
	// A pointer rather than a slice because the server keeps at most one
	// pending invite per target — a second invite overwrites the first — so
	// "either there or not" is the honest shape.
	PendingInvite *PartyInviteView `json:"pendingInvite,omitempty"`
	// Hits is every hit landed in the last few turn resolutions (see
	// HitView's doc for the coalescing/dedupe contract) — the per-hit
	// crit/glance moments the HP deltas alone can't express (#114).
	Hits []HitView `json:"hits"`
	// WorldID identifies this running world instance — a random hex string
	// minted once at world creation and persisted in the snapshot (so a
	// restored world is still considered the SAME world). It never changes
	// while the process/snapshot lineage is unbroken, and rides every turn
	// bundle so a client can tell a genuine world reset (a restart with no
	// matching snapshot, or a fresh world under a different snapshot lineage)
	// from an ordinary reconnect: if a bundle's WorldID differs from the
	// first one this client ever saw, the world underneath it changed (item
	// 4, playtest feedback batch 3).
	WorldID string `json:"worldId"`
}

// HitView is one landed hit from a recent turn resolution, riding the turn
// bundle so the client can render per-hit combat moments (#114) — most
// importantly whether the hit was a crit (an attacker-side chance-conditioned
// damage multiplier fired: elf passive, Misericorde, Duelist's Saber) or a
// glance (a defender-side chance-conditioned reduction fired: the Rogue
// passive — a halved hit, never a miss; see docs/game-identity.md for why the
// vocabulary is crit/glance, never miss/dodge). Purely cosmetic: Amount is
// the same damage already reflected in the entities' HP — the client must
// never apply it again.
//
// Turn is the resolution that produced the hit. The server keeps hits from
// the last few resolutions in every bundle (SSE ticks coalesce — a slow
// client skips intermediate bundles), so a client renders only hits with
// Turn greater than the last bundle it processed and ignores the rest.
type HitView struct {
	Turn       int64 `json:"turn"`
	AttackerID int64 `json:"attackerId"`
	VictimID   int64 `json:"victimId"`
	Amount     int   `json:"amount"`
	Crit       bool  `json:"crit"`
	Glance     bool  `json:"glance"`
	// Fatal marks the blow that killed the victim (#298): the last hit it took
	// on the turn its HP reached zero.
	//
	// It is on the wire because a client CANNOT derive death. An entity that
	// vanishes from a bundle either died or walked past the interest radius
	// (#289), and those look identical from outside — so a client guessing
	// would play a death sound at whoever wandered off. The server is the only
	// side that knows.
	Fatal bool `json:"fatal"`
}

// QuestState is a quest's lifecycle stage on the board.
type QuestState string

// The quest lifecycle. Completed quests stay completed — the board depletes
// (repeatable quests arrive with continuous monster spawning, later).
const (
	QuestAvailable QuestState = "available"
	QuestTaken     QuestState = "taken"
	QuestCompleted QuestState = "completed"
)

// QuestView is one quest on the board as the client sees it. The whole board
// (~6 rows) rides every turn bundle (full-snapshot philosophy); the client
// picks out its own quest by holder id.
type QuestView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Kind is "kill" (slay TargetN monsters) or "reach" (stand on GoalHex).
	Kind     string     `json:"kind"`
	TargetN  int        `json:"targetN"`
	GoalHex  Hex        `json:"goalHex"`
	Progress int        `json:"progress"`
	RewardXP int        `json:"rewardXp"`
	State    QuestState `json:"state"`
	// The holder when taken: at most one of these is non-zero.
	HolderEntityID int64 `json:"holderEntityId"`
	HolderPartyID  int64 `json:"holderPartyId"`
}

// SkillView is one skill as the wire shows it (#124) — NEAR-SIGHTED by
// construction: the server sends only skills the viewer has LEARNED or can
// learn right now, so a locked skill never reaches the client and the tree
// cannot leak even by accident.
type SkillView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Tree is one of the three tree names ("class"/"adventure"/"survival").
	Tree string `json:"tree"`
	// Stats are the rendered stat lines (#171); Flavor is the authored lore
	// line. Mechanical text is never authored beside a card.
	Stats  []StatView `json:"stats"`
	Flavor string     `json:"flavor"`
	// Learned distinguishes an owned skill from one currently learnable.
	Learned bool `json:"learned"`
	// Active-skill fields (#161/#185): Active marks a triggerable skill;
	// CooldownTurns and RangeHex are its static profile; TurnsUntilReady is
	// this player's live cooldown (0 = ready). All zero for a passive.
	Active          bool `json:"active"`
	CooldownTurns   int  `json:"cooldownTurns"`
	RangeHex        int  `json:"rangeHex"`
	TurnsUntilReady int  `json:"turnsUntilReady"`
	// Aim is one of the SkillAim* values and tells the client WHICH targeting
	// flow this active needs (#300) — self-cast, a hex, or another entity.
	// Empty for a passive.
	//
	// It is on the wire rather than derived client-side because the aim is a
	// property of the skill's behaviour kind, which is server content: a
	// client guessing it (RangeHex == 0 means self-cast, say) would be a
	// second, divergent implementation of a rule that already exists.
	Aim string `json:"aim"`
}

// Active-skill targeting modes (#300) — the values of SkillView.Aim.
const (
	// SkillAimSelf fires the moment it is triggered: no targeting step, no
	// map click. A "click yourself" flow would be a worse version of pressing
	// the button.
	SkillAimSelf = "self"
	// SkillAimHex arms, and the next map click is the target hex.
	SkillAimHex = "hex"
	// SkillAimEntity arms, and the next map click must land on a hostile —
	// the entity's id is what gets sent, not the hex.
	SkillAimEntity = "entity"
)

// StatView is one rendered stat line (#171) — "+50% Chaos Resistance",
// "×2 Damage vs Adjacent". Derived server-side from the item's rule cards, so
// the text and the mechanic can never disagree; the client only draws it.
type StatView struct {
	Text string `json:"text"`
	// Drawback marks a stat that makes its holder WORSE (Iron Plate Armor's
	// +25% Aggro Range), so the client can style it apart. Sign alone cannot
	// say: +25% Aggro Range is bad, +5% XP is good.
	Drawback bool `json:"drawback"`
}

// ItemView is one owned item as the client sees it: display stats plus
// whether it currently sits in its slot. The numbers ride the wire so the
// client never compiles against item content.
type ItemView struct {
	ID    int64  `json:"id"`
	DefID string `json:"defId"`
	Name  string `json:"name"`
	// Type is the item's itemType (the ItemType* consts above) — the equip
	// slot this item occupies or would occupy (hand name for weapons; the
	// slot key equals the type for armor/jewelry; consumables have no slot).
	Type string `json:"type"`
	// Tags names which attacks fire a weapon (WeaponTagMelee/Ranged/Magic);
	// empty for a non-weapon item.
	Tags []string `json:"tags"`
	// DamageType is the DamageType* a weapon deals (#92) — what resistances
	// and vulnerabilities key on; empty for a non-weapon item.
	DamageType string `json:"damageType"`
	// TwoHanded is true for a weapon that occupies main-hand AND locks
	// off-hand; always false for a non-weapon item.
	TwoHanded bool `json:"twoHanded"`
	Damage    int  `json:"damage"`
	RangeHex  int  `json:"rangeHex"`
	AoERadius int  `json:"aoeRadius"`
	// Stats are the rendered stat lines (#171), in display order.
	Stats []StatView `json:"stats"`
	// Flavor is the item's authored lore ("Fantasy") line; empty for items
	// without lore. Cosmetic only — flavor text in the inventory tooltip.
	Flavor   string `json:"flavor"`
	Equipped bool   `json:"equipped"`
	// Count is the stack size for a consumable backpack stack (1..ItemStackCap);
	// always 1 for gear.
	Count int `json:"count"`
}

// GroundItemView is one dropped stack lying on the map, waiting to be picked
// up (IntentPickup). ID is the representative item instance id (stable client
// key, and the id a pickup intent names). Type feeds the client's pickup
// prompt (name + type); Count is the stack size (a consumable stack drops
// whole — 1..ItemStackCap; always 1 for gear). The detail fields mirror
// ItemView so the pickup modal can show what an item IS before you take it
// (#139) — same meanings as on ItemView.
type GroundItemView struct {
	ID    int64  `json:"id"`
	Hex   Hex    `json:"hex"`
	DefID string `json:"defId"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Count int    `json:"count"`
	// Detail fields (#139) — identical meanings to ItemView's.
	Tags       []string   `json:"tags"`
	DamageType string     `json:"damageType"`
	TwoHanded  bool       `json:"twoHanded"`
	Damage     int        `json:"damage"`
	RangeHex   int        `json:"rangeHex"`
	AoERadius  int        `json:"aoeRadius"`
	Desc       string     `json:"desc"`
	Stats      []StatView `json:"stats"`
	Flavor     string     `json:"flavor"`
}

// Entity is one thing standing on the map: a player or a monster.
type Entity struct {
	ID       int64  `json:"id"`
	Hex      Hex    `json:"hex"`
	Kind     string `json:"kind"`
	Class    string `json:"class"`
	Species  string `json:"species"`
	HP       int    `json:"hp"`
	MaxHP    int    `json:"maxHp"`
	InCombat bool   `json:"inCombat"`
	// Reach is a MONSTER's attack range in hexes (0 = melee) — the one threat
	// stat shown to a player before contact (#201). Its damage and damage type
	// are deliberately NOT sent: those are learned by being hit (near-sighted
	// combat). Players send 0 (their own reach is their equipped weapon's,
	// already known to them).
	Reach int `json:"reach"`
	// XP is server-authoritative; monsters send 0, players send their actual XP.
	XP int `json:"xp"`
	// Level is server-authoritative; monsters send 1, players send their actual level.
	Level int `json:"level"`
	// Name is the entity's display name: the player's chosen name for a
	// player, or the monster kind's display name ("Wolf", "Dragon", ...)
	// for a monster (milestone 6c — previously always empty for monsters).
	Name string `json:"name"`
	// PartyID groups players into a party (≥2 members share a non-zero id);
	// 0 means solo. Monsters are always 0. The roster and on-map partymate
	// coloring are derived client-side by grouping entities on this.
	PartyID int64 `json:"partyId"`
	// Items is the entity's owned items. Players only; monsters send none.
	Items []ItemView `json:"items"`
	// Skills is the viewer's OWN learned + currently-learnable skills (#124);
	// empty on every other entity, since skills are own-only on the wire.
	Skills []SkillView `json:"skills"`
	// SkillPoints is the viewer's own unspent bank; zero on other entities.
	SkillPoints int `json:"skillPoints"`
	// Energy / MaxEnergy are the viewer's OWN action-currency pool (#322);
	// zero on every other entity. Own-only rather than public like HP: another
	// player's remaining energy is build information, and nothing on screen
	// needs it.
	Energy    int `json:"energy"`
	MaxEnergy int `json:"maxEnergy"`
	// HealthPotionReadyIn / EnergyPotionReadyIn are the turns until each
	// always-available draught can be drunk again (#322); 0 means ready. The
	// globes render these, and they are own-only for the same reason the pools
	// are.
	HealthPotionReadyIn int `json:"healthPotionReadyIn"`
	EnergyPotionReadyIn int `json:"energyPotionReadyIn"`
	// EvadeReadyIn is how many turns until the viewer may evade again (#322);
	// 0 means ready now. Own-only, like Skills.
	//
	// It needs its own field precisely BECAUSE evade is universal: a mechanic
	// everyone has is not a learnable skill, so it carries no SkillView, and
	// the HUD ball would otherwise have no cooldown to render.
	EvadeReadyIn int `json:"evadeReadyIn"`
	// MonsterKind is the monster-kind registry id ("wolf", "dragon", ...);
	// empty for players. Drives per-kind client rendering (color/glyph).
	MonsterKind string `json:"monsterKind"`
}

// JoinRequest is the body of POST /api/join. A returning client sends its
// stored token to reclaim its entity; an empty token means "new player".
type JoinRequest struct {
	Token string `json:"token"`
	// Name is the player's display name (chat sender label). Required for a
	// new player (non-empty after trim, at most MaxNameLen runes); ignored on
	// a reclaim (known token) — an existing entity already has its name.
	Name string `json:"name"`
	// Class is the player's chosen class. Required for a new player (empty
	// token or unknown token): must be ClassFighter, ClassRogue, or
	// ClassMage. Ignored on a reclaim (known token) — an existing entity
	// already has its class.
	Class string `json:"class"`
	// Species is the player's chosen species. Required for a new player (empty
	// token or unknown token): must be SpeciesHuman, SpeciesElf, or
	// SpeciesDwarf. Ignored on a reclaim (known token) — an existing entity
	// already has its species.
	Species string `json:"species"`
}

// JoinResponse identifies the caller's entity. The token is the bearer
// secret for submitting intents — the "name + secret link" auth of the plan,
// minus the name for now.
type JoinResponse struct {
	EntityID int64  `json:"entityId"`
	Token    string `json:"token"`
	Hex      Hex    `json:"hex"`
}

// TokenCheckRequest is the body of POST /api/token-check: the stored token the
// client is about to reclaim with. A POST with a body, never a GET with a query
// parameter — tokens are bearer secrets, and a URL is the one place they must
// never appear (logs, referrers, history).
type TokenCheckRequest struct {
	Token string `json:"token"`
}

// TokenCheckResponse reports whether a token would still reclaim a character.
//
// Known is true for a live entity AND for an archived one — both are reclaimable
// via Join, so both mean "show the returning-player card". False means the world
// has never heard of this token (most plausibly it was reset out from under the
// client), which is what lets the start screen offer the creation form FIRST
// instead of a welcome-back card that a Continue click would only disprove.
type TokenCheckResponse struct {
	Known bool `json:"known"`
}

// IntentRequest is the body of POST /api/intent: "walk to Target" or "attack Target".
// Target is any walkable hex (for move) or target hex (for attack), not just a
// neighbor — the server pathfinds from the entity's current position and walks the
// route one hex per turn. A keyboard step is simply a Target one hex away. One
// intent per entity per turn; a later submission in the same input window replaces
// the earlier intent.
type IntentRequest struct {
	EntityID int64  `json:"entityId"`
	Token    string `json:"token"`
	// Kind is the intent type. Required: one of the Intent* constants (move,
	// attack, equip, unequip, drop, pickup, drink).
	Kind string `json:"kind"`
	// Target is the destination/aim hex: a move's walkable goal, an attack's
	// target hex (#271).
	// to a server-chosen safe hex.
	Target Hex `json:"target"`
	// ItemID names the OWNED item an inventory action targets. Equip, unequip,
	// drop and drink intents.
	ItemID int64 `json:"itemId"`
	// SkillID names the skill a learn-skill intent spends a point on (#124).
	SkillID string `json:"skillId"`
	// GroundItemID names the GROUND item a pickup targets (GroundItemView.ID;
	// it must lie on the player's own hex). Pickup intents only.
	GroundItemID int64 `json:"groundItemId"`
	// TargetEntityID names a single-target ranged attack's victim by entity
	// id instead of a hex (item 7, playtest batch 2): 0 = none (ground-
	// targeted — a mage's AoE cast, whose blast radius makes a hex the
	// natural target). A bow-class attack (aoeRadius 0) sets this instead of
	// relying on Target; the server resolves against the named entity's
	// pre-move hex (#104), so a committed shot tracks a sidestepping or
	// fleeing target by id rather than a stale hex. Attack intents only.
	TargetEntityID int64 `json:"targetEntityId"`
}

// ChatMessage is the payload of an EventChat frame: one line in the global
// channel. Seq is a server-assigned monotonic sequence (a stable client key
// and ordering aid — not a timestamp). Sender is the author's display name.
type ChatMessage struct {
	Seq    int64  `json:"seq"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
	// Recipient addresses this line to ONE entity; 0 (the default) is the
	// global channel every line used before #385. Added for the party decline,
	// which has to reach the inviter and nobody else — a broadcast makes
	// saying no socially expensive, and silence is indistinguishable from
	// being ignored.
	//
	// Enforced SERVER-SIDE: writeChat (internal/server/events.go) never writes
	// the frame to a stream whose viewer is not the recipient, so this is not a
	// "please hide this" flag the client is trusted to honour. The only client
	// that ever sees the field is the one it is addressed to.
	Recipient int64 `json:"recipient,omitempty"`
}

// ChatRequest is the body of POST /api/chat. Token authenticates the sender;
// Text is the message (or a "/command"). The server resolves the sender's
// name and position from the token — the client cannot set them.
type ChatRequest struct {
	Token string `json:"token"`
	Text  string `json:"text"`
}

// ErrorResponse is the JSON body of every non-2xx API response.
type ErrorResponse struct {
	Error string `json:"error"`
}
