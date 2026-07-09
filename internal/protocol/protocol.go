// Package protocol is the single source of truth for the wire between the Go
// server and the TypeScript client. Every exported type and constant here is
// mirrored into client/src/protocol.gen.ts by tygo (`make protocol`); the
// generated file must never be edited by hand.
package protocol

// Turn cadence. One world turn every TurnSeconds: an input window, an instant
// server resolution, then a playback window on the client. Inside a combat
// time bubble the cadence is suspended and turns are action-gated instead.
const (
	// TurnSeconds is the full world-turn period out of combat.
	TurnSeconds = 5
	// InputWindowSeconds is the slice of the turn in which intents are accepted.
	InputWindowSeconds = 3
	// PlaybackSeconds is the client-side animation window after resolution.
	PlaybackSeconds = 2
)

// World rules that both sides need to agree on.
const (
	// CombatRadius is the mutual-line-of-sight distance (in hexes) at which a
	// combat time bubble forms around a player and a hostile.
	CombatRadius = 6
	// StackCap is the maximum number of friendly entities on one hex — sized
	// so a full party fits.
	StackCap = 5
	// MaxChatLen caps a chat message length in runes (defence-in-depth; the
	// client also caps input). MaxNameLen caps a player's display name.
	MaxChatLen = 500
	MaxNameLen = 24
)

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
// IntentMove or IntentAttack.
const (
	IntentMove   = "move"
	IntentAttack = "attack"
)

// Starting/maximum hit points by kind. HP is on the wire from milestone 6.2 so
// the client can show health bars once combat (6.3) starts changing it.
const (
	PlayerMaxHP  = 20
	MonsterMaxHP = 10
)

// MonsterAttackDamage is a monster's flat melee damage per attack. (Player melee
// is per-class weapon damage since 6b.2 — see the class weapon constants below.)
const MonsterAttackDamage = 3

// XP & leveling (milestone 6b.1). Flat curve for now; per-class/species tuning
// is 6b.2/6b.3.
const (
	// XPPerLevel is the XP needed to advance one level.
	XPPerLevel = 100
	// MonsterXP is awarded to every player in the fight when a monster dies —
	// the full amount each, not divided.
	MonsterXP = 20
)

// Per-class base stats (level 1). Level scaling: MaxHP += HPPerLevel * (level - 1);
// weapon damage += DamagePerLevel * (level - 1).
const (
	// FighterMaxHP is the level-1 max HP for Fighter class (tanky melee).
	FighterMaxHP = 30
	// RogueMaxHP is the level-1 max HP for Rogue class (high single-target damage, squishy).
	RogueMaxHP = 16
	// MageMaxHP is the level-1 max HP for Mage class (AoE ranged, squishy).
	MageMaxHP = 14

	// SwordDamage is level-1 damage for the Fighter's close weapon (sword).
	SwordDamage = 4
	// DaggerDamage is level-1 damage for the Rogue's close weapon (dagger).
	DaggerDamage = 7
	// BowDamage is level-1 damage for the Rogue's ranged weapon (bow).
	BowDamage = 6
	// StaffBonkDamage is level-1 damage for the Mage's close weapon (staff bonk).
	StaffBonkDamage = 2
	// StaffMagicDamage is level-1 damage per target for the Mage's ranged weapon (staff magic AoE).
	StaffMagicDamage = 4
	// FistsDamage is level-1 damage for fallback/unarmed attacks.
	FistsDamage = 1

	// BowRange is the maximum hex distance for Rogue bow attacks.
	BowRange = 4
	// MageRange is the maximum hex distance for Mage magic attacks.
	MageRange = 4
	// MageAoERadius is the splash radius in hexes for Mage AoE magic (includes target + neighbors).
	MageAoERadius = 1

	// HPPerLevel is the additional max HP gained per level above 1.
	HPPerLevel = 4
	// DamagePerLevel is the additional damage gained per level above 1.
	DamagePerLevel = 1
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
	// Name is the player's display name; empty for monsters.
	Name string `json:"name"`
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
	// Kind is the intent type. Required: must be IntentMove ("move") or
	// IntentAttack ("attack").
	Kind   string `json:"kind"`
	Target Hex    `json:"target"`
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
