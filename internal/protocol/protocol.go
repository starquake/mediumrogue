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
// impassable but open; grass and forest are walkable.
const (
	TerrainGrass  Terrain = "grass"
	TerrainForest Terrain = "forest"
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
	// IntentDrink drinks one unit of an owned consumable stack
	// (IntentRequest.ItemID): applies the def's heal (clamped to max HP) and
	// decrements the stack; an emptied stack frees its backpack entry.
	IntentDrink = "drink"
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

// Starting/maximum hit points by kind. HP is on the wire from milestone 6.2 so
// the client can show health bars once combat (6.3) starts changing it.
// MonsterMaxHP is superseded by per-kind maxHP (internal/game's monsterDef
// registry, milestone 6c) — wolf's entry carries this exact value forward —
// but stays here as the historical baseline several tests still pin against.
const (
	PlayerMaxHP  = 20
	MonsterMaxHP = 10
)

// RegenPerTurn is the HP a player passively recovers each WORLD-domain turn
// resolution while out of combat (bubbleID == 0) and below max HP — the
// passive recovery layer (plan §9). It kills the inverted incentive where
// dying (a full-HP respawn) was the only way to heal: standing around out of
// a fight now tops you up too, slowly. Monsters never regen; a bubbled player
// (mid-fight) does not either — being in a fight means no regen.
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
	// HumanXPBonusPercent is the XP gain multiplier for Human species (e.g. +50%).
	HumanXPBonusPercent = 50
	// ElfCritChancePercent is the percent base crit chance for Elf species.
	ElfCritChancePercent = 20
	// ElfCritMultiplier is the damage multiplier for Elf crits.
	ElfCritMultiplier = 2
	// DwarfDamageReduction is the flat damage reduction per attack for Dwarf species.
	DwarfDamageReduction = 1
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
	// Entities is every entity in the world, sorted by ID.
	Entities []Entity `json:"entities"`
	// Bubbles is every active combat time bubble in the world; a client filters
	// to the one containing its own entity.
	Bubbles []BubbleView `json:"bubbles"`
	// Quests is the whole quest board, sorted by ID.
	Quests []QuestView `json:"quests"`
	// GroundItems is every dropped item currently lying on the map.
	GroundItems []GroundItemView `json:"groundItems"`
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
	// Desc is the authored human-readable rule text ("+3 vs targets below
	// half HP"); empty for rule-less items.
	Desc string `json:"desc"`
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
	Tags       []string `json:"tags"`
	DamageType string   `json:"damageType"`
	TwoHanded  bool     `json:"twoHanded"`
	Damage     int      `json:"damage"`
	RangeHex   int      `json:"rangeHex"`
	AoERadius  int      `json:"aoeRadius"`
	Desc       string   `json:"desc"`
	Flavor     string   `json:"flavor"`
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
	Kind   string `json:"kind"`
	Target Hex    `json:"target"`
	// ItemID names the OWNED item an inventory action targets. Equip,
	// unequip, drop, and drink intents only.
	ItemID int64 `json:"itemId"`
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
