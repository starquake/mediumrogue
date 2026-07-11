package game

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	mrand "math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// combatLogKey is the slog attribute key every combat-resolution log line
// carries (slog.Info("combat", "event", ...)) — the seed of the milestone-12
// analytics log (docs/roguelike-mp-plan.md §12). Filter on this key (msg ==
// "combat") to isolate the sim's structured event stream from ordinary
// server logs. combatEvent* names the "event" attribute's fixed vocabulary.
const combatLogMsg = "combat"

const (
	combatEventMove   = "move"
	combatEventAttack = "attack"
	combatEventFizzle = "fizzle"
	combatEventDeath  = "death"
	combatEventPickup = "pickup"
	combatEventDrop   = "drop"
	combatEventDrink  = "drink"
	combatEventXP     = "xp_award"
)

// identityLogMsg is the slog message every identity-lifecycle log line
// carries (slog.Info("identity", "event", ...)) — the same filterable
// convention as combatLogMsg, added for item 7 of playtest feedback batch 3
// so the next cross-machine "players swapped" report gets diagnosed from
// server logs instead of hypothesized. identityEvent* names the "event"
// attribute's fixed vocabulary; the sixth event, "snapshot-restore", is
// emitted from RestoreState (snapshot.go).
const identityLogMsg = "identity"

const (
	identityEventJoinNew         = "join-new"
	identityEventJoinReclaim     = "join-reclaim"
	identityEventJoinRestore     = "join-restore"
	identityEventJoinRejected    = "join-rejected"
	identityEventSweepArchive    = "sweep-archive"
	identityEventSnapshotRestore = "snapshot-restore"
)

// tokenPrefixLen is how many leading characters of a bearer token identity
// log lines carry — enough to correlate a client across joins/sweeps in the
// logs, never the full secret (a full token in a log file would be a
// character-theft vector; the log is not the trust boundary the VPS disk
// is).
const tokenPrefixLen = 8

// tokenPrefix truncates token for audit logging — see tokenPrefixLen.
func tokenPrefix(token string) string {
	if len(token) <= tokenPrefixLen {
		return token
	}

	return token[:tokenPrefixLen]
}

// Intent validation errors, mapped to HTTP statuses by the API layer.
var (
	// ErrUnauthorized covers unknown entities and bad tokens alike, so a
	// caller cannot probe which entity IDs exist.
	ErrUnauthorized = errors.New("unknown entity or bad token")
	// ErrNotWalkable rejects water, rock, and off-map destinations.
	ErrNotWalkable = errors.New("target is not walkable")
	// ErrNoPath rejects a walkable destination with no route from the
	// entity's current hex (walled off by impassable terrain).
	ErrNoPath = errors.New("no path to target")
	// ErrWorldFull means no walkable hex has room for another entity — only
	// plausible if joins vastly outnumber the map's capacity.
	ErrWorldFull = errors.New("world is full: no walkable hex with room left")
	// ErrNoRangedWeapon rejects an attack intent from a class with no ranged
	// weapon (the Fighter, or any classless entity).
	ErrNoRangedWeapon = errors.New("class has no ranged weapon")
	// ErrOutOfRange rejects an attack intent whose target is farther than the
	// entity's ranged-weapon reach.
	ErrOutOfRange = errors.New("target is out of range")
	// ErrAttackTargetNotFound rejects an entity-targeted attack intent
	// (item 7) naming an entity id that does not exist or is already dead.
	ErrAttackTargetNotFound = errors.New("target entity not found")
	// ErrAttackTargetNotHostile rejects an entity-targeted attack intent
	// (item 7) naming a same-faction entity — ranged attacks only ever hit
	// hostiles.
	ErrAttackTargetNotHostile = errors.New("target is not hostile")
	// ErrItemNotOwned rejects an equip intent naming an item instance id the
	// entity does not own.
	ErrItemNotOwned = errors.New("item not owned")
	// ErrWrongClass rejects an equip intent naming an item whose class does not
	// match the entity's class.
	ErrWrongClass = errors.New("item is for a different class")
	// ErrInvalidClass rejects a Join for a new entity whose Class is not one of
	// ClassFighter, ClassRogue, ClassMage.
	ErrInvalidClass = errors.New("invalid class")
	// ErrInvalidSpecies rejects a Join for a new entity whose Species is not one
	// of SpeciesHuman, SpeciesElf, SpeciesDwarf.
	ErrInvalidSpecies = errors.New("invalid species")
	// ErrInvalidName rejects an empty or over-long display name at join.
	ErrInvalidName = errors.New("invalid name")
	// ErrInvalidIntentKind rejects a SubmitIntent whose Kind is not one of
	// the protocol.Intent* constants.
	ErrInvalidIntentKind = errors.New("invalid intent kind")
	// ErrBackpackFull rejects an inventory action that needs a free backpack
	// entry (pickup with no mergeable stack, unequip, equip-toggle-off) when
	// none exists. The exact wording is the client-facing feedback the spec
	// pins ("backpack full — drop something first").
	ErrBackpackFull = errors.New("backpack full — drop something first")
	// ErrItemNotEquipped rejects an unequip intent naming an owned item that
	// is not currently equipped.
	ErrItemNotEquipped = errors.New("item is not equipped")
	// ErrNotDrinkable rejects a drink intent naming a non-consumable item.
	ErrNotDrinkable = errors.New("item is not drinkable")
	// ErrNoSuchGroundItem rejects a pickup intent naming a ground item that
	// is not lying on the player's own hex (stale id, or an item elsewhere).
	ErrNoSuchGroundItem = errors.New("no such item here")
)

// tokenBytes sizes the bearer token: 16 random bytes = 128 bits.
const tokenBytes = 16

// worldIDBytes sizes World.worldID: 8 random bytes = 64 bits, hex-encoded —
// plenty of entropy to tell world instances apart (it is an identity signal,
// not a secret) while staying short in logs/turn bundles.
const worldIDBytes = 8

// newWorldID mints a random hex worldID for a freshly constructed World (see
// World.worldID's doc). A failed crypto read leaves a zero-valued (all
// zeros, still a valid non-empty hex string) id — worse entropy, never a
// crash, matching NewWorld's existing worldSeed fallback above.
func newWorldID() string {
	buf := make([]byte, worldIDBytes)
	_, _ = rand.Read(buf)

	return hex.EncodeToString(buf)
}

// entity is the server-side entity record. The wire shape is
// protocol.Entity; the token never leaves this package except via Join.
type entity struct {
	id    int64
	hex   protocol.Hex
	token string
	kind  string
	// monsterKind is the monster-kind registry id (content.go's monsterDefs,
	// e.g. "wolf"); empty for players. Set at spawn (SpawnMonsters,
	// SpawnMonsterAt, PlaceMonsterForTest); kindOf resolves it back to the
	// def that carries this entity's stats, loot table, and aggro radius.
	monsterKind string
	// name is the player's display name (chat sender label), validated and set
	// at Join; empty for monsters.
	name string
	// partyID is the party this entity belongs to, or 0 for none. Assigned by
	// PartyAccept, cleared by PartyLeave (or the disconnect sweep); a party of
	// fewer than two members dissolves (see leavePartyLocked).
	partyID int64
	// class is the player's class (protocol.ClassFighter/Rogue/Mage), validated
	// and set at Join; empty for monsters. It selects the entity's base HP
	// (class.go's maxHPFor) and, via classDefaultIDs, the starting items
	// grantDefaultsLocked equips at Join.
	class string
	// species is the player's species (protocol.SpeciesHuman/Elf/Dwarf), validated
	// and set at Join; empty for monsters. It selects the passive rule cards
	// (speciesCards in content.go: human XP bonus, elf crit, dwarf damage
	// reduction) the pipeline (rules.go) applies at combat/XP events.
	species string
	hp      int
	maxHP   int
	// xp is the entity's cumulative experience (players only; monsters stay 0).
	// Level is derived from it via levelFor; on death it falls to the current
	// level's floor (levelFloorXP).
	xp int
	// path is the remaining route (steps excluding the current hex), consumed
	// one hex per turn. Empty when the entity is idle.
	path []protocol.Hex
	// attackTarget is a pending GROUND-targeted ranged-attack hex for this
	// turn, or nil for none — a mage's AoE cast, ground-targeted by nature
	// (the blast radius centers on a hex, not a victim). Set by an "attack"
	// intent with no TargetEntityID (which clears path — you shoot, you
	// don't move), resolved and cleared in the attack phase. Mutually
	// exclusive with attackTargetEntity; see queueAttackLocked.
	attackTarget *protocol.Hex
	// attackTargetEntity is a pending single-target (bow) ranged attack's
	// victim, named by entity id (item 7, playtest batch 2) — 0 for none, or
	// for a ground-targeted attack (see attackTarget). Resolution re-aims at
	// this entity's CURRENT (post-move) hex rather than trusting the hex
	// captured at submit time, so a sidestepping or retreating victim is
	// tracked: hits if still within the weapon's range from the shooter's
	// own post-move hex, else fizzles (resolveEntityTargetedLocked).
	attackTargetEntity int64
	// bubbleID is the combat bubble this entity belongs to, or 0 for the world
	// domain. Recomputed from positions every turn by recomputeBubblesLocked.
	bubbleID int64
	// streams is the number of live event streams currently open for this player
	// (multiple browser tabs → >1). Players only; monsters leave it 0 and are
	// never swept (they have no token). Bumped by StreamOpened, dropped by
	// StreamClosed.
	streams int
	// disconnectedAt is when this player's last event stream closed (or its join
	// time, before its first stream opens): the start of its removal-grace clock.
	// Only consulted while streams == 0. Players only.
	disconnectedAt time.Time
	// equipped is the entity's worn/wielded gear, keyed by slot (the 8 typed
	// slots of the inventory-slots milestone: the six universal body slots
	// plus the class's two class-shaped weapon slots — slot keys are itemType
	// strings, see slotForType/weaponSlotsFor in items.go). Players only;
	// monsters own no items and fight with their kind's own claws profile
	// (monsterDef.claws). Granted at Join by grantDefaultsLocked; gear
	// survives death (never cleared by respawn). May be nil on a
	// monster/zero-value fixture — equippedDefIn treats nil as all-empty.
	equipped map[string]itemInstance
	// backpack is the entity's protocol.BackpackSize carry entries: one gear
	// instance or one consumable stack per entry (backpackEntry, items.go).
	// Every owned item lives in exactly one of equipped or backpack.
	backpack [protocol.BackpackSize]backpackEntry
	// pending is the inventory action (equip/unequip/drop/pickup/drink)
	// queued by an intent submitted inside a combat bubble — the action
	// becomes this turn's committed action (see commitItemActionLocked,
	// inventory.go) instead of applying immediately. The zero value means no
	// action is queued. Applied and cleared by resolveCombatLocked's pending
	// pass; also cleared on death (resolveDeathsLocked) as a safety net for
	// resolution paths that skip that pass (e.g. the ResolveCombatOnlyForTest
	// test bridge).
	pending pendingItemAction
}

// World is the authoritative game state: the map, every entity, and each
// entity's queued walk path. One World per process; all access is serialized
// through its mutex (15 players — contention is not a concern, simplicity is).
type World struct {
	interval time.Duration
	ticks    *hub.Hub

	// combatPatience is how long a combat bubble waits for an unready player
	// before auto-resolving its turn; bubblePoll is the control loop's polling
	// cadence for checking bubble readiness and world-tick elapse. Both have
	// sensible defaults set in NewWorld; milestone 6.4 Task 4 threads them from
	// config (a clean seam — they are not yet in NewWorld's signature).
	combatPatience time.Duration
	bubblePoll     time.Duration
	// disconnectGrace is how long a disconnected player's entity lingers before
	// the world sweeps it. Set from config in NewWorld.
	disconnectGrace time.Duration
	// now is the clock, injectable in tests so the two-clock gating can be driven
	// deterministically without real time. Defaults to time.Now.
	now func() time.Time
	// logger receives the structured "combat" event stream (moves, bumps,
	// ranged hits/fizzles, deaths, kill-XP awards, pickups) — the seed of the
	// milestone-12 analytics log. Defaults to slog.Default() in NewWorld;
	// override via SetLogger (mirrors SetAnnounce).
	logger *slog.Logger

	mu   sync.Mutex
	turn int64
	// lastWorldTick is when the world domain last resolved, for the control
	// loop's world-tick accounting. Read/written only under mu.
	lastWorldTick time.Time
	terrain       map[protocol.Hex]protocol.Terrain
	worldMap      protocol.MapResponse
	// radius is the world's hex radius (from Config.WorldRadius), the loop
	// bound for spawnHexLocked's outward spiral.
	radius int
	// worldSeed is the procedural-map generation seed (Config.WorldSeed),
	// kept (in addition to feeding GenerateMap/generateQuests at
	// construction) so a snapshot restore can gate on it: a snapshot taken
	// under a different seed or radius describes a different map, and
	// RestoreState refuses to load it rather than silently mismatching
	// terrain against persisted positions. See snapshot.go.
	worldSeed uint64
	// worldID identifies this running world instance to clients (item 4,
	// playtest feedback batch 3) — a random hex string minted once in
	// NewWorld and, unlike worldSeed/radius, PERSISTED in the snapshot (see
	// snapshot.go): a restored world keeps its predecessor's worldID because
	// it IS the same world, not a new one. Rides every TurnEvent so a client
	// can distinguish an ordinary reconnect from a genuine world reset (a
	// restart with no matching snapshot).
	worldID string
	// spawnable is the origin-reachable walkable region (BFS from origin over
	// walkable tiles, computed once at construction) — spawnHexLocked only
	// places players on hexes in this set, so a spawn can never land in a
	// walkable pocket cut off from the origin by water/rock.
	spawnable map[protocol.Hex]bool
	entities  map[int64]*entity
	byToken   map[string]*entity
	nextID    int64
	// pendingInvites maps an invitee entity id to the inviter's entity id, set by
	// PartyInvite and consumed (or purged by the disconnect sweep) before it is
	// acted on. At most one pending invite per invitee — a second invite
	// overwrites the first.
	pendingInvites map[int64]int64
	// nextPartyID mints party ids; 0 is reserved for "no party".
	nextPartyID int64
	// bubbles are the active combat time bubbles, keyed by id. Rebuilt each turn
	// by recomputeBubblesLocked; ids carry across recomputes for stable gating.
	bubbles      map[int64]*bubble
	nextBubbleID int64
	// seed is the world's tie-break RNG seed, minted once at construction. Each
	// turn's move-resolution shuffle uses a PCG seeded from (seed, turn) — the
	// turn selects the stream — so it's reproducible given the world + turn but
	// unpredictable to players (they don't know the world seed).
	seed int64
	// quests is the quest board, generated once at construction from the world
	// seed (deterministic given seed + map). Fixed size and id-sorted by
	// construction; entries mutate in place as they are taken/progressed/completed.
	quests []*quest
	// announce is the chat hook for in-resolution quest events (completion,
	// auto-abandon). Defaults to a no-op so tests without chat wiring pass; set
	// via SetAnnounce.
	announce func(sender, text string)
	// groundItems is every dropped item currently lying on the map, keyed by
	// hex. Populated by dropLootLocked (a slain monster's death hex), drained
	// by pickupLocked (a player walking onto the hex takes everything there).
	// Instance ids are minted from the same nextID sequence as entities and
	// owned items — unique across the whole world.
	groundItems map[protocol.Hex][]itemInstance
	// archive holds characters removed by the disconnect sweep (or loaded
	// from a snapshot that was never re-claimed live): identity, XP, and
	// gear, keyed by token. sweepDisconnectedLocked populates an entry in
	// place of discarding a player's progress; Join consumes (deletes) the
	// entry on a rejoin with that token, restoring a fresh entity from it.
	// Never touched for monsters (no token). Party/quest membership is NOT
	// archived — that is session-scoped social state, not progression (see
	// sweepDisconnectedLocked).
	archive map[string]characterRecord
}

// characterRecord is what the disconnect sweep archives from a player entity
// before deleting it: identity, progression, and gear. Everything else about
// a live entity (hex, hp, path, bubble membership, streams) is transient by
// design — a restored character is as if freshly joined, but with its
// progression and gear intact. See World.archive and sweepDisconnectedLocked.
type characterRecord struct {
	name, class, species string
	xp                   int
	equipped             map[string]itemInstance
	backpack             [protocol.BackpackSize]backpackEntry
}

// archiveLocked captures e's character record for World.archive. The
// equipped map is cloned — the record must stay a stable snapshot even
// though the source entity is deleted right after (and a restored entity
// gets its own map, never one shared with a lingering record). Callers
// hold w.mu.
func archiveLocked(e *entity) characterRecord {
	equipped := make(map[string]itemInstance, len(e.equipped))
	maps.Copy(equipped, e.equipped)

	return characterRecord{
		name: e.name, class: e.class, species: e.species,
		xp:       e.xp,
		equipped: equipped, backpack: e.backpack,
	}
}

// NewWorld builds the world from a procedurally generated map (GenerateMap,
// seeded by worldSeed/radius — see internal/game/worldgen.go).
// combatPatience is the AFK fallback before a combat bubble resolves without a
// straggler; bubblePoll is the control-loop cadence (see Run); disconnectGrace
// is how long a disconnected player's entity lingers before the world sweeps
// it. Run must be started for turns to advance.
func NewWorld(
	interval, combatPatience, bubblePoll, disconnectGrace time.Duration,
	worldSeed uint64, radius int, ticks *hub.Hub,
) *World {
	worldMap := GenerateMap(worldSeed, radius)

	terrain := make(map[protocol.Hex]protocol.Terrain, len(worldMap.Tiles))
	for _, t := range worldMap.Tiles {
		terrain[t.Hex] = t.Terrain
	}

	var seedBuf [8]byte
	// A failed crypto read leaves a zero seed — still valid, just less random.
	_, _ = rand.Read(seedBuf[:])
	//nolint:gosec // a random world seed can be any 64-bit value; the sign is irrelevant.
	seed := int64(binary.BigEndian.Uint64(seedBuf[:]))

	worldID := newWorldID()

	return &World{
		interval:        interval,
		ticks:           ticks,
		combatPatience:  combatPatience,
		bubblePoll:      bubblePoll,
		disconnectGrace: disconnectGrace,
		now:             time.Now,
		logger:          slog.Default(),
		terrain:         terrain,
		worldMap:        worldMap,
		radius:          radius,
		worldSeed:       worldSeed,
		worldID:         worldID,
		spawnable:       reachableWalkable(worldMap),
		entities:        make(map[int64]*entity),
		byToken:         make(map[string]*entity),
		pendingInvites:  make(map[int64]int64),
		bubbles:         make(map[int64]*bubble),
		seed:            seed,
		quests:          generateQuests(worldSeed, worldMap),
		announce:        func(string, string) {},
		groundItems:     make(map[protocol.Hex][]itemInstance),
		archive:         make(map[string]characterRecord),
	}
}

// Map returns the immutable world map.
func (w *World) Map() protocol.MapResponse {
	return w.worldMap
}

// SetLogger installs the structured "combat" event sink (mirrors
// SetAnnounce). A nil logger is ignored — callers that don't want the
// stream keep NewWorld's slog.Default() rather than crashing on a nil
// dereference the next time combat resolves.
func (w *World) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}

	w.logger = logger
}

// Run advances the world until ctx is canceled, on a single control loop that
// runs both clocks (see the two-clock model in docs). Every bubblePoll it (a)
// resolves the world domain if a full interval has elapsed and (b) resolves any
// combat bubble whose players have all locked in or whose patience has expired.
// A resolution announces on the tick hub. Blocks; run in a goroutine.
func (w *World) Run(ctx context.Context) {
	// Snapshot the clock and cadence under the lock at startup: the loop then
	// reads neither field again, so a test (or a future config reload) that
	// mutates them under the lock cannot race this goroutine.
	w.mu.Lock()
	now := w.now
	poll := time.NewTicker(w.bubblePoll)
	w.lastWorldTick = now()
	w.mu.Unlock()

	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			if w.pollTick(now()) {
				w.ticks.Publish()
			}
		}
	}
}

// pollTick runs one control-loop pass under w.mu: a world resolution if the
// interval has elapsed since lastWorldTick, plus every ready-or-expired bubble.
//
// It decides what resolves, and captures each resolution's member set, from the
// state at the START of the pass — before any resolution mutates positions or
// membership. That ordering is load-bearing: a world resolution can walk an
// entity into an existing bubble, and a bubble resolution can let one flee into
// the world; capturing member sets up front (and recomputing bubbles only once,
// at the end) guarantees every entity acts exactly once, never twice and never
// zero times, regardless of how the pass reshuffles domains. Bubbles resolve in
// sorted-id order for reproducibility.
//
// Its first step is the disconnect sweep (before any member set is captured), so
// a player gone past the grace never lands in a resolution this pass. Returns
// whether anything changed — a resolution or a swept removal — so a removal-only
// pass still republishes.
func (w *World) pollTick(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Sweep first, before capturing any member set: a swept entity is then never
	// part of this pass's resolution, and the recompute inside the sweep leaves
	// readyBubbleTurnsLocked reading up-to-date bubbles. A removal alone (no other
	// resolution) still publishes so clients despawn the gone entity.
	swept := w.sweepDisconnectedLocked(now)

	doWorld := now.Sub(w.lastWorldTick) >= w.interval

	turns := w.readyBubbleTurnsLocked(now)

	if !doWorld && len(turns) == 0 {
		return swept
	}

	if doWorld {
		w.resolveWorldTurnLocked(w.domainMembersLocked())
		w.lastWorldTick = now
	}

	for _, bt := range turns {
		w.resolveBubbleTurnLocked(bt.bubble, bt.members, now)
	}

	// Final recompute after this pass's resolutions moved entities. (A sweep
	// above may have already recomputed once — keep this one anyway: positions
	// changed since. recompute is idempotent, so the extra call is harmless.)
	w.recomputeBubblesLocked(now)

	return true
}

// bubbleTurn is one bubble's scheduled resolution: the bubble and the member
// snapshot to resolve it over, both captured before the pass mutates anything.
type bubbleTurn struct {
	bubble  *bubble
	members []*entity
}

// readyBubbleTurnsLocked collects, in sorted-id order, the bubbles that should
// resolve this pass (all players locked in, or patience expired) together with a
// snapshot of each one's members. Callers hold w.mu.
func (w *World) readyBubbleTurnsLocked(now time.Time) []bubbleTurn {
	ids := make([]int64, 0, len(w.bubbles))
	for id := range w.bubbles {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	var turns []bubbleTurn

	for _, id := range ids {
		b := w.bubbles[id]
		if w.bubbleReadyOrExpiredLocked(b, now) {
			turns = append(turns, bubbleTurn{bubble: b, members: w.bubbleMembersLocked(b)})
		}
	}

	return turns
}

// Join returns the entity for token in three orders of preference: (1) a
// LIVE token reclaims its existing entity; (2) an ARCHIVED token (swept for
// disconnection, or loaded from a snapshot and never re-claimed) restores a
// fresh entity from its characterRecord — new spawn hex via the normal
// guarded random spawn, full level-scaled HP, but identity/XP/gear exactly as
// left; (3) an unknown token quietly becomes a NEW player rather than an
// error — the stored identity of a restarted server (pre-archive/snapshot)
// is gone, and the client's right move is always "then give me a fresh
// entity". For a new entity, name is required — non-empty after trimming and
// at most protocol.MaxNameLen runes, else Join returns ErrInvalidName; class
// is required — it must be ClassFighter, ClassRogue, or ClassMage, else Join
// returns ErrInvalidClass, and species must be SpeciesHuman, SpeciesElf, or
// SpeciesDwarf, else ErrInvalidSpecies. For a reclaim or a restore, name,
// class, and species are ignored — the identity already exists.
func (w *World) Join(token, name, class, species string) (protocol.JoinResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok && token != "" {
		// A reclaim is a fresh sign of life: refresh the grace clock so a sweep
		// can't remove the entity in the gap before its reopened stream calls
		// StreamOpened.
		e.disconnectedAt = w.now()

		w.logger.Info(identityLogMsg, "event", identityEventJoinReclaim,
			"id", e.id, "name", e.name, "token_prefix", tokenPrefix(token))

		return protocol.JoinResponse{EntityID: e.id, Token: e.token, Hex: e.hex}, nil
	}

	if rec, ok := w.archive[token]; ok && token != "" {
		return w.restoreArchivedLocked(token, rec)
	}

	name = strings.TrimSpace(name)
	if err := w.validateNewJoinLocked(token, name, class, species); err != nil {
		return protocol.JoinResponse{}, err
	}

	spawn, err := w.spawnHexLocked()
	if err != nil {
		w.logger.Info(identityLogMsg, "event", identityEventJoinRejected,
			"reason", "no_spawn_hex", "name", name, "token_prefix", tokenPrefix(token))

		return protocol.JoinResponse{}, err
	}

	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return protocol.JoinResponse{}, fmt.Errorf("generate token: %w", err)
	}

	maxHP := maxHPFor(class, 1)

	w.nextID++
	e := &entity{
		id: w.nextID, hex: spawn, token: hex.EncodeToString(buf),
		kind: protocol.EntityPlayer, name: name, class: class, species: species, hp: maxHP, maxHP: maxHP,
		// streams starts 0, disconnectedAt at join time: the removal-grace clock
		// runs from the join so a client that joins but never opens a stream is
		// eventually swept. The client opens its stream within ms (StreamOpened).
		disconnectedAt: w.now(),
	}
	w.entities[e.id] = e
	w.byToken[e.token] = e
	w.grantDefaultsLocked(e)

	w.logger.Info(identityLogMsg, "event", identityEventJoinNew,
		"id", e.id, "name", e.name, "class", e.class, "token_prefix", tokenPrefix(e.token))

	return protocol.JoinResponse{EntityID: e.id, Token: e.token, Hex: e.hex}, nil
}

// validateNewJoinLocked checks a NEW player's name/class/species (name
// already trimmed by the caller), logging a "join-rejected" identity audit
// event with the failing reason before returning the matching sentinel.
// Callers hold w.mu.
func (w *World) validateNewJoinLocked(token, name, class, species string) error {
	if !validName(name) {
		w.logger.Info(identityLogMsg, "event", identityEventJoinRejected,
			"reason", "invalid_name", "token_prefix", tokenPrefix(token))

		return ErrInvalidName
	}

	if !validClass(class) {
		w.logger.Info(identityLogMsg, "event", identityEventJoinRejected,
			"reason", "invalid_class", "name", name, "token_prefix", tokenPrefix(token))

		return ErrInvalidClass
	}

	if !validSpecies(species) {
		w.logger.Info(identityLogMsg, "event", identityEventJoinRejected,
			"reason", "invalid_species", "name", name, "token_prefix", tokenPrefix(token))

		return ErrInvalidSpecies
	}

	return nil
}

// restoreArchivedLocked implements Join's archived-token branch: a fresh
// entity built from rec (identity/XP/gear as archived), spawned at a new
// guarded random hex with full level-scaled HP, keeping the same token —
// this IS the character coming back, not a new one. Consumes (deletes) the
// archive entry, since a character lives in exactly one place at a time.
// Callers hold w.mu.
func (w *World) restoreArchivedLocked(token string, rec characterRecord) (protocol.JoinResponse, error) {
	spawn, err := w.spawnHexLocked()
	if err != nil {
		return protocol.JoinResponse{}, err
	}

	level := levelFor(rec.xp)
	maxHP := maxHPFor(rec.class, level)

	w.nextID++
	e := &entity{
		id: w.nextID, hex: spawn, token: token,
		kind: protocol.EntityPlayer, name: rec.name, class: rec.class, species: rec.species,
		xp: rec.xp, hp: maxHP, maxHP: maxHP,
		equipped: rec.equipped, backpack: rec.backpack,
		// Restored as if freshly joined: the removal-grace clock starts now,
		// not at the (long-gone) pre-sweep disconnect time — the spec's pinned
		// risk (see archive_test.go).
		disconnectedAt: w.now(),
	}
	w.entities[e.id] = e
	w.byToken[e.token] = e
	delete(w.archive, token)

	w.logger.Info(identityLogMsg, "event", identityEventJoinRestore,
		"id", e.id, "name", e.name, "token_prefix", tokenPrefix(token))

	return protocol.JoinResponse{EntityID: e.id, Token: e.token, Hex: e.hex}, nil
}

// validName accepts a trimmed, non-empty name of at most
// protocol.MaxNameLen runes.
func validName(name string) bool {
	n := utf8.RuneCountInString(name)

	return n > 0 && n <= protocol.MaxNameLen
}

// SenderFor resolves a chat sender from their token: their display name and
// current authoritative position. ok is false for an unknown or empty token
// (not joined). Used by POST /api/chat so /here can't be spoofed.
func (w *World) SenderFor(token string) (string, protocol.Hex, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.byToken[token]
	if !ok || token == "" {
		return "", protocol.Hex{}, false
	}

	return e.name, e.hex, true
}

// StreamOpened marks that a live event stream opened for the entity with this
// token (a new connection or an EventSource reconnect). A positive stream count
// keeps the entity out of the disconnect sweep. No-op for an unknown or empty
// token (a stream opened before/without a join).
func (w *World) StreamOpened(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok && token != "" {
		e.streams++
	}
}

// StreamClosed marks that an event stream for this token closed; when the last
// one closes it stamps disconnectedAt, starting the removal grace. No-op for an
// unknown/empty token or an entity with no open streams.
func (w *World) StreamClosed(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok && token != "" && e.streams > 0 {
		e.streams--
		if e.streams == 0 {
			e.disconnectedAt = w.now()
		}
	}
}

// sweepDisconnectedLocked removes every player whose event stream has been gone
// longer than the disconnect grace: a player entity (kind player AND a token)
// with streams == 0 and now-disconnectedAt > disconnectGrace. Monsters (no
// token) are never candidates. It collects candidate ids first (sorted) and
// deletes after, so the entity map is never mutated mid-range and removals are
// deterministic. Before deleting each entity it archives its character
// (identity, XP, gear — see characterRecord/World.archive) so a later Join
// with the same token restores it instead of minting a fresh one; party and
// personal-quest state are NOT archived — they dissolve/return to the board
// exactly as before (session-scoped social state, not progression). If it
// removed anyone it recomputes bubbles — a swept entity may have been
// mid-fight — and returns true, so the caller republishes and clients despawn
// the entity. Callers hold w.mu.
func (w *World) sweepDisconnectedLocked(now time.Time) bool {
	var gone []int64

	for id, e := range w.entities {
		if e.kind != protocol.EntityPlayer || e.token == "" {
			continue
		}

		if e.streams == 0 && now.Sub(e.disconnectedAt) > w.disconnectGrace {
			gone = append(gone, id)
		}
	}

	if len(gone) == 0 {
		return false
	}

	slices.Sort(gone)

	for _, id := range gone {
		e := w.entities[id]

		w.archive[e.token] = archiveLocked(e)

		w.logger.Info(identityLogMsg, "event", identityEventSweepArchive,
			"id", e.id, "name", e.name, "token_prefix", tokenPrefix(e.token))

		w.leavePartyLocked(e)
		w.abandonPersonalQuestLocked(e)
		delete(w.pendingInvites, id)

		for invitee, inviter := range w.pendingInvites {
			if inviter == id {
				delete(w.pendingInvites, invitee)
			}
		}

		delete(w.entities, id)
		delete(w.byToken, e.token)
	}

	w.recomputeBubblesLocked(now)

	return true
}

// SubmitIntent applies one player intent for the next turn. Kind is required:
// a "move" intent sets the entity's route to Target: any walkable, reachable
// hex — the server pathfinds from the entity's current position and the walk
// advances one hex per resolved turn. An "attack" intent queues a ranged
// attack at Target (bow single-target or mage AoE) and clears the route — you
// shoot, you don't move. An "equip" intent (ItemID) swaps an owned item into
// its slot: outside a combat bubble it is free and immediate; inside one it
// is the player's action for the turn (clearing any queued move/attack, see
// queueEquipLocked). Any other Kind (including empty) is rejected with
// ErrInvalidIntentKind. For an entity inside a combat bubble the submission
// also counts as a lock-in for the bubble's action-gated turn, and once every
// player member has locked in the bubble resolves immediately. The latest
// submission in an input window replaces the entity's queued action.
func (w *World) SubmitIntent(req protocol.IntentRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entities[req.EntityID]
	if !ok || e.token != req.Token {
		return ErrUnauthorized
	}

	if err := w.dispatchIntentLocked(e, req); err != nil {
		return err
	}

	// Lock-in: inside a combat bubble, submitting an intent commits this player
	// for the bubble's action-gated turn. Once every player member has locked
	// in, the bubble resolves immediately (rather than waiting for the poll or
	// the patience timeout) and the tick hub is notified — UNLESS the turn
	// floor (bubbleFloorElapsedLocked, playtest item 5) has not yet elapsed
	// since the bubble's previous resolution: a solo player spamming intents
	// then stays ready-but-unresolved, and the poll loop's own
	// bubbleReadyOrExpiredLocked check (same floor) picks the turn up the
	// moment the floor allows it, rather than resolving faster than the
	// world's own turn cadence.
	if e.bubbleID != 0 {
		if b, ok := w.bubbles[e.bubbleID]; ok {
			b.ready[e.id] = struct{}{}

			now := w.now()

			if w.allPlayersReadyLocked(b) && w.bubbleFloorElapsedLocked(b, now) {
				w.resolveBubbleTurnLocked(b, w.bubbleMembersLocked(b), now)
				w.recomputeBubblesLocked(now)
				w.ticks.Publish()
			}
		}
	}

	return nil
}

// dispatchIntentLocked routes one validated intent to its queue function by
// kind (split out of SubmitIntent to keep its cognitive complexity in
// check). Callers hold w.mu.
func (w *World) dispatchIntentLocked(e *entity, req protocol.IntentRequest) error {
	switch req.Kind {
	case protocol.IntentMove:
		return w.queueMoveLocked(e, req.Target)
	case protocol.IntentAttack:
		return w.queueAttackLocked(e, req.Target, req.TargetEntityID)
	case protocol.IntentEquip:
		return w.queueEquipLocked(e, req.ItemID)
	case protocol.IntentUnequip:
		return w.queueUnequipLocked(e, req.ItemID)
	case protocol.IntentDrop:
		return w.queueDropLocked(e, req.ItemID)
	case protocol.IntentPickup:
		return w.queuePickupLocked(e, req.GroundItemID)
	case protocol.IntentDrink:
		return w.queueDrinkLocked(e, req.ItemID)
	default:
		return ErrInvalidIntentKind
	}
}

// queueMoveLocked validates a move intent and sets the entity's route to a
// walkable, reachable target, clearing any pending ranged attack or queued
// equip (the latest intent in the window wins — the swap is a whole turn's
// action, so a move submitted after an equip must displace it exactly as an
// equip displaces a queued move). Callers hold w.mu.
func (w *World) queueMoveLocked(e *entity, target protocol.Hex) error {
	if !w.walkableLocked(target) {
		return ErrNotWalkable
	}

	path := Pathfind(e.hex, target, w.walkableLocked)
	if path == nil {
		return ErrNoPath
	}

	e.path = path
	e.attackTarget = nil
	e.attackTargetEntity = 0
	e.pending = pendingItemAction{}

	return nil
}

// queueAttackLocked validates a ranged attack intent and queues it: the
// entity must have a ranged weapon equipped (else ErrNoRangedWeapon). A
// single-target weapon (aoeRadius 0, a bow) with a non-zero targetEntityID
// (item 7, playtest batch 2) is ENTITY-targeted: the named entity must exist
// and be alive (else ErrAttackTargetNotFound), be a hostile — opposing faction
// (else ErrAttackTargetNotHostile), and be within the weapon's reach from e's
// current hex at submit time (else ErrOutOfRange); resolution re-aims at
// the victim's post-move hex (resolveEntityTargetedLocked) rather than
// trusting this submit-time position, so a sidestep or retreat is tracked.
// Anything else (an AoE cast, or targetEntityID 0 — e.g. a defensive/legacy
// hex-only bow shot) is GROUND-targeted at target, checked the same way
// against e's current hex. On success it records the target and clears the
// route and any queued equip — a ranged attack replaces the move AND the
// swap for this turn (the latest intent in the window wins).
//
// INVARIANT: max over every registered def's rangeHex+aoeRadius must stay <=
// CombatRadius (validateMaxReach, run at content load by mustValidateContent),
// so any entity a ranged attack can reach is always already in the shooter's
// combat bubble. If that invariant were ever violated, a monster could be
// ranged-killed in the WORLD domain (where resolveWorldTurnLocked awards no
// kill-XP) — add an in-bubble/target-in-member-set guard here then. Callers
// hold w.mu.
func (w *World) queueAttackLocked(e *entity, target protocol.Hex, targetEntityID int64) error {
	def := rangedDefFor(e)
	if def == nil {
		return ErrNoRangedWeapon
	}

	if targetEntityID != 0 && def.aoeRadius == 0 {
		victim, ok := w.entities[targetEntityID]
		if !ok || victim.hp <= 0 {
			return ErrAttackTargetNotFound
		}

		if !opposing(e, victim) {
			return ErrAttackTargetNotHostile
		}

		if HexDistance(e.hex, victim.hex) > def.rangeHex {
			return ErrOutOfRange
		}

		e.attackTargetEntity = targetEntityID
		e.attackTarget = nil
		e.path = nil
		e.pending = pendingItemAction{}

		return nil
	}

	if HexDistance(e.hex, target) > def.rangeHex {
		return ErrOutOfRange
	}

	t := target
	e.attackTargetEntity = 0
	e.attackTarget = &t
	e.path = nil
	e.pending = pendingItemAction{}

	return nil
}

// queueEquipLocked validates and applies/queues an equip OR unequip toggle.
// An equip intent naming an item instance ALREADY in its slot toggles it OFF
// (back into a free backpack entry — playtest batch 2's toggle, now subject
// to the backpack having room); any other owned, wearable item toggles ON,
// swapping into its type-derived slot through the backpack
// (items.go's toggleEquip). Follows the shared free-outside/turn-inside rule
// (commitItemActionLocked). Callers hold w.mu.
func (w *World) queueEquipLocked(e *entity, itemID int64) error {
	// Validate at queue time so the intent's HTTP response reports a bad
	// equip immediately; the apply (immediate or at resolution) re-validates.
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	if !canEquip(e.class, itemDefByID[inst.defID]) {
		return ErrWrongClass
	}

	return w.commitItemActionLocked(e, protocol.IntentEquip, itemID, func() error {
		return w.equipItemLocked(e, itemID)
	})
}

// queueUnequipLocked validates and applies/queues an unequip: the named item
// must be owned, currently equipped, and the backpack must have a free
// entry. Callers hold w.mu.
func (w *World) queueUnequipLocked(e *entity, itemID int64) error {
	inst, def, err := w.ownedDefLocked(e, itemID)
	if err != nil {
		return err
	}

	if cur, ok := e.equipped[slotForType(def.itemType)]; !ok || cur.id != inst.id {
		return ErrItemNotEquipped
	}

	if e.freeBackpackIndex() < 0 {
		return ErrBackpackFull
	}

	return w.commitItemActionLocked(e, protocol.IntentUnequip, itemID, func() error {
		return w.unequipItemLocked(e, itemID)
	})
}

// queueDropLocked validates and applies/queues a drop of an owned item (or
// whole consumable stack) onto the player's own hex. Callers hold w.mu.
func (w *World) queueDropLocked(e *entity, itemID int64) error {
	if _, ok := e.itemByID(itemID); !ok {
		return ErrItemNotOwned
	}

	return w.commitItemActionLocked(e, protocol.IntentDrop, itemID, func() error {
		return w.dropItemLocked(e, itemID)
	})
}

// queuePickupLocked validates and applies/queues a pickup of one ground item
// from the player's own hex: the item must lie there, and a home must exist
// in the spec's priority order (mergeable stack > free entry >
// ErrBackpackFull — validated here too, so a doomed pickup 422s immediately
// instead of silently fizzling a whole bubble turn). Callers hold w.mu.
func (w *World) queuePickupLocked(e *entity, groundItemID int64) error {
	var found *itemInstance

	for _, it := range w.groundItems[e.hex] {
		if it.id == groundItemID {
			found = &it

			break
		}
	}

	if found == nil {
		return ErrNoSuchGroundItem
	}

	if e.stackIndexFor(found.defID) < 0 && e.freeBackpackIndex() < 0 {
		return ErrBackpackFull
	}

	return w.commitItemActionLocked(e, protocol.IntentPickup, groundItemID, func() error {
		return w.pickupGroundLocked(e, groundItemID)
	})
}

// queueDrinkLocked validates and applies/queues drinking one unit of an
// owned consumable stack. Callers hold w.mu.
func (w *World) queueDrinkLocked(e *entity, itemID int64) error {
	_, def, err := w.ownedDefLocked(e, itemID)
	if err != nil {
		return err
	}

	if def.itemType != protocol.ItemTypeConsumable {
		return ErrNotDrinkable
	}

	return w.commitItemActionLocked(e, protocol.IntentDrink, itemID, func() error {
		return w.drinkItemLocked(e, itemID)
	})
}

// entityNameLocked is the wire Name for e: a player's chosen display name,
// or a monster's kind's display name ("Wolf", "Dragon", ...) — monsters'
// Name was always empty until 6c (no field collision: a player's name and
// a monster's kind name occupy the same wire field, but nothing produces
// both for the same entity). kindOf(e) nil (a malformed monster fixture)
// falls back to empty, matching the pre-6c wire shape rather than panicking
// on a Snapshot call. Callers hold w.mu.
func entityNameLocked(e *entity) string {
	if e.kind != protocol.EntityMonster {
		return e.name
	}

	if k := kindOf(e); k != nil {
		return k.name
	}

	return ""
}

// Snapshot is the current turn bundle: turn number plus every entity,
// sorted by ID for a deterministic wire shape.
func (w *World) Snapshot() protocol.TurnEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	entities := make([]protocol.Entity, 0, len(w.entities))
	for _, e := range w.entities {
		entities = append(entities, protocol.Entity{
			ID: e.id, Hex: e.hex, Kind: e.kind, Name: entityNameLocked(e), Class: e.class, Species: e.species,
			HP: e.hp, MaxHP: e.maxHP, InCombat: e.bubbleID != 0, XP: e.xp, Level: levelFor(e.xp), PartyID: e.partyID,
			Items: itemViewsLocked(e), MonsterKind: e.monsterKind,
		})
	}

	slices.SortFunc(entities, func(a, b protocol.Entity) int { return int(a.ID - b.ID) })

	now := w.now()

	bubbles := make([]protocol.BubbleView, 0, len(w.bubbles))
	for _, b := range w.bubbles {
		bubbles = append(bubbles, w.bubbleViewLocked(b, now))
	}

	slices.SortFunc(bubbles, func(a, b protocol.BubbleView) int { return int(a.ID - b.ID) })

	questViews := make([]protocol.QuestView, 0, len(w.quests))
	for _, q := range w.quests {
		questViews = append(questViews, protocol.QuestView{
			ID: q.id, Name: q.name, Kind: q.kind, TargetN: q.targetN,
			GoalHex: q.goalHex, Progress: q.progress, RewardXP: q.rewardXP,
			State: q.state, HolderEntityID: q.holderEntity, HolderPartyID: q.holderParty,
		})
	}

	groundItems := make([]protocol.GroundItemView, 0, len(w.groundItems))

	for hex, items := range w.groundItems {
		for _, it := range items {
			def := itemDefByID[it.defID]
			groundItems = append(groundItems, protocol.GroundItemView{
				ID: it.id, Hex: hex, DefID: it.defID, Name: def.name, Type: def.itemType,
			})
		}
	}

	slices.SortFunc(groundItems, func(a, b protocol.GroundItemView) int { return int(a.ID - b.ID) })

	return protocol.TurnEvent{
		Turn: w.turn, IntervalMs: w.interval.Milliseconds(), Entities: entities, Bubbles: bubbles,
		Quests: questViews, GroundItems: groundItems, WorldID: w.worldID,
	}
}

// itemViewsLocked builds the wire item list for one entity: an ItemView per
// owned item instance — equipped gear first, in canonicalSlotOrder (so the
// list order is deterministic despite e.equipped being a map), then backpack
// entries in index order. Slot carries the def's itemType (the taxonomy
// string; see protocol.ItemView.Slot's doc comment). Always a non-nil
// slice — empty (not null) for a monster (which owns nothing) or a player
// who owns nothing, so the wire shape matches the generated TS type's
// non-optional ItemView[]. Callers hold w.mu.
func itemViewsLocked(e *entity) []protocol.ItemView {
	views := make([]protocol.ItemView, 0, len(e.equipped)+len(e.backpack))

	for _, slot := range canonicalSlotOrder {
		inst, ok := e.equipped[slot]
		if !ok || inst.id == 0 {
			continue
		}

		views = append(views, itemViewOf(inst, true, 1))
	}

	for _, be := range e.backpack {
		if be.empty() {
			continue
		}

		views = append(views, itemViewOf(be.inst, false, be.count))
	}

	return views
}

// itemViewOf renders one owned item instance for the wire. count is the
// stack size (1 for gear and equipped items).
func itemViewOf(inst itemInstance, equipped bool, count int) protocol.ItemView {
	def := itemDefByID[inst.defID]

	return protocol.ItemView{
		ID: inst.id, DefID: inst.defID, Name: def.name, Type: def.itemType,
		Damage: def.damage, RangeHex: def.rangeHex, AoERadius: def.aoeRadius, Desc: def.desc,
		Equipped: equipped, Count: count,
	}
}

// opposing reports whether a and b are of different factions (player vs
// monster). Same-faction entities stack; opposing ones can't share a hex.
func opposing(a, b *entity) bool { return a.kind != b.kind }

func hasOpposing(occs []*entity, m *entity) bool {
	for _, o := range occs {
		if opposing(o, m) {
			return true
		}
	}

	return false
}

func opposingOccupants(occs []*entity, m *entity) []*entity {
	var out []*entity

	for _, o := range occs {
		if opposing(o, m) {
			out = append(out, o)
		}
	}

	return out
}

// removeEntity drops m from an occupant slice (by identity).
func removeEntity(occs []*entity, m *entity) []*entity {
	for i, o := range occs {
		if o == m {
			return append(occs[:i], occs[i+1:]...)
		}
	}

	return occs
}

// pendingBump is a move onto an opposing-held hex, re-checked post-move.
type pendingBump struct {
	m      *entity
	target protocol.Hex
}

// pendingAttack is a bump that is still opposing-held after the move phase
// completes, and therefore lands as an attack.
type pendingAttack struct {
	attacker *entity
	target   protocol.Hex
}

// resolveWorldTurnLocked advances the world domain one turn: the phased combat
// pipeline over the given world-domain member set, then a turn bump. It does
// NOT recompute bubbles — the caller recomputes once, after every resolution of
// the pass, so an entity that changes domain mid-pass (walks into a bubble, or
// flees one) still acts exactly once, in the phase it belonged to when the pass
// captured its members. Callers hold w.mu.
//
// The slain-kinds list resolveCombatLocked returns is deliberately dropped:
// kill XP is scoped to a real fight (a combat bubble), so a monster that dies in
// the world domain — only possible via an anomalous faction-blind spawn/join
// landing a player next to an un-bubbled monster — credits no XP to anyone.
func (w *World) resolveWorldTurnLocked(members []*entity) {
	w.regenPlayersLocked(members)
	w.resolveCombatLocked(members, w.allPlayersLocked(), true)
	w.checkReachQuestsLocked()
	w.turn++
}

// regenPlayersLocked heals every out-of-combat player protocol.RegenPerTurn HP
// on a WORLD-domain turn resolution — the passive recovery layer (plan §9):
// staying alive and out of a fight is now itself a way to top up HP, instead
// of death (a full-HP respawn) being the only heal. It never fires for a
// bubbled player (mid-fight means no regen), a monster (they don't regen at
// all), a dead entity (hp <= 0), or one already at max HP, and it never pushes
// hp past maxHP. members is always the world-domain set here
// (resolveWorldTurnLocked's only caller passes domainMembersLocked, already
// filtered to bubbleID == 0), but the check below stays explicit rather than
// relying on that — cheap, and it fails safe if a future caller ever passes a
// mixed set. Callers hold w.mu.
func (w *World) regenPlayersLocked(members []*entity) {
	for _, e := range members {
		if e.kind != protocol.EntityPlayer || e.bubbleID != 0 {
			continue
		}

		if e.hp <= 0 || e.hp >= e.maxHP {
			continue
		}

		e.hp = min(e.hp+protocol.RegenPerTurn, e.maxHP)
	}
}

// resolveBubbleTurnLocked advances one combat bubble a single action-gated turn:
// the phased combat pipeline over the given member set, then the shared kill-XP
// award, then it clears the bubble's lock-ins and restarts its patience deadline
// for the next turn. Like resolveWorldTurnLocked it does NOT recompute — see that
// method. Callers hold w.mu.
func (w *World) resolveBubbleTurnLocked(b *bubble, members []*entity, now time.Time) {
	slain := w.resolveCombatLocked(members, playersOf(members), false)

	// Kill XP belongs to the fight: every player who survived this bubble-turn
	// earns the FULL sum of the slain kinds' xp — no last-hit competition,
	// helping always pays, and the award is not divided. A player who died
	// this same turn is not surviving (hp<=0), so earns nothing.
	if len(slain) > 0 {
		totalXP := 0
		for _, k := range slain {
			totalXP += k.xp
		}

		for _, e := range members {
			if e.kind == protocol.EntityPlayer && e.hp > 0 {
				award := applyRules(evEarnXP, totalXP, earnXPCards(e), ruleCtx{})
				e.xp += award
				syncMaxHPLocked(e)

				w.logger.Info(combatLogMsg, "event", combatEventXP, "id", e.id, "base", totalXP, "awarded", award)
			}
		}

		// The chat stream doubles as the combat log: one kill summary per
		// bubble turn (not per monster — a mage's AoE turn stays one line),
		// naming the slain kinds. The summed base XP is quoted because it's
		// the only shared number (species bonuses are per-player). Kill
		// credit deliberately does not exist for a MULTI-player bubble — the
		// nameless wording stays. But when exactly one player is in the
		// bubble at award time (playtest item 3), there is no competing
		// credit to avoid: name them ("NAME slew a wolf (+20 XP)") — a solo
		// hunt reads better attributed.
		if players := playersOf(members); len(players) == 1 {
			w.announce("system", killSoloSummary(players[0].name, slain))
		} else {
			w.announce("system", killSummary(slain))
		}

		w.tickKillQuestsLocked(members, len(slain))
	}

	w.checkReachQuestsLocked()

	clear(b.ready)
	b.deadline = now.Add(w.combatPatience)
	b.lastResolvedAt = now

	w.turn++
}

// resolveCombatLocked runs the decided phased resolution over a given entity
// set: think → move (faction-aware, with bump deferral) → attack (simultaneous,
// post-move positions) → apply damage & deaths. The set is a whole
// CombatRadius-connected domain (the world domain or one bubble), so no move,
// bump, stack, or attack can reach an entity outside it. worldDomain selects
// thinkMonstersLocked's aggro gating (true for the world domain, false inside
// a bubble — see that function's doc comment). It does not recompute bubbles
// or advance the turn — the two resolve callers own that. It returns the
// kinds of every monster that died this resolution (one entry per dead
// monster, not deduplicated), which the bubble path turns into the shared
// kill-XP award and the kill-summary announce. Callers hold w.mu.
func (w *World) resolveCombatLocked(members, monsterTargets []*entity, worldDomain bool) []*monsterDef {
	// Built before thinkMonstersLocked (unlike pre-6.4, when the rng was built
	// after) so a future aggro-range rule card using a condChance condition has
	// a real, turn-seeded rng to consume instead of a nil one — see
	// aggroRadiusForLocked.
	//nolint:gosec // deterministic per-turn combat RNG, not security-sensitive; reproducibility is required.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), uint64(w.turn)))

	w.thinkMonstersLocked(rng, members, monsterTargets, worldDomain)

	// Pending inventory actions are this turn's action for any member that
	// queued one (commitItemActionLocked, inside a bubble): apply them before
	// movement and damage resolve — members arrive id-sorted, so this is
	// deterministic like the rest of the phased resolution (two players'
	// pending pickups of the same ground item resolve lowest-id-first; the
	// loser fizzles).
	for _, e := range members {
		if e.hp > 0 {
			w.applyPendingItemLocked(e)
		}
	}

	// Evolving board: who is on each hex as moves resolve.
	byHex := make(map[protocol.Hex][]*entity, len(members))
	for _, e := range members {
		byHex[e.hex] = append(byHex[e.hex], e)
	}

	attacks := w.moveAndBumpLocked(rng, byHex, members)

	// NOTE: walk-over auto-pickup used to run here (between movement and the
	// attack phase). The inventory-slots milestone removed it — picking up is
	// now an explicit pickup INTENT (inventory.go), free outside a bubble and
	// a whole turn inside one, applied by the pending pass above.

	w.attackLocked(rng, byHex, attacks)

	return w.resolveDeathsLocked(rng, members)
}

// allyInBubbleLocked reports whether another living same-faction entity shares
// e's bubble — the pack-bow style condition. Callers hold w.mu.
func (w *World) allyInBubbleLocked(e *entity) bool {
	if e.bubbleID == 0 {
		return false
	}

	b, ok := w.bubbles[e.bubbleID]
	if !ok {
		return false
	}

	for id := range b.members {
		if o, ok := w.entities[id]; ok && o != e && o.kind == e.kind && o.hp > 0 {
			return true
		}
	}

	return false
}

// rollDamageLocked runs one hit through the pipeline: the attacker's
// deal-damage cards (species + the acting weapon's rules) then the victim's
// take-damage cards (species + the rules of EVERYTHING the victim has
// equipped, folded in canonicalSlotOrder — a hit lands on the whole entity,
// not just the slot that happens to be attacking; this is how armor's
// take-damage cards apply). A monster victim folds its kind's claws rules
// instead (it never equips). weapon is the attacker's acting weapon def
// (closeDefFor for a bump, rangedDefFor for a shot); it is never nil — every
// combat site resolves a def (fists/claws fallback for close, a real
// equipped item for ranged, since a nil ranged def never reaches here — see
// queueAttackLocked/resolveRangedLocked). Every damage number in the game
// flows through here. Callers hold w.mu.
func (w *World) rollDamageLocked(rng *mrand.Rand, attacker, victim *entity, weapon *itemDef, base int) int {
	ctx := ruleCtx{attacker: attacker, victim: victim, allyInBubble: w.allyInBubbleLocked(attacker), rng: rng}

	attackerCards := slices.Concat(speciesCards(attacker.species), weapon.rules)
	dealt := applyRules(evDealDamage, base, attackerCards, ctx)

	victimCards := slices.Concat(speciesCards(victim.species), victimGearCards(victim))

	return applyRules(evTakeDamage, dealt, victimCards, ctx)
}

// victimGearCards returns the rule cards an entity contributes to
// pipeline folds over its whole person (take-damage, aggro-range): a
// monster's kind claws rules (monsterDef's rules seam — a monster never
// equips), or every equipped item's rules for a player (equippedRuleCards,
// canonicalSlotOrder — deterministic).
func victimGearCards(e *entity) []ruleCard {
	if e.kind == protocol.EntityMonster {
		return closeDefFor(e).rules
	}

	return equippedRuleCards(e)
}

// earnXPCards returns the cards folded over an XP award for player e:
// species passives plus every equipped item's rules (canonicalSlotOrder —
// deterministic), so gear like the Headband of Learning modifies XP the same
// way species passives do. Shared by the kill award
// (resolveBubbleTurnLocked) and quest completion payouts (quest.go).
func earnXPCards(e *entity) []ruleCard {
	return slices.Concat(speciesCards(e.species), equippedRuleCards(e))
}

// domainMembersLocked returns every world-domain entity (bubbleID == 0), sorted
// by id for deterministic resolution. Callers hold w.mu.
func (w *World) domainMembersLocked() []*entity {
	out := make([]*entity, 0, len(w.entities))

	for _, e := range w.entities {
		if e.bubbleID == 0 {
			out = append(out, e)
		}
	}

	slices.SortFunc(out, func(a, b *entity) int { return int(a.id - b.id) })

	return out
}

// bubbleMembersLocked returns bubble b's live members, sorted by id for
// deterministic resolution. A member id with no live entity (removed since the
// last recompute) is skipped. Callers hold w.mu.
func (w *World) bubbleMembersLocked(b *bubble) []*entity {
	out := make([]*entity, 0, len(b.members))

	for id := range b.members {
		if e, ok := w.entities[id]; ok {
			out = append(out, e)
		}
	}

	slices.SortFunc(out, func(a, b *entity) int { return int(a.id - b.id) })

	return out
}

// allPlayersReadyLocked reports whether every player member of b has locked in
// this bubble-turn. False for a bubble with no live player member (it can only
// advance by patience timeout). Callers hold w.mu.
func (w *World) allPlayersReadyLocked(b *bubble) bool {
	hasPlayer := false

	for id := range b.members {
		e, ok := w.entities[id]
		if !ok || e.kind != protocol.EntityPlayer {
			continue
		}

		hasPlayer = true

		if _, ready := b.ready[id]; !ready {
			return false
		}
	}

	return hasPlayer
}

// bubbleReadyOrExpiredLocked reports whether b should resolve now: every player
// has locked in, or its patience deadline has passed — AND, either way, the
// turn floor (bubbleFloorElapsedLocked, playtest item 5) has elapsed since
// its previous resolution. Callers hold w.mu.
func (w *World) bubbleReadyOrExpiredLocked(b *bubble, now time.Time) bool {
	if !w.bubbleFloorElapsedLocked(b, now) {
		return false
	}

	if !b.deadline.IsZero() && now.After(b.deadline) {
		return true
	}

	return w.allPlayersReadyLocked(b)
}

// bubbleFloorElapsedLocked reports whether at least one world turn interval
// has passed since b's previous resolution — the turn floor a bubble-turn
// may never resolve faster than (playtest item 5: no solo action-spam), even
// when every player is locked in. True for a bubble that has never resolved
// (lastResolvedAt zero — its first turn is ungated by this floor). The floor
// scales with w.interval, the world's configured turn interval (equal to
// protocol.TurnSeconds in production, shrunk by tests/e2e the same way
// TURN_INTERVAL is), never a hardcoded constant — see docs on
// combatPatience for why the interval is already threaded through
// NewWorld/config the same way. Callers hold w.mu.
func (w *World) bubbleFloorElapsedLocked(b *bubble, now time.Time) bool {
	return b.lastResolvedAt.IsZero() || now.Sub(b.lastResolvedAt) >= w.interval
}

// moveAndBumpLocked resolves the move phase: movers advance one hex from
// their path in seeded-shuffled order, unless the destination is
// opposing-held (deferred as a bump) or the destination hex is at StackCap
// for a same-faction move (waits, path retained). Deferred bumps are
// re-checked once every other move has landed — a bump target that emptied
// out this turn (the defender retreated) completes as a normal move instead
// of an attack. Returns the bumps that are still opposing-held after that
// re-check, i.e. the attacks to resolve this turn. Callers hold w.mu.
func (w *World) moveAndBumpLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity, members []*entity,
) []pendingAttack {
	movers := make([]*entity, 0, len(members))
	for _, e := range members {
		if len(e.path) > 0 {
			movers = append(movers, e)
		}
	}

	slices.SortFunc(movers, func(a, b *entity) int { return int(a.id - b.id) })
	rng.Shuffle(len(movers), func(i, j int) { movers[i], movers[j] = movers[j], movers[i] })

	var bumps []pendingBump

	for _, m := range movers {
		next := m.path[0]
		occs := byHex[next]

		switch {
		case hasOpposing(occs, m):
			bumps = append(bumps, pendingBump{m, next}) // stay; resolve after move phase
		case len(occs) < protocol.StackCap:
			from := m.hex
			byHex[m.hex] = removeEntity(byHex[m.hex], m)
			byHex[next] = append(byHex[next], m)
			m.hex = next
			m.path = m.path[1:]
			w.logger.Info(combatLogMsg, "event", combatEventMove, "id", m.id, "kind", m.kind, "from", from, "to", next)
		}
		// else: same-faction hex full → wait (path retained).
	}

	// Post-move bump re-check: still opposing-held → attack; vacated → complete
	// the move (retreat dodge / follow into the empty hex).
	var attacks []pendingAttack

	for _, b := range bumps {
		occs := byHex[b.target]

		switch {
		case hasOpposing(occs, b.m):
			attacks = append(attacks, pendingAttack{b.m, b.target})
		case len(occs) < protocol.StackCap:
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "bump_target_vacated",
				"attacker", b.m.id, "target_hex", b.target)

			from := b.m.hex
			byHex[b.m.hex] = removeEntity(byHex[b.m.hex], b.m)
			byHex[b.target] = append(byHex[b.target], b.m)
			b.m.hex = b.target
			b.m.path = b.m.path[1:]
			w.logger.Info(combatLogMsg, "event", combatEventMove, "id", b.m.id, "kind", b.m.kind, "from", from, "to", b.target)
		}
	}

	return attacks
}

// attackLocked resolves the attack phase: each bump attack and each pending
// ranged attack accumulates damage against pre-attack HP (nothing applied yet)
// into one shared map, so order is irrelevant and mutual kills work, then
// applies it all at once. A stacked defending hex picks its victim with rng, so
// a bump against a stack damages exactly one occupant. Ranged attacks resolve in
// the same map (resolveRangedLocked) so a bow shot and a bump land
// simultaneously. Callers hold w.mu.
func (w *World) attackLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity, attacks []pendingAttack) {
	damage := make(map[int64]int)

	for _, a := range attacks {
		victims := opposingOccupants(byHex[a.target], a.attacker)
		if len(victims) == 0 {
			continue // guard; the re-check ensured at least one
		}

		// Canonical order first, like the movers shuffle above: byHex was
		// populated by ranging w.entities (a map), whose iteration order is
		// unspecified and varies per range — without this sort, victim choice
		// would depend on that incidental order instead of the seed alone.
		slices.SortFunc(victims, func(a, b *entity) int { return int(a.id - b.id) })

		victim := victims[rng.IntN(len(victims))]

		// Melee/bump damage: the attacker's equipped close-slot item (or the
		// fists/claws fallback — closeDefFor), level-scaled via itemDamage. A
		// monster's closeDefFor is its KIND's own claws profile (6c —
		// monsterDef.claws, e.g. a rat's 1 vs a dragon's 9); levelFor(0) == 1
		// keeps the level-scaling term 0 for monsters. Resolved once here
		// (mirroring resolveRangedLocked's def := rangedDefFor(e) below) so
		// the def is looked up exactly once per hit.
		weapon := closeDefFor(a.attacker)
		base := itemDamage(weapon, levelFor(a.attacker.xp))
		dealt := w.rollDamageLocked(rng, a.attacker, victim, weapon, base)
		damage[victim.id] += dealt

		w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", a.attacker.id, "victim", victim.id,
			"weapon", weapon.id, "base", base, "dealt", dealt)
	}

	w.resolveRangedLocked(rng, byHex, damage)

	for id, dmg := range damage {
		w.entities[id].hp -= dmg
	}
}

// resolveRangedLocked folds every pending ranged attack into the shared damage
// map (against pre-attack HP, so a bow shot lands simultaneously with bumps).
// Shooters are processed in id order so the seeded single-target victim pick is
// reproducible regardless of map iteration order. An ENTITY-targeted attack
// (item 7, playtest batch 2 — attackTargetEntity != 0, a single-target bow
// shot aimed at a specific victim rather than a hex) delegates to
// resolveEntityTargetedLocked, which re-aims at the victim's post-move hex.
// Everything else is GROUND-targeted at attackTarget's hex, re-checked
// against post-move positions in byHex: a shot that is now out of range
// fizzles. A bow (aoeRadius 0) shot this way damages one opposing occupant at
// the target hex — a stack picks one hostile with rng, mirroring the bump
// victim pick (this is the legacy/defensive hex-only bow path — kept for the
// SetAttackTargetForTest bridge and any future hex-only ranged use; a real
// client always sends an entity id for a single-target weapon). Magic
// (aoeRadius > 0) damages every opposing-faction entity within aoeRadius of
// the target hex — no friendly fire, ground-targeted by nature. Every
// shooter's pending target is cleared, hit or fizzle. byHex holds exactly the
// resolving member set, so targets outside the domain are naturally
// unreachable. Callers hold w.mu.
func (w *World) resolveRangedLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity, damage map[int64]int) {
	var shooters []*entity

	for _, occs := range byHex {
		for _, e := range occs {
			if e.attackTarget != nil || e.attackTargetEntity != 0 {
				shooters = append(shooters, e)
			}
		}
	}

	slices.SortFunc(shooters, func(a, b *entity) int { return int(a.id - b.id) })

	for _, e := range shooters {
		targetEntityID := e.attackTargetEntity
		hexTarget := e.attackTarget
		e.attackTarget = nil // resolved, whether it hits or fizzles
		e.attackTargetEntity = 0

		def := rangedDefFor(e)
		if def == nil {
			// unequipped mid-turn (equip intent, Task 4) → fizzle
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "unequipped", "attacker", e.id)

			continue
		}

		if targetEntityID != 0 {
			w.resolveEntityTargetedLocked(rng, e, def, targetEntityID, damage)

			continue
		}

		target := *hexTarget

		if HexDistance(e.hex, target) > def.rangeHex {
			// moved out of range this turn → fizzle
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "out_of_range", "attacker", e.id,
				"weapon", def.id, "target_hex", target)

			continue
		}

		dmg := itemDamage(def, levelFor(e.xp))

		if def.aoeRadius == 0 {
			w.resolveBowLocked(rng, byHex, e, def, target, dmg, damage)

			continue
		}

		w.resolveAoELocked(rng, byHex, e, def, target, def.aoeRadius, dmg, damage)
	}
}

// resolveEntityTargetedLocked resolves one entity-targeted single-target
// ranged attack (item 7, playtest batch 2): re-aims at the victim's CURRENT
// (post-move) hex rather than trusting the hex it happened to occupy at
// submit time, so a sidestepping or retreating target is tracked the way a
// hex-pinned shot never could — hits if the victim is still within the
// weapon's range from the shooter's own post-move hex, else fizzles
// (reason out_of_range, same as the ground-targeted path). A victim that
// died or vanished this same turn — a simultaneous kill by another attacker,
// resolved earlier in this same damage-accumulation pass — also fizzles
// (reason target_gone) rather than panicking on a missing entity; damage
// application happens all-at-once after every attack accumulates
// (attackLocked), so "vanished" here really means removed by a PRIOR
// resolution this turn (deaths, not this pass — resolveDeathsLocked runs
// after this), i.e. any entity that already left w.entities entirely, which
// resolveCombatLocked never does mid-attack-phase; this guard is therefore
// mostly defensive, matching resolveBowLocked's own empty-hex no-op. Callers
// hold w.mu.
func (w *World) resolveEntityTargetedLocked(
	rng *mrand.Rand, attacker *entity, weapon *itemDef, targetEntityID int64, damage map[int64]int,
) {
	victim, ok := w.entities[targetEntityID]
	if !ok || victim.hp <= 0 {
		w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "target_gone", "attacker", attacker.id)

		return
	}

	if HexDistance(attacker.hex, victim.hex) > weapon.rangeHex {
		w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "out_of_range",
			"attacker", attacker.id, "weapon", weapon.id, "victim", victim.id)

		return
	}

	dmg := itemDamage(weapon, levelFor(attacker.xp))
	dealt := w.rollDamageLocked(rng, attacker, victim, weapon, dmg)
	damage[victim.id] += dealt

	w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", attacker.id, "victim", victim.id,
		"weapon", weapon.id, "base", dmg, "dealt", dealt)
}

// resolveBowLocked accumulates single-target ranged damage: the opposing-faction
// occupant at the target hex, or one seeded-random hostile if the hex holds a
// stack. An empty or friendly-only target hex deals nothing. Callers hold w.mu.
func (w *World) resolveBowLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity,
	attacker *entity, weapon *itemDef, target protocol.Hex, dmg int, damage map[int64]int,
) {
	victims := opposingOccupants(byHex[target], attacker)
	if len(victims) == 0 {
		return
	}

	slices.SortFunc(victims, func(a, b *entity) int { return int(a.id - b.id) })

	victim := victims[rng.IntN(len(victims))]
	dealt := w.rollDamageLocked(rng, attacker, victim, weapon, dmg)
	damage[victim.id] += dealt

	w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", attacker.id, "victim", victim.id,
		"weapon", weapon.id, "base", dmg, "dealt", dealt)
}

// resolveAoELocked accumulates AoE ranged damage: dmg to every opposing-faction
// entity within aoeRadius of the target hex. Same-faction entities (the caster
// and friendly players) are skipped — no friendly fire. Each victim rolls the
// pipeline independently (an elf caster can crit some splash victims and not
// others — per-victim rolls, not one shared roll for the whole cast), in id
// order: byHex is populated by ranging w.entities (a map), so without a sort
// the per-victim roll order — and thus the rng stream each victim consumes —
// would depend on incidental map iteration order instead of the seed alone.
// Callers hold w.mu.
func (w *World) resolveAoELocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity,
	attacker *entity, weapon *itemDef, target protocol.Hex, aoeRadius, dmg int, damage map[int64]int,
) {
	var victims []*entity

	for _, occs := range byHex {
		for _, o := range occs {
			if opposing(attacker, o) && HexDistance(target, o.hex) <= aoeRadius {
				victims = append(victims, o)
			}
		}
	}

	slices.SortFunc(victims, func(a, b *entity) int { return int(a.id - b.id) })

	for _, o := range victims {
		dealt := w.rollDamageLocked(rng, attacker, o, weapon, dmg)
		damage[o.id] += dealt

		w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", attacker.id, "victim", o.id,
			"weapon", weapon.id, "base", dmg, "dealt", dealt)
	}
}

// killSummary renders one bubble turn's monster deaths for the chat/combat
// log, naming the slain kinds and quoting their summed base XP (see the call
// site's comment on why base and why nameless). slain arrives in the order
// resolveDeathsLocked collected it (members' id-sorted iteration order) —
// killPhrase groups same-kind entries wherever they fall in that order, so
// two wolves dying non-consecutively (a wolf, then a troll, then a second
// wolf) still read as "2 wolves and a troll", not three separate clauses.
func killSummary(slain []*monsterDef) string {
	// Unreachable from resolveBubbleTurnLocked (its len(slain) > 0 gate),
	// but the exported-for-test wrapper (KillSummaryForTest) can reach it —
	// return an empty line instead of letting killPhrase's join panic.
	if len(slain) == 0 {
		return ""
	}

	total := 0
	for _, k := range slain {
		total += k.xp
	}

	was := "were"
	if len(slain) == 1 {
		was = "was"
	}

	return fmt.Sprintf("%s %s slain (+%d XP to everyone in the fight)", killPhrase(slain), was, total)
}

// killSoloSummary renders one bubble turn's monster deaths for the chat/
// combat log when exactly one player was in the bubble at award time
// (playtest item 3): named, past-tense, active voice — "NAME slew a wolf
// (+20 XP)" — instead of killSummary's nameless passive wording (which
// stays for a multi-player bubble, where kill credit deliberately does not
// exist). Mirrors killSummary's grouping (killPhrase) and summed-XP quoting
// for mixed-kind kills in the same turn.
func killSoloSummary(playerName string, slain []*monsterDef) string {
	if len(slain) == 0 {
		return ""
	}

	total := 0
	for _, k := range slain {
		total += k.xp
	}

	return fmt.Sprintf("%s slew %s (+%d XP)", playerName, killPhrase(slain), total)
}

// killPhrase joins slain into an English noun-phrase list — "a wolf",
// "2 ghouls", "a wolf and a troll", "a wolf, a troll and a dragon" — using
// each kind's first-appearance position in slain to order the groups (a
// stable, deterministic order given a deterministic slain, without forcing
// an unrelated alphabetical resort).
func killPhrase(slain []*monsterDef) string {
	counts := make(map[string]int, len(slain))

	var order []string

	for _, k := range slain {
		if counts[k.id] == 0 {
			order = append(order, k.id)
		}

		counts[k.id]++
	}

	phrases := make([]string, 0, len(order))

	for _, id := range order {
		if n := counts[id]; n == 1 {
			phrases = append(phrases, "a "+id)
		} else {
			phrases = append(phrases, fmt.Sprintf("%d %s", n, pluralizeKind(id)))
		}
	}

	//nolint:mnd // list-grammar arities (one phrase, a pair, three-or-more), not tuning knobs.
	switch len(phrases) {
	case 1:
		return phrases[0]
	case 2:
		return phrases[0] + " and " + phrases[1]
	default:
		return strings.Join(phrases[:len(phrases)-1], ", ") + " and " + phrases[len(phrases)-1]
	}
}

// pluralizeKind returns a monster-kind id's plural noun for the kill
// summary: irregular where English needs it (wolf -> wolves), a simple
// appended "s" otherwise (every other launch kind — rat, ghoul, troll,
// dragon — pluralizes regularly).
func pluralizeKind(id string) string {
	if id == idKindWolf {
		return "wolves"
	}

	return id + "s"
}

// levelFor returns the 1-based level for a cumulative XP total.
func levelFor(xp int) int { return 1 + xp/protocol.XPPerLevel }

// syncMaxHPLocked recalibrates a player's maxHP to its class and current level
// (via maxHPFor) after an XP change, clamping current HP to the new max. It does
// not heal: a level-up raises the ceiling but keeps current HP (respawn resets
// hp=maxHP separately). Callers hold w.mu; call only for players (a monster's
// empty class would resolve to the fallback base).
func syncMaxHPLocked(e *entity) {
	e.maxHP = maxHPFor(e.class, levelFor(e.xp))
	if e.hp > e.maxHP {
		e.hp = e.maxHP
	}
}

// levelFloorXP returns the XP at the start of xp's current level.
func levelFloorXP(xp int) int { return (xp / protocol.XPPerLevel) * protocol.XPPerLevel }

// resolveDeathsLocked floors a dying player's XP to its level start, removes dead
// monsters (rolling each one's ground-loot drop first — dropLootLocked), and
// respawns dead players (full HP, fresh spawn hex, same id + token — the
// client stays joined) among the given member set. It returns the kind of
// every monster that died, one entry per dead monster in members' id-sorted
// order (not deduplicated) — the kill-XP award and kill-summary announce
// live in the bubble-resolution path (resolveBubbleTurnLocked), so a kill
// only pays inside a real fight. The death-floor here still applies to ANY
// player death, world or bubble. rng is the resolution's shared turn RNG
// (resolveCombatLocked/ResolveCombatOnlyForTest) — one drop roll per dead
// monster, consumed in the same id-sorted order as the rest of this pass, so
// a full turn stays reproducible from the seed alone. Callers hold w.mu.
func (w *World) resolveDeathsLocked(rng *mrand.Rand, members []*entity) []*monsterDef {
	var dead []*entity

	var slain []*monsterDef

	for _, e := range members {
		if e.hp <= 0 {
			dead = append(dead, e)

			if e.kind == protocol.EntityMonster {
				if k := kindOf(e); k != nil {
					slain = append(slain, k)
				}
			}
		}
	}

	// Sort by id so simultaneous respawns claim spawn hexes in a deterministic
	// order (the map range above is unordered) — keeps a full turn reproducible.
	slices.SortFunc(dead, func(a, b *entity) int { return int(a.id - b.id) })

	for _, e := range dead {
		if e.kind == protocol.EntityMonster {
			w.logger.Info(combatLogMsg, "event", combatEventDeath, "id", e.id, "kind", e.kind,
				"monster_kind", e.monsterKind, "at", e.hex)

			w.dropLootLocked(rng, kindOf(e), e.hex)
			delete(w.entities, e.id)

			continue
		}

		w.logger.Info(combatLogMsg, "event", combatEventDeath, "id", e.id, "kind", e.kind, "at", e.hex)

		// Player: fall back to the start of the XP level you were in — keep the
		// level, lose the within-level progress — then respawn in place of a
		// re-join. The death is announced to the chat/combat log — previously
		// the only combat event with zero textual feedback. Test-bridge
		// players have no name; skip the announce rather than print " died".
		if e.name != "" {
			w.announce("system", e.name+" died")
		}

		e.xp = levelFloorXP(e.xp)

		if spawn, err := w.spawnHexLocked(); err == nil {
			e.hex = spawn
		}

		// Recompute maxHP from the class and post-floor level so a leveled player
		// respawns with its full, level-scaled bar (via the same maxHPFor source).
		e.maxHP = maxHPFor(e.class, levelFor(e.xp))
		e.hp = e.maxHP
		e.path = nil
		e.pending = pendingItemAction{}
	}

	return slain
}

// dropLootLocked rolls a slain monster's ground-loot drop: k's own
// dropChance (out of percentBase) chance of anything at all, and if it
// hits, one weight-weighted def from k's own drops table (pickDropFrom)
// lands on at — the monster's death hex — as a fresh item instance (id
// minted from the shared nextID sequence, same as entities and owned
// items). A miss, or an empty drops table, drops nothing. k nil (a
// malformed monster entity — never produced by a real spawn path) is a
// defensive no-op. Callers hold w.mu.
func (w *World) dropLootLocked(rng *mrand.Rand, k *monsterDef, at protocol.Hex) {
	if k == nil {
		return
	}

	if rng.IntN(percentBase) >= k.dropChance {
		return
	}

	defID := pickDropFrom(rng, k.drops)
	if defID == "" {
		return
	}

	w.nextID++

	inst := itemInstance{id: w.nextID, defID: defID}
	w.groundItems[at] = append(w.groundItems[at], inst)
}

// takeItemLocked gives ground item it a home on player e in the spec's
// priority order: merge into an existing consumable stack (the merged
// instance's own id disappears — the stack keeps its representative
// instance and just counts up), else a free backpack entry, else fails
// (false; the caller leaves the item where it was). Items never auto-equip
// on pickup, even into an empty matching slot — equipping is always an
// explicit action. Callers hold w.mu.
func (w *World) takeItemLocked(e *entity, it itemInstance) bool {
	if idx := e.stackIndexFor(it.defID); idx >= 0 {
		e.backpack[idx].count++

		return true
	}

	if idx := e.freeBackpackIndex(); idx >= 0 {
		e.backpack[idx] = backpackEntry{inst: it, count: 1}

		return true
	}

	return false
}

// spawnStream is a fixed PCG stream for monster placement, distinct from the
// per-turn move-shuffle stream (which uses the turn number).
const spawnStream uint64 = 0x5EED

// tooCloseToMonsterLocked reports whether h is occupied by, or within
// CombatRadius of, any living monster — spawning a player there would either
// land them ON a monster or form an instant, faction-blind combat bubble the
// moment they appear (both observed live, #36). Callers hold w.mu.
func (w *World) tooCloseToMonsterLocked(h protocol.Hex) bool {
	for _, e := range w.entities {
		if e.kind == protocol.EntityMonster && e.hp > 0 && HexDistance(h, e.hex) <= protocol.CombatRadius {
			return true
		}
	}

	return false
}

// occupiedByMonsterLocked reports whether h is directly on a living
// monster's hex — the distance-0 case tooCloseToMonsterLocked also covers,
// split out because spawnHexLocked's fallback ladder relaxes the "within
// CombatRadius" preference (a crowded clearing may leave no hex outside it)
// before it EVER relaxes "not literally on top of one": a monster co-located
// with its own target pathfinds itself-to-itself (empty path) and never
// bumps (thinkMonstersLocked's co-location dormancy), so landing a spawn
// there doesn't just risk an instant bubble — it can silently stall combat
// forever. Callers hold w.mu.
func (w *World) occupiedByMonsterLocked(h protocol.Hex) bool {
	for _, e := range w.entities {
		if e.kind == protocol.EntityMonster && e.hp > 0 && e.hex == h {
			return true
		}
	}

	return false
}

// tooCloseToPlayerLocked mirrors tooCloseToMonsterLocked for monster
// placement: h must not be occupied by, or within CombatRadius of, any living
// player, so a spawned monster can't stall a run by landing on top of (or
// instantly bubbling with) someone (#36, the task-6 testing mid-run stall).
// Callers hold w.mu.
func (w *World) tooCloseToPlayerLocked(h protocol.Hex) bool {
	for _, e := range w.entities {
		if e.kind == protocol.EntityPlayer && e.hp > 0 && HexDistance(h, e.hex) <= protocol.CombatRadius {
			return true
		}
	}

	return false
}

// tooCloseToSanctuaryLocked reports whether h is within protocol.SanctuaryRadius
// of the origin — the permanent monster-free zone (milestone 6c, the seed of
// a future trade hub), distinct from tooCloseToPlayerLocked's spawn-moment
// player-proximity guard. Reads no entity state; named -Locked and given a
// receiver for symmetry with the other spawn guards it's always applied
// alongside. Callers hold w.mu.
func (*World) tooCloseToSanctuaryLocked(h protocol.Hex) bool {
	return HexDistance(protocol.Hex{Q: 0, R: 0}, h) <= protocol.SanctuaryRadius
}

// SpawnMonsters adds n monster entities at random walkable hexes, chosen
// with the world seed so a given seed is reproducible: placement is
// distributed across the map's difficulty rings (ringOf, worldgen.go)
// weighted by each ring's candidate-hex count (a proxy for its area that is
// naturally terrain-aware — water/rock reduce a ring's usable area too),
// and each placement picks a kind uniformly among the kinds registered for
// that ring (content.go's monsterDefs' own rings field), capping dragon at
// protocol.DragonCount for the whole call. Skips hexes already at
// StackCap and, when at least one candidate allows it, hexes on/within
// CombatRadius of a living player (tooCloseToPlayerLocked — #36) or within
// protocol.SanctuaryRadius of the origin (tooCloseToSanctuaryLocked — 6c);
// if EVERY walkable hex fails one of those guards, both are dropped
// entirely for this call rather than placing nothing (the pre-#36
// behavior, so a tiny or crowded map never silently spawns fewer monsters
// than requested for lack of a "safe" hex). Intended for **startup, before
// any player joins** (server startup via MONSTER_COUNT, or tests), where
// the player guard is inert today — it exists for a future
// continuous/respawn spawner called mid-run.
func (w *World) SpawnMonsters(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	//nolint:gosec // deterministic seeded placement, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), spawnStream))

	byRing, ringWeights := w.spawnCandidatesByRingLocked(rng)
	kindsByRing := kindsPerRing()

	// The dragon cap is per WORLD, not per call: start from the dragons
	// already alive (a previous SpawnMonsters call, or a SpawnMonsterKindAt-
	// seeded one), so the future continuous/density spawner calling this
	// again mid-run can never stack a second dragon past DragonCount.
	dragonsPlaced := w.livingDragonsLocked()
	placed := 0

	for placed < n {
		h, r, ok := nextSpawnHexLocked(rng, byRing, ringWeights)
		if !ok {
			break // every ring is out of both weight and candidates
		}

		if w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		kindID, ok := pickSpawnKind(rng, kindsByRing[r], dragonsPlaced)
		if !ok {
			continue // ring exhausted of spawnable kinds (dragon-only ring, cap reached)
		}

		if kindID == idKindDragon {
			dragonsPlaced++
		}

		k := monsterDefByID[kindID]

		w.nextID++
		w.entities[w.nextID] = &entity{
			id: w.nextID, hex: h,
			kind: protocol.EntityMonster, monsterKind: k.id, hp: k.maxHP, maxHP: k.maxHP,
		}
		placed++
	}
}

// livingDragonsLocked counts the living dragon-kind monsters currently in
// the world — SpawnMonsters' starting point for the per-WORLD dragon cap
// (protocol.DragonCount), so repeated spawn calls (or a test/future-spawner
// mix of SpawnMonsterKindAt and SpawnMonsters) never accumulate dragons
// past the cap. Callers hold w.mu.
func (w *World) livingDragonsLocked() int {
	n := 0

	for _, e := range w.entities {
		if e.kind == protocol.EntityMonster && e.hp > 0 && e.monsterKind == idKindDragon {
			n++
		}
	}

	return n
}

// spawnCandidatesByRingLocked gathers every walkable candidate hex (the
// safe/unguarded-fallback tiers SpawnMonsters' doc comment describes),
// shuffles each ring's bucket with rng, and returns the per-ring hex
// buckets alongside their initial weights (candidate count — the area
// proxy). Callers hold w.mu.
func (w *World) spawnCandidatesByRingLocked(rng *mrand.Rand) ([][]protocol.Hex, []int) {
	var safe, unguarded []protocol.Hex

	for _, t := range w.worldMap.Tiles {
		if !w.walkableLocked(t.Hex) {
			continue
		}

		unguarded = append(unguarded, t.Hex)

		if !w.tooCloseToPlayerLocked(t.Hex) && !w.tooCloseToSanctuaryLocked(t.Hex) {
			safe = append(safe, t.Hex)
		}
	}

	walkable := safe
	if len(walkable) == 0 {
		walkable = unguarded
	}

	slices.SortFunc(walkable, compareHexQR)

	byRing := make([][]protocol.Hex, protocol.RingCount)

	for _, h := range walkable {
		r := ringOf(h, w.radius)
		byRing[r] = append(byRing[r], h)
	}

	ringWeights := make([]int, protocol.RingCount)

	for r, hexes := range byRing {
		rng.Shuffle(len(hexes), func(i, j int) { hexes[i], hexes[j] = hexes[j], hexes[i] })
		ringWeights[r] = len(hexes)
	}

	return byRing, ringWeights
}

// nextSpawnHexLocked draws one ring-weighted hex from byRing, popping it
// off that ring's bucket and zeroing the ring's weight once its bucket
// empties (so it's never picked again). ok is false once every ring is out
// of both weight and candidates. Callers hold w.mu (byRing/ringWeights are
// SpawnMonsters-local, but ringOf/occupancy-adjacent state justifies the
// same locking discipline as its caller).
func nextSpawnHexLocked(rng *mrand.Rand, byRing [][]protocol.Hex, ringWeights []int) (protocol.Hex, int, bool) {
	for {
		r, ok := weightedRingPick(rng, ringWeights)
		if !ok {
			return protocol.Hex{}, 0, false
		}

		if len(byRing[r]) == 0 {
			ringWeights[r] = 0 // exhausted candidates; never pick this ring again

			continue
		}

		h := byRing[r][len(byRing[r])-1]
		byRing[r] = byRing[r][:len(byRing[r])-1]

		return h, r, true
	}
}

// pickSpawnKind draws one kind uniformly from ringKinds, excluding dragon
// once dragonsPlaced has reached protocol.DragonCount. ok is false if that
// exclusion leaves nothing to pick (a dragon-only ring whose cap is
// already reached).
func pickSpawnKind(rng *mrand.Rand, ringKinds []string, dragonsPlaced int) (string, bool) {
	kinds := ringKinds
	if dragonsPlaced >= protocol.DragonCount {
		kinds = excludeKind(kinds, idKindDragon)
	}

	if len(kinds) == 0 {
		return "", false
	}

	return kinds[rng.IntN(len(kinds))], true
}

// SpawnMonsterAt spawns a single monster at h, returning whether it spawned. It
// refuses a non-walkable hex or one already at StackCap. Unlike SpawnMonsters
// (random, world-seeded placement) it puts a monster at a caller-chosen hex, so
// a caller can seed a known-position monster — e.g. an integration test that
// needs a monster a couple hexes from where a player is (or will be), for a
// short, deterministic chase or an immediate fight. It mirrors SpawnMonsters'
// entity shape (kind monster, MonsterMaxHP). Like SpawnMonsters it is a
// startup primitive meant to run before Run: it does not recompute bubbles
// (Run does that each tick) and does not avoid opposing occupants.
//
// Unlike SpawnMonsters/spawnHexLocked it does NOT apply the #36
// too-close-to-a-player guard: this API names exactly one caller-chosen hex
// with no alternative candidate to fall back to, and both of those guarded
// callers fall back to placing anyway when nothing else qualifies — applying
// the same guard here would only ever produce that same fallback, silently.
// A caller that needs a guaranteed-clear hex should choose one itself
// (tooCloseToPlayerLocked is unexported, but SpawnMonsters' random search
// already does this). Holds w.mu.
func (w *World) SpawnMonsterAt(h protocol.Hex) bool {
	return w.SpawnMonsterKindAt(h, defaultMonsterKindID)
}

// SpawnMonsterKindAt is SpawnMonsterAt for a caller-chosen monster kind
// (content.go's monsterDefs id) — lets a test or a future ring-aware spawner
// seed a specific kind at a specific hex. Panics if kind is not registered
// (a content bug, not a runtime condition a caller should need to handle).
func (w *World) SpawnMonsterKindAt(h protocol.Hex, kind string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	k, ok := monsterDefByID[kind]
	if !ok {
		panic("game: SpawnMonsterKindAt unknown monster kind " + kind)
	}

	if !w.walkableLocked(h) || w.occupancyLocked(h) >= protocol.StackCap {
		return false
	}

	w.nextID++
	w.entities[w.nextID] = &entity{
		id: w.nextID, hex: h,
		kind: protocol.EntityMonster, monsterKind: k.id, hp: k.maxHP, maxHP: k.maxHP,
	}

	return true
}

// thinkMonstersLocked sets each monster in the member set to a single step
// toward its nearest player among `targets`. Recomputed every turn (players
// move). The two domains scope targets differently: a bubble's monsters chase
// only that bubble's players (a frozen fight stays self-contained), while
// WORLD monsters chase the nearest player anywhere — including one frozen in
// a bubble — so the world keeps running (§5) and an approaching monster is
// absorbed by the bubble recompute the moment it closes within CombatRadius
// of a bubbled player (walk-in reinforcement).
//
// worldDomain gates that WORLD chase behind aggro range (#36): a WORLD-domain
// monster only picks a target among players within THEIR OWN effective aggro
// radius (aggroRadiusForLocked — nearestAggroedPlayerLocked does the
// filtering); if nobody qualifies it stands still (no wander this slice) —
// see rng's doc comment on why it's threaded in even though no content uses
// evAggroRange yet. A bubble's monsters (worldDomain false) keep chasing
// unconditionally — a fight is a fight, aggro range does not apply once
// you're already in one. Callers hold w.mu.
//
// When adjacent, path[0] is the player's own hex, so the move phase converts
// this into a bump-to-attack (6.3).
func (w *World) thinkMonstersLocked(rng *mrand.Rand, members, targets []*entity, worldDomain bool) {
	if len(targets) == 0 {
		return
	}

	for _, m := range members {
		if m.kind != protocol.EntityMonster {
			continue
		}

		var target *entity
		if worldDomain {
			target = w.nearestAggroedPlayerLocked(rng, m, targets)
			if target == nil {
				m.path = nil // nobody within their own aggro range: stand still

				continue
			}
		} else {
			target = nearestPlayer(m.hex, targets)
		}

		path := Pathfind(m.hex, target.hex, w.walkableLocked)
		// Step toward the target; when adjacent, path[0] is the player's own
		// hex, so the move phase converts this into a bump-to-attack (6.3).
		if len(path) >= 1 {
			m.path = []protocol.Hex{path[0]}
		} else {
			m.path = nil
		}
	}
}

// nearestAggroedPlayerLocked returns the player nearest monster m among
// `players`, considering only players within THEIR OWN effective aggro
// radius of m (aggroRadiusForLocked, based on m's kind's own aggroRadius —
// gear/species can further make one player more or less noticeable than
// another), ties broken by lowest id like nearestPlayer. Returns nil if no
// player qualifies — the monster notices nobody. Callers hold w.mu.
func (w *World) nearestAggroedPlayerLocked(rng *mrand.Rand, m *entity, players []*entity) *entity {
	base := baseAggroRadiusFor(m)

	var best *entity

	bestDist := 0

	for _, p := range players {
		d := HexDistance(m.hex, p.hex)
		if d > aggroRadiusForLocked(rng, base, p) {
			continue
		}

		if best == nil || d < bestDist || (d == bestDist && p.id < best.id) {
			best, bestDist = p, d
		}
	}

	return best
}

// baseAggroRadiusFor returns monster m's own base aggro radius before any
// player-side noticeability fold: its kind's aggroRadius override
// (monsterDef.aggroRadius) if non-zero, else the shared
// protocol.MonsterAggroRadius default. m is assumed to be a monster (the
// only caller, nearestAggroedPlayerLocked, only ever calls this for one);
// kindOf(m) nil (a malformed fixture) falls back to the default too.
func baseAggroRadiusFor(m *entity) int {
	if k := kindOf(m); k != nil && k.aggroRadius != 0 {
		return k.aggroRadius
	}

	return protocol.MonsterAggroRadius
}

// aggroRadiusForLocked returns the hex radius at which a WORLD-domain
// monster with base aggro radius `base` (baseAggroRadiusFor — per-kind since
// 6c) notices player p: base folded through p's own noticeability rule
// cards (species + every equipped item's rules, in canonicalSlotOrder —
// mirroring rollDamageLocked's victimCards fold: any gear on the entity can
// contribute, not just the "acting" slot) via the evAggroRange event. No
// content defines an evAggroRange card yet, so this is a no-op fold today —
// the hook exists so a future sneaky/loud item can shrink or grow it
// without touching this call site. Callers hold w.mu.
func aggroRadiusForLocked(rng *mrand.Rand, base int, p *entity) int {
	cards := slices.Concat(speciesCards(p.species), equippedRuleCards(p))

	return applyRules(evAggroRange, base, cards, ruleCtx{attacker: p, rng: rng})
}

// playersOf filters the player entities out of a member set, preserving order.
func playersOf(members []*entity) []*entity {
	players := make([]*entity, 0, len(members))

	for _, e := range members {
		if e.kind == protocol.EntityPlayer {
			players = append(players, e)
		}
	}

	return players
}

// allPlayersLocked returns every player in the world regardless of domain,
// sorted by id (the deterministic nearest-player tie-break). Callers hold w.mu.
func (w *World) allPlayersLocked() []*entity {
	players := make([]*entity, 0, len(w.entities))

	for _, e := range w.entities {
		if e.kind == protocol.EntityPlayer {
			players = append(players, e)
		}
	}

	slices.SortFunc(players, func(a, b *entity) int { return int(a.id - b.id) })

	return players
}

// nearestPlayer returns the player closest to `from` by hex distance, ties
// broken by lowest id for determinism. `players` must be non-empty.
func nearestPlayer(from protocol.Hex, players []*entity) *entity {
	best := players[0]
	bestDist := HexDistance(from, best.hex)

	for _, p := range players[1:] {
		d := HexDistance(from, p.hex)
		if d < bestDist || (d == bestDist && p.id < best.id) {
			best, bestDist = p, d
		}
	}

	return best
}

// spawnPointStream is a fixed PCG stream salt for player spawn-point
// selection, distinct from spawnStream (initial monster placement). Combined
// with w.nextID (ever-increasing — it advances on every entity/item mint), it
// gives each spawnHexLocked call its own draw while staying reproducible for
// a fixed world seed and call sequence (#36 — random spawn points instead of
// every join/respawn racing to pile onto the exact same tile).
const spawnPointStream uint64 = 0x50A5

// spawnHexLocked picks a hex for a player join or respawn: a random
// walkable, capacity-available hex in the origin's forced clearing
// (worldgen.go's clearingRadius) that is not occupied by, or within
// CombatRadius of, a living monster (tooCloseToMonsterLocked) — so a spawn
// can never land a player ON a monster or form an instant combat bubble the
// moment they appear (both observed live, #36). Random, not the old
// spiral-nearest-to-origin search: players (and respawns) no longer pile
// deterministically onto the same hex.
//
// Four tiers, each engaged only if the one above yields nothing, so a small
// or crowded map never fails a join outright — but "not literally on top of a
// monster" is relaxed dead last, since that specific case can silently stall
// combat forever (occupiedByMonsterLocked's doc comment), not just risk an
// instant bubble:
//  1. clearing hexes clear of monsters entirely (the common case)
//  2. clearing hexes not occupied by one, ignoring the CombatRadius
//     preference (a monster-dense clearing may leave nothing outside it)
//  3. clearing hexes at all, ignoring both monster checks (the clearing
//     itself is saturated — every hex in it has a monster standing on it)
//  4. spawnHexSpiralLocked over the WHOLE reachable region, ignoring every
//     guard — the pre-#36 search, kept verbatim as the last resort so "a
//     crowded tiny test map must not break joins" still holds
//
// Callers hold w.mu.
func (w *World) spawnHexLocked() (protocol.Hex, error) {
	origin := protocol.Hex{Q: 0, R: 0}

	var clearingSafe, clearingUnoccupied, clearingAny []protocol.Hex

	for h := range w.spawnable {
		if HexDistance(origin, h) > clearingRadius || w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		clearingAny = append(clearingAny, h)

		if w.occupiedByMonsterLocked(h) {
			continue
		}

		clearingUnoccupied = append(clearingUnoccupied, h)

		if !w.tooCloseToMonsterLocked(h) {
			clearingSafe = append(clearingSafe, h)
		}
	}

	candidates := clearingSafe
	if len(candidates) == 0 {
		candidates = clearingUnoccupied
	}

	if len(candidates) == 0 {
		candidates = clearingAny
	}

	if len(candidates) == 0 {
		return w.spawnHexSpiralLocked()
	}

	slices.SortFunc(candidates, compareHexQR)

	//nolint:gosec // deterministic seeded placement, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), spawnPointStream+uint64(w.nextID)))

	return candidates[rng.IntN(len(candidates))], nil
}

// spawnHexSpiralLocked is the pre-#36 search: the free walkable hex nearest
// the origin, spiraling outward, ignoring the monster guard entirely — the
// tier-3 fallback spawnHexLocked reaches for only when neither clearing tier
// above yields a single candidate (an extremely crowded or tiny map), so a
// join never hard-fails just because the origin clearing is exhausted.
// Callers hold w.mu.
//
// Faction-blind by design in this fallback path: it can land a player on a
// monster-occupied hex (opposing co-occupancy, a §5 MUST in the rare case it
// is ever reached). It is inert only because a co-located monster's think
// step gets Pathfind(from==to)==∅ and holds (never bumps).
func (w *World) spawnHexSpiralLocked() (protocol.Hex, error) {
	origin := protocol.Hex{Q: 0, R: 0}

	for radius := 0; radius <= w.radius; radius++ {
		for q := -radius; q <= radius; q++ {
			for r := -radius; r <= radius; r++ {
				h := protocol.Hex{Q: q, R: r}
				if HexDistance(origin, h) != radius {
					continue
				}

				// w.spawnable[h] already implies walkable; using it (rather than
				// walkableLocked) keeps spawns off any walkable pocket the origin
				// can't reach.
				if w.spawnable[h] && w.occupancyLocked(h) < protocol.StackCap {
					return h, nil
				}
			}
		}
	}

	return protocol.Hex{}, ErrWorldFull
}

func (w *World) walkableLocked(h protocol.Hex) bool {
	t, ok := w.terrain[h]

	return ok && (t == protocol.TerrainGrass || t == protocol.TerrainForest)
}

func (w *World) occupancyLocked(h protocol.Hex) int {
	n := 0

	for _, e := range w.entities {
		if e.hex == h {
			n++
		}
	}

	return n
}
