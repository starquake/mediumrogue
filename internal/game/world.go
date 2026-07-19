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
	"math"
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
// analytics log (docs/design.md §12). Filter on this key (msg ==
// "combat") to isolate the sim's structured event stream from ordinary
// server logs. combatEvent* names the "event" attribute's fixed vocabulary.
const combatLogMsg = "combat"

const (
	combatEventMove   = "move"
	combatEventAttack = "attack"
	combatEventLeash  = "leash"
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
	// ErrNotEquippable rejects an equip intent naming an item whose type has
	// no equip slot at all (a consumable — drink, not equip, is its action).
	// Class gates are gone (gear keystone, #56): anyone may equip anything
	// that HAS a slot.
	ErrNotEquippable = errors.New("that item can't be equipped")
	// ErrNoSuchGroundItem rejects a pickup intent naming a ground item that
	// is not lying on the player's own hex (stale id, or an item elsewhere).
	ErrNoSuchGroundItem = errors.New("no such item here")
	// ErrNoSuchSkill rejects a learn intent naming an unregistered skill.
	// It and the four sentinels below are the learn-skill rejections (#124);
	// all five are 422s — the request was well-formed and the world simply
	// says no.
	ErrNoSuchSkill = errors.New("no such skill")
	// ErrSkillAlreadyLearned rejects re-learning — points are spent once and
	// there is no respec in v1.
	ErrSkillAlreadyLearned = errors.New("skill already learned")
	// ErrSkillPrereqUnmet rejects a skill whose prerequisites are not all
	// learned. Near-sightedness means the client should never OFFER such a
	// skill, so this fires on a stale or hand-made request.
	ErrSkillPrereqUnmet = errors.New("prerequisite not learned")
	// ErrNoSkillPoints rejects learning with an empty bank.
	ErrNoSkillPoints = errors.New("no skill points to spend")
	// ErrLearnInCombat rejects learning inside a combat bubble. Deliberately
	// NOT queued like the inventory actions: learning is a between-fights
	// decision, not a turn's action, so it costs no bubble turn and needs no
	// pending-action plumbing.
	ErrLearnInCombat = errors.New("can't learn a skill in combat")
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
	// learned is the ids of this player's learned skills (#124), kept SORTED
	// so skillCards folds them in a stable order no matter what order they
	// were learned in — determinism is load-bearing and a map-derived or
	// insertion-ordered slice would leak into the pipeline.
	learned []string
	// skillPoints is the unspent bank. Earned per level (grantSkillPointsLocked).
	skillPoints int
	// pointsGrantedLevel is the highest level this player has ALREADY been
	// paid for — the high-water mark that makes the award idempotent. There
	// is no level-up event in the engine (level is derived from xp via
	// levelFor), and death floors xp to levelFloorXP, so re-earning the same
	// level must not re-grant. Never decreases.
	pointsGrantedLevel int
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
	// attackTargetEntity is a pending entity-targeted attack's victim, named
	// by entity id (item 7, playtest batch 2) — 0 for none, or for a
	// ground-targeted attack (see attackTarget). Resolution aims at this
	// entity's PRE-MOVE hex (#104 — attacks resolve before moves), so a
	// sidestepping or retreating victim is tracked by id rather than a stale
	// hex. If the victim is adjacent at resolution, this resolves as an
	// EXCLUSIVE melee swing (#116, every held melee weapon); otherwise it
	// hits if still within a held ranged/magic weapon's range from the
	// shooter's own pre-move hex, else fizzles defensively
	// (resolveEntityTargetedLocked).
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
	// equipped is the entity's worn/wielded gear, keyed by slot (the gear
	// keystone's 8 slots: main-hand + off-hand — a weapon's slot is a hand
	// chosen at equip time, weaponTargetSlot — plus the six universal
	// armor/jewelry slots, whose keys equal their itemType; see slotForType
	// in items.go). Class gates are gone: any class may equip any item that
	// has a slot. Players only; monsters own no items and fight with their
	// kind's own claws profile (monsterDef.claws). Granted at Join by
	// grantDefaultsLocked; gear survives death (never cleared by respawn).
	// May be nil on a monster/zero-value fixture — equippedDefIn treats nil
	// as all-empty.
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
	// homeHex is a monster's home tile (#102): the hex it spawned on, stamped
	// by every monster spawn path (SpawnMonsters, SpawnMonsterKindAt,
	// PlaceMonsterKindForTest) and never re-stamped afterwards. A WORLD-domain
	// monster farther than leashRadiusFor from it drops any chase and paths
	// back (thinkReturnHomeLocked). Monsters only; players leave it zero — the
	// zero value is the origin hex, which is fine because no player code path
	// ever reads it.
	homeHex protocol.Hex
	// returningHome marks a monster walking back to homeHex after exceeding
	// its leash (#102): while set, the WORLD-domain think pass ignores players
	// entirely — no re-aggro mid-return — until arrivedHomeLocked clears it.
	// Bubble membership overrides it (a bubbled monster chases
	// unconditionally); the flag survives the bubble so an interrupted return
	// resumes when the monster re-enters the world domain. Persisted (it is
	// multi-turn behavioral state, not a per-turn transient like path).
	returningHome bool
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
	// logger receives the structured "combat" event stream (moves, melee
	// attacks, ranged hits/fizzles, deaths, kill-XP awards, pickups) — the seed of the
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
	// hex. Populated by dropLootLocked (a slain monster's death hex) and by a
	// player drop (dropItemLocked, inventory.go); drained one item at a time
	// by an explicit pickup intent (pickupGroundLocked — walk-over auto-pickup
	// was removed with the inventory system). Instance ids are minted from the
	// same nextID sequence as entities and owned items — unique across the
	// whole world. Each entry is a groundStack (a consumable stack drops
	// whole; gear/loot is count 1).
	groundItems map[protocol.Hex][]groundStack
	// archive holds characters removed by the disconnect sweep (or loaded
	// from a snapshot that was never re-claimed live): identity, XP, and
	// gear, keyed by token. sweepDisconnectedLocked populates an entry in
	// place of discarding a player's progress; Join consumes (deletes) the
	// entry on a rejoin with that token, restoring a fresh entity from it.
	// Never touched for monsters (no token). Party/quest membership is NOT
	// archived — that is session-scoped social state, not progression (see
	// sweepDisconnectedLocked).
	archive map[string]characterRecord
	// recentHits is every hit landed in the last hitRetentionTurns turn
	// resolutions (#114): appended by rollDamageLocked (the single choke
	// point every damage number flows through), pruned by advanceTurnLocked,
	// and carried on every bundle as TurnEvent.Hits so the client can render
	// crit/glance moments. Transient cosmetics: deliberately NOT persisted in
	// the snapshot (like entity.path), so no snapshot version bump.
	recentHits []hitRecord
}

// hitRecord is one landed hit, kept for the turn bundle's Hits view (#114).
// turn is the resolution that produced it — w.turn+1 at record time, since
// hits resolve before advanceTurnLocked increments the counter, and the
// bundle broadcast for that resolution carries the incremented number.
type hitRecord struct {
	turn     int64
	attacker int64
	victim   int64
	amount   int
	crit     bool
	glance   bool
}

// hitRetentionTurns is how many turn resolutions a hitRecord rides bundles
// for before advanceTurnLocked prunes it. More than one, because SSE ticks
// coalesce (a slow client skips intermediate bundles yet should still see
// the moments it missed — it dedupes on HitView.Turn); small, because stale
// moments are dead wire weight.
const hitRetentionTurns = 4

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
		groundItems:     make(map[protocol.Hex][]groundStack),
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
//
// Acceptance is deliberately NOT clock-gated (#99): there is no server-side
// input-window cutoff, because none is needed for integrity — w.mu
// serializes every submission against the resolution passes (pollTick, and
// the lock-in resolution below), so an intent can never land mid-pass. One
// that arrives while a turn is resolving blocks until the pass completes,
// then queues as normal and applies to the NEXT turn. Rejecting "late"
// intents would punish clients that submit during playback for no gain.
// Pinned by intent_window_test.go.
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
	case protocol.IntentLearnSkill:
		return w.learnSkillLocked(e, req.SkillID)
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

	// #116: a walk that ends on a hostile-held hex stops adjacent — attacking
	// is an explicit attack intent, never a move. Board read at submit time:
	// a monster that wanders mid-walk just leaves the route one hex short,
	// like any stale path. Trimming to empty is a valid no-op move (already
	// adjacent — the client routes that click as an attack anyway). The
	// len(path) > 0 guard covers Pathfind's own no-op case (target ==
	// e.hex, e.g. a "stay"/ready move in a bubble): that returns a non-nil
	// EMPTY slice, which the path == nil check above does not catch, so an
	// unguarded path[len(path)-1] would panic. Only players reach
	// queueMoveLocked (intents are player-dispatched); the AI writes
	// monster paths directly and never passes through this trim.
	if len(path) > 0 && w.occupiedByMonsterLocked(path[len(path)-1]) {
		path = path[:len(path)-1]
	}

	e.path = path
	e.attackTarget = nil
	e.attackTargetEntity = 0
	e.pending = pendingItemAction{}

	return nil
}

// queueAttackLocked validates an attack intent and queues it. A non-zero
// targetEntityID (item 7, playtest batch 2) is ENTITY-targeted: the named
// entity must exist and be alive (else ErrAttackTargetNotFound), be a
// hostile — opposing faction (else ErrAttackTargetNotHostile) — and be in
// REACH from e's current hex at submit time, else ErrOutOfRange. Reach
// (#116) is unified: an ADJACENT victim (HexDistance == 1) is always
// attackable as melee — every entity is melee-armed (meleeDefsFor falls back
// to fists/claws), so there is no melee weapon gate here, only the ranged
// gate (below) remains conditional. Beyond distance 1, at least one held
// ranged/magic weapon must reach (rangedDefsFor — any-reach, not just best;
// task 2, dual-wield). A melee-only attacker (e.g. a Fighter, who holds no
// ranged weapon) naming a distant victim therefore rejects as ErrOutOfRange,
// not the old ErrNoRangedWeapon — that sentinel now belongs to the
// ground-targeted branch only (below), which has no melee equivalent (there
// is no hex-targeted melee). Resolution (#104) runs against the victim's
// pre-move hex (resolveEntityTargetedLocked/resolveRangedLocked) — the same
// position checked at submit, so a sidestep or retreat is tracked by id
// rather than a stale hex. Ranged routing no longer keys off any single
// "best" weapon's aoeRadius (task 2, dual-wield): a shooter dual-wielding a
// bow and a magic weapon still gets the bow's entity-targeted hit AND the
// magic weapon's AoE around the victim's hex, both from one entity-targeted
// intent — resolution already fires every reaching def this way regardless
// of which one happens to have the longest range.
//
// Anything else (targetEntityID 0 — e.g. a defensive/legacy hex-only shot)
// is GROUND-targeted at target, checked against e's current hex. There is no
// hex-targeted melee, so this branch keeps the old ErrNoRangedWeapon
// pre-gate (rangedDefFor(e) == nil — any-reach existence only, independent
// of this shot's distance) ahead of the per-shot ErrOutOfRange check.
//
// Either branch clears the route and any queued equip on success — an
// attack replaces the move AND the swap for this turn (the latest intent in
// the window wins).
//
// INVARIANT: max over every registered def's rangeHex+aoeRadius must stay <=
// CombatRadius (validateMaxReach, run at content load by mustValidateContent),
// so any entity a ranged attack can reach is always already in the shooter's
// combat bubble. If that invariant were ever violated, a monster could be
// ranged-killed in the WORLD domain (where resolveWorldTurnLocked awards no
// kill-XP) — add an in-bubble/target-in-member-set guard here then. Callers
// hold w.mu.
func (w *World) queueAttackLocked(e *entity, target protocol.Hex, targetEntityID int64) error {
	if targetEntityID != 0 {
		victim, ok := w.entities[targetEntityID]
		if !ok || victim.hp <= 0 {
			return ErrAttackTargetNotFound
		}

		if !opposing(e, victim) {
			return ErrAttackTargetNotHostile
		}

		// Reach (#116): an ADJACENT victim is always attackable — melee, and
		// every entity is melee-armed (meleeDefsFor falls back to fists/claws).
		// Beyond that, at least one held ranged/magic weapon must reach
		// (dual-wield: any, not best). A melee-only attacker naming a distant
		// victim therefore rejects as out-of-range, not as weaponless.
		dist := HexDistance(e.hex, victim.hex)
		if dist != 1 && len(rangedDefsFor(e, dist)) == 0 {
			return ErrOutOfRange
		}

		e.attackTargetEntity = targetEntityID
		e.attackTarget = nil
		e.path = nil
		e.pending = pendingItemAction{}

		return nil
	}

	// Ground-targeted (hex) attacks stay ranged-only: there is no hex-targeted
	// melee, so the old weapon gate lives on in this branch.
	if rangedDefFor(e) == nil {
		return ErrNoRangedWeapon
	}

	if len(rangedDefsFor(e, HexDistance(e.hex, target))) == 0 {
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

	if err := equipValidate(itemDefByID[inst.defID]); err != nil {
		return err
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

	if currentSlotOf(e, inst, def) == "" {
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

// queuePickupLocked validates and applies/queues a pickup of one ground stack
// from the player's own hex: the stack must lie there, and there must be room
// for at least one unit of it — a mergeable consumable stack that isn't full,
// or a free backpack entry (else ErrBackpackFull, validated here too so a
// doomed pickup 422s immediately instead of silently fizzling a whole bubble
// turn). A partial fit is allowed (pickupGroundLocked takes what fits and
// leaves the remainder). Callers hold w.mu.
func (w *World) queuePickupLocked(e *entity, groundItemID int64) error {
	found, _ := w.findGroundStackLocked(e.hex, groundItemID)
	if found == nil {
		return ErrNoSuchGroundItem
	}

	if !e.hasRoomForLocked(found.inst.defID) {
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
// Snapshot renders the turn bundle with NO viewer: own-only fields (skills,
// the point bank — #124) are omitted for every entity. Used by tests and any
// caller that isn't a player's own stream; the SSE handler calls SnapshotFor.
func (w *World) Snapshot() protocol.TurnEvent {
	return w.SnapshotFor("")
}

// SnapshotFor renders the turn bundle as the holder of viewerToken sees it.
// Skills and the unspent point bank are OWN-ONLY (#124 Q9): they are filled
// in on the viewer's own entity and left zero on everyone else's, so another
// player's build never reaches this client at all.
//
// Cost: one bundle per open stream per turn rather than one shared bundle —
// ~15 at this game's scale, which is why own-only was affordable. The hub's
// coalescing contract is untouched: this is still "fetch the latest state",
// just rendered per viewer.
func (w *World) SnapshotFor(viewerToken string) protocol.TurnEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	entities := w.entityViewsLocked()
	w.fillOwnOnlyLocked(entities, viewerToken)

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

	for hex, stacks := range w.groundItems {
		for _, gs := range stacks {
			def := itemDefByID[gs.inst.defID]
			groundItems = append(groundItems, protocol.GroundItemView{
				ID: gs.inst.id, Hex: hex, DefID: gs.inst.defID, Name: def.name, Type: def.itemType, Count: gs.count,
				// Detail fields (#139), read straight off the def like itemViewOf.
				Tags: wireTags(def), DamageType: def.damageType, TwoHanded: def.twoHanded,
				Damage: def.damage, RangeHex: def.rangeHex, AoERadius: def.aoeRadius, Desc: def.desc, Flavor: def.flavor,
			})
		}
	}

	slices.SortFunc(groundItems, func(a, b protocol.GroundItemView) int { return int(a.ID - b.ID) })

	// Hits ride in append order — already deterministic (hits are recorded in
	// resolution order under w.mu). Always non-nil, matching the generated TS
	// type's non-optional HitView[] (the itemViewsLocked precedent).
	hits := make([]protocol.HitView, 0, len(w.recentHits))
	for _, h := range w.recentHits {
		hits = append(hits, protocol.HitView{
			Turn: h.turn, AttackerID: h.attacker, VictimID: h.victim,
			Amount: h.amount, Crit: h.crit, Glance: h.glance,
		})
	}

	return protocol.TurnEvent{
		Turn: w.turn, IntervalMs: w.interval.Milliseconds(), Entities: entities, Bubbles: bubbles,
		Quests: questViews, GroundItems: groundItems, WorldID: w.worldID, Hits: hits,
	}
}

// itemViewsLocked builds the wire item list for one entity: an ItemView per
// owned item instance — equipped gear first, in canonicalSlotOrder (so the
// list order is deterministic despite e.equipped being a map), then backpack
// entries in index order. Tags/TwoHanded carry a weapon's tag set and
// two-handedness (see protocol.ItemView's doc comments). Always a non-nil
// slice — empty (not null) for a monster (which owns nothing) or a player who
// owns nothing, so the wire shape matches the generated TS type's
// non-optional ItemView[]. Callers hold w.mu.
func itemViewsLocked(e *entity) []protocol.ItemView {
	views := make([]protocol.ItemView, 0, len(e.equipped)+len(e.backpack))

	for _, slot := range canonicalSlotOrder {
		inst, ok := e.equipped[slot]
		if !ok || inst.id == 0 {
			continue
		}

		views = append(views, itemViewOf(inst, slot, 1))
	}

	for _, be := range e.backpack {
		if be.empty() {
			continue
		}

		views = append(views, itemViewOf(be.inst, "", be.count))
	}

	return views
}

// wireTags renders a def's weapon tags for the wire, never nil. A nil Go
// slice marshals to JSON `null`, but the generated TS type is a
// NON-OPTIONAL `tags: string[]` — so sending null was the server lying to
// the client about its own contract, and the client (reasonably) called
// .includes() on it. Every non-weapon has nil tags, so equipping ANY armor
// froze the client's turn handler: the exception escaped onTurn, rendering
// stopped, and SSE stayed connected — "connected but nothing moves".
//
// Same "always non-nil" rule the hits slice already follows (see
// SnapshotFor), applied to the one place that had slipped through.
func wireTags(def *itemDef) []string {
	if def.tags == nil {
		return []string{}
	}

	return def.tags
}

// itemViewOf renders one owned item instance for the wire. slot is the equip
// slot this instance currently occupies, or "" for a backpack entry. count is
// the stack size (1 for gear and equipped items). Type carries slot for an
// equipped item — for armor/jewelry that equals def.itemType already (a slot
// name IS the type), but for a weapon it is the occupied hand (SlotMainHand/
// SlotOffHand) rather than the generic "weapon" taxonomy string, so the wire
// (and the client's slot-keyed equipped map) can tell the two hands apart —
// the gear keystone's dual-wield model (protocol.ItemView's doc comment).
// An unequipped weapon (backpack) has no hand yet (weaponTargetSlot decides
// one at equip time), so it falls back to def.itemType like every other
// backpack entry.
func itemViewOf(inst itemInstance, slot string, count int) protocol.ItemView {
	def := itemDefByID[inst.defID]
	equipped := slot != ""
	viewType := def.itemType

	if equipped {
		viewType = slot
	}

	return protocol.ItemView{
		ID: inst.id, DefID: inst.defID, Name: def.name, Type: viewType,
		Tags: wireTags(def), DamageType: def.damageType, TwoHanded: def.twoHanded,
		Damage: def.damage, RangeHex: def.rangeHex, AoERadius: def.aoeRadius, Desc: def.desc,
		Flavor:   def.flavor,
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

// blockedFor reports whether hex h is closed to m on the evolving board (byHex):
// held by an opposing entity, or already full at StackCap. Terrain is not its
// business — w.terrain never mutates, so a step that was walkable when the route
// was queued still is; only occupancy can turn a queued step away.
//
// This is the single definition of "blocked": movePhaseLocked's wait rule and
// the #96 re-path predicate both read it, and they must agree — a re-route built
// on a looser rule would hand back a first step the move phase then refuses.
func blockedFor(m *entity, byHex map[protocol.Hex][]*entity, h protocol.Hex) bool {
	occs := byHex[h]

	return hasOpposing(occs, m) || len(occs) >= protocol.StackCap
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

// pendingAttack is a melee attack committed in the attack phase (#104,
// attacks-before-moves): a move intent whose next step was opposing-held on
// the PRE-MOVE board. The attacker stays put (path retained — a standing
// intent keeps attacking); target is the victim hex as it stood before any
// move this turn.
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
	w.advanceTurnLocked()
}

// advanceTurnLocked increments the turn counter at the end of a resolution
// (world-domain or bubble) and prunes hit records too old to ride any more
// bundles (#114 — see hitRetentionTurns). The single owner of w.turn++, so
// the prune can never be forgotten at one site. Callers hold w.mu.
func (w *World) advanceTurnLocked() {
	w.turn++
	w.recentHits = slices.DeleteFunc(w.recentHits, func(h hitRecord) bool {
		return h.turn <= w.turn-hitRetentionTurns
	})
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
				grantSkillPointsLocked(e)

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

	w.advanceTurnLocked()
}

// resolveCombatLocked runs the decided phased resolution over a given entity
// set: think → attack (simultaneous, pre-move positions — #104,
// attacks-before-moves) → move (faction-aware, melee attackers skipped) → apply
// damage & deaths. The set is a whole CombatRadius-connected domain (the
// world domain or one bubble), so no move,
// melee attack, stack, or attack can reach an entity outside it. worldDomain selects
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

	// NOTE: walk-over auto-pickup used to run here (between movement and the
	// attack phase, pre-#104). The inventory-slots milestone removed it —
	// picking up is now an explicit pickup INTENT (inventory.go), free
	// outside a bubble and a whole turn inside one, applied by the pending
	// pass above.

	// #104, attacks-before-moves: the attack phase resolves first, against
	// PRE-MOVE positions (byHex as built above), then movers advance. A
	// committed attack always lands; retreat trades hits for distance. Note
	// the rng-consumption order is contractual for determinism: melee victim
	// picks + damage folds draw first, the mover shuffle draws after.
	attacks, attacked := w.collectMeleeAttacksLocked(byHex, members)

	w.attackLocked(rng, byHex, attacks)

	w.movePhaseLocked(rng, byHex, members, attacked)

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
// take-damage cards (species + class + the rules of EVERYTHING the victim has
// equipped, folded in canonicalSlotOrder — a hit lands on the whole entity,
// not just the slot that happens to be attacking; this is how armor's
// take-damage cards apply). A monster victim folds its kind's claws rules
// instead (it never equips). weapon is the ONE hit currently resolving's
// acting weapon def — a single entry of meleeDefsFor for a melee attack or
// rangedDefsFor for a shot (task 2: every fitting held weapon fires its own
// hit, each folding through here separately); it is never nil — every combat
// site resolves a real def per hit (fists/claws fallback for an unarmed
// close attacker, a real equipped item for ranged, since an empty ranged def
// list never reaches here — see queueAttackLocked/resolveRangedLocked). Every
// damage number in the game flows through here. Callers hold w.mu.
func (w *World) rollDamageLocked(rng *mrand.Rand, attacker, victim *entity, weapon *itemDef, base int) int {
	ctx := ruleCtx{
		attacker: attacker, victim: victim, damageType: weapon.damageType, weapon: weapon,
		allyInBubble: w.allyInBubbleLocked(attacker), rng: rng,
	}

	// Order is CONTRACTUAL for determinism: species -> gear -> skills, with
	// skills appended LAST (#124 task 4) so every card that existed before
	// this slice keeps its position in the rng stream. A chance-conditioned
	// skill card consumes rng; putting skills anywhere but last would shift
	// every pinned seed in the repo.
	attackerCards := slices.Concat(speciesCards(attacker.species), weapon.rules, skillCards(attacker))
	dealt, dealTrace := applyRulesTraced(evDealDamage, base, attackerCards, ctx)

	// Species, then class, then gear: chance conditions consume the turn rng
	// in card order, so this concat order is contractual for determinism.
	victimCards := slices.Concat(
		speciesCards(victim.species), classCards(victim.class), victimGearCards(victim), skillCards(victim),
	)

	taken, takeTrace := applyRulesTraced(evTakeDamage, dealt, victimCards, ctx)

	// #114: record the hit for the turn bundle's Hits view. Crit is the
	// ATTACKER-side moment (a chance-conditioned boost fired in the
	// deal-damage fold: elf passive, Misericorde, Duelist's Saber); glance
	// the DEFENDER-side one (a chance-conditioned reduction in the
	// take-damage fold: the Rogue passive). The other two trace combinations
	// (an attacker-side chance reduction, a defender-side chance boost) have
	// no content and no vocabulary yet — deliberately not surfaced. Purely
	// observational: appending here consumes no rng and changes no damage.
	w.recentHits = append(w.recentHits, hitRecord{
		turn: w.turn + 1, attacker: attacker.id, victim: victim.id, amount: taken,
		crit: dealTrace.boostFired, glance: takeTrace.reduceFired,
	})

	return taken
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
	return slices.Concat(speciesCards(e.species), equippedRuleCards(e), skillCards(e))
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

// collectMeleeAttacksLocked scans the PRE-MOVE board for this turn's melee
// attacks (#104, attacks-before-moves): a mover whose next step is an opposing-held
// hex commits its turn to an attack on that hex and will not move (path
// retained). Consumes no rng — detection reads the static pre-move board in
// members' id-sorted order, so the returned attack order is deterministic
// without a draw. Returns the attacks and the committed melee attackers' ids for
// movePhaseLocked to skip. The old retreat-dodge (a deferred melee attack
// re-checked post-move, completing as a move when the defender vacated —
// fizzle reason melee_target_vacated) is removed by design: a committed
// attack always lands.
//
// #116: move-conversion is a MONSTER rule — the AI attacks by pathing onto
// players. A PLAYER mover whose next step is hostile-held no longer converts;
// movePhaseLocked's hasOpposing check blocks it (waits, path retained), and
// player melee arrives as an entity-targeted attack intent instead
// (resolveEntityTargetedLocked). Callers hold w.mu.
func (w *World) collectMeleeAttacksLocked(
	byHex map[protocol.Hex][]*entity, members []*entity,
) ([]pendingAttack, map[int64]bool) {
	var attacks []pendingAttack

	attacked := make(map[int64]bool)

	for _, m := range members {
		if m.kind != protocol.EntityMonster || len(m.path) == 0 {
			continue
		}

		if hasOpposing(byHex[m.path[0]], m) {
			attacks = append(attacks, pendingAttack{m, m.path[0]})
			attacked[m.id] = true
		}
	}

	return attacks, attacked
}

// movePhaseLocked resolves the move phase, AFTER attacks (#104): movers
// advance one hex from their path in seeded-shuffled order — skipping
// entities that committed a melee attack this turn (attacked; a melee attack is the turn's
// whole action) and entities killed in the attack phase (hp <= 0 — the dead
// never move; deaths are removed later by resolveDeathsLocked).
//
// A blocked next step — opposing-held on the evolving board (including a
// hostile that arrived this same phase), or same-faction at StackCap; see
// blockedFor — splits by faction:
//
//   - a MONSTER waits, path retained. That wait is load-bearing: next turn its
//     standing intent becomes a melee attack (collectMeleeAttacksLocked).
//     Monsters also re-path from a retained goal every turn anyway
//     (thinkMonstersLocked), so a stale route is not a thing they can have.
//   - a PLAYER re-routes around the blocker (#96, repathBlockedLocked) and
//     still advances this turn. Player melee is an entity-targeted attack
//     intent, never a move (#116), so a waiting player was pure dead time —
//     and unattended auto-walk is the point of click-to-move. It falls back to
//     waiting, path retained, when no acceptable detour exists.
//
// Callers hold w.mu.
func (w *World) movePhaseLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity, members []*entity, attacked map[int64]bool,
) {
	movers := make([]*entity, 0, len(members))

	for _, e := range members {
		if len(e.path) > 0 && !attacked[e.id] && e.hp > 0 {
			movers = append(movers, e)
		}
	}

	slices.SortFunc(movers, func(a, b *entity) int { return int(a.id - b.id) })
	rng.Shuffle(len(movers), func(i, j int) { movers[i], movers[j] = movers[j], movers[i] })

	for _, m := range movers {
		if blockedFor(m, byHex, m.path[0]) && !w.repathBlockedLocked(m, byHex) {
			continue // blocked, no acceptable detour → wait, path retained
		}

		next := m.path[0]
		from := m.hex
		byHex[m.hex] = removeEntity(byHex[m.hex], m)
		byHex[next] = append(byHex[next], m)
		m.hex = next
		m.path = m.path[1:]
		w.logger.Info(combatLogMsg, "event", combatEventMove, "id", m.id, "kind", m.kind, "from", from, "to", next)
	}
}

// repathBlockedLocked re-routes a PLAYER's queued walk around whatever is
// standing in its way (#96) and reports whether it adopted a new route; false
// means the caller waits, path retained, exactly as every mover did before #96.
// Monsters always get false — see movePhaseLocked for why their wait is
// deliberate.
//
// The goal is the route's own end: the walk's intended stopping point, already
// trimmed by queueMoveLocked's #116 rule (a walk onto a hostile-held hex stops
// adjacent), so re-routing toward it preserves that rule for free — no separate
// destination has to be stored, which is also why a queued walk stays a
// transient (it never reaches the snapshot).
//
// The goal is EXEMPT from the occupancy half of the predicate, because Pathfind
// returns nil whenever !walkable(to) — an occupied goal would otherwise refuse
// every detour outright, including the long approach that gives the goal time to
// clear. The exemption is precisely why the adopted route's FIRST step is
// re-checked: for a blocked goal one hex away the exempt route is [goal], and
// stepping it would walk through the very StackCap/hostile rules the block
// exists to enforce.
//
// Callers hold w.mu.
func (w *World) repathBlockedLocked(m *entity, byHex map[protocol.Hex][]*entity) bool {
	if m.kind != protocol.EntityPlayer {
		return false
	}

	goal := m.path[len(m.path)-1]

	route := Pathfind(m.hex, goal, func(h protocol.Hex) bool {
		return w.walkableLocked(h) && (h == goal || !blockedFor(m, byHex, h))
	})

	// Blockers are transient — the monster in the way has usually moved on by
	// next turn — so a detour that costs more than the slack loses to simply
	// standing still (protocol.RepathDetourSlack). An unreachable goal
	// (nil/empty route) waits for the same reason.
	if len(route) == 0 || len(route) > len(m.path)+protocol.RepathDetourSlack {
		return false
	}

	if blockedFor(m, byHex, route[0]) {
		return false
	}

	m.path = route

	return true
}

// attackLocked resolves the attack phase: each melee attack and each pending
// ranged attack accumulates damage against pre-attack HP (nothing applied yet)
// into one shared map, so order is irrelevant and mutual kills work, then
// applies it all at once. A stacked defending hex picks its victim with rng, so
// a melee attack against a stack damages exactly one occupant. Ranged attacks resolve in
// the same map (resolveRangedLocked) so a bow shot and a melee attack land
// simultaneously. Callers hold w.mu.
func (w *World) attackLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity, attacks []pendingAttack) {
	damage := make(map[int64]int)

	for _, a := range attacks {
		victims := opposingOccupants(byHex[a.target], a.attacker)
		if len(victims) == 0 {
			// Guard; collectMeleeAttacksLocked ensured at least one on the pre-move
			// board — a same-phase state change here would be a bug.
			continue
		}

		// Canonical order first, like the movers shuffle above: byHex was
		// populated by ranging w.entities (a map), whose iteration order is
		// unspecified and varies per range — without this sort, victim choice
		// would depend on that incidental order instead of the seed alone.
		slices.SortFunc(victims, func(a, b *entity) int { return int(a.id - b.id) })

		victim := victims[rng.IntN(len(victims))]

		// Melee damage: EVERY melee-tagged weapon the attacker holds
		// lands its own hit on the same victim (task 2, dual-wield) — the
		// fists/claws fallback (meleeDefsFor) for an unarmed player or a
		// monster's KIND claws profile (6c — monsterDef.claws, e.g. a rat's 1
		// vs a dragon's 9) is a single-entry slice, so a single-weapon
		// attacker still lands exactly one hit. hand order (heldWeapons) keeps
		// rng consumption deterministic.
		for _, weapon := range meleeDefsFor(a.attacker) {
			base := itemDamage(weapon)
			dealt := w.rollDamageLocked(rng, a.attacker, victim, weapon, base)
			damage[victim.id] += dealt

			w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", a.attacker.id, "victim", victim.id,
				"weapon", weapon.id, "base", base, "dealt", dealt)
		}
	}

	w.resolveRangedLocked(rng, byHex, damage)

	for id, dmg := range damage {
		w.entities[id].hp -= dmg
	}
}

// resolveRangedLocked folds every pending queued attack intent into the
// shared damage map (against pre-attack HP, so a shot lands simultaneously
// with monster-conversion melee attacks). Shooters are processed in id order
// so the seeded single-target victim pick is reproducible regardless of map
// iteration order. An ENTITY-targeted attack (item 7, playtest batch 2 —
// attackTargetEntity != 0, aimed at a specific victim rather than a hex)
// delegates to resolveEntityTargetedLocked BEFORE the unequipped-fizzle
// check below: at distance 1 that function resolves an EXCLUSIVE melee swing
// (#116) using meleeDefsFor's fists/claws fallback, so a melee-only attacker
// (no ranged/magic weapon held at all — rangedDefFor(e) == nil) must not be
// fizzled here as "unequipped" before its swing ever gets a chance to
// resolve. The unequipped-fizzle check therefore only applies to
// GROUND-targeted (hex) intents now — there is no hex-targeted melee, so a
// weaponless ground shot is still a legitimate fizzle. A ground-targeted
// intent is checked against pre-move positions (#104 — attacks resolve
// before moves, so entity/hex positions here are as-submitted) in byHex: a
// shot that is out of every held weapon's range fizzles. Every ranged/magic
// weapon that still reaches the target (rangedDefsFor, task 2 dual-wield)
// fires as its own hit, in hand order: a bow (aoeRadius 0) damages one
// opposing occupant at the target hex — a stack picks one hostile with rng,
// mirroring the melee victim pick (this is the legacy/defensive hex-only bow
// path — kept for the SetAttackTargetForTest bridge and any future hex-only
// ranged use; a real client always sends an entity id for a single-target
// weapon) — while magic (aoeRadius > 0) damages every opposing-faction entity
// within its own aoeRadius of the target hex — no friendly fire,
// ground-targeted by nature. A single-weapon shooter still fires exactly one
// hit, so this is unchanged for every pre-dual-wield board. Every shooter's
// pending target is cleared, hit or fizzle. byHex holds exactly the resolving
// member set, so targets outside the domain are naturally unreachable.
// Callers hold w.mu.
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

		// #116: entity-targeted intents delegate FIRST — resolveEntityTargetedLocked
		// resolves an adjacent victim as a melee swing (fists/claws fallback, no
		// ranged weapon required), so a melee-only attacker must not hit the
		// unequipped-fizzle check below before its swing gets a chance.
		if targetEntityID != 0 {
			w.resolveEntityTargetedLocked(rng, byHex, e, targetEntityID, damage)

			continue
		}

		// Ground-targeted (hex) intents have no melee equivalent, so the
		// unequipped fizzle still applies here.
		if rangedDefFor(e) == nil {
			// unequipped mid-turn (equip intent, Task 4) → fizzle
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "unequipped", "attacker", e.id)

			continue
		}

		w.resolveGroundTargetedLocked(rng, byHex, e, *hexTarget, damage)
	}
}

// resolveGroundTargetedLocked resolves one ground-targeted ranged attack (the
// legacy/defensive hex-only path — kept for the SetAttackTargetForTest bridge
// and any future hex-only ranged use; a real client always sends an entity id
// for a single-target weapon), checked against pre-move positions in byHex
// (#104 — attacks resolve before moves, so entity/hex positions here are
// as-submitted): fizzles (reason out_of_range) if the target hex is out of
// every held weapon's range. Every ranged/magic weapon that still reaches
// (rangedDefsFor, task 2 dual-wield) fires as its own hit, in hand order.
// Single-target (aoeRadius 0) defs share ONE stack-victim pick, drawn lazily
// on the first such def, mirroring attackLocked's melee victim pick — a
// dual-wielded pair of single-target weapons both land on the SAME stack
// member instead of splitting a stack across weapons via independent rng
// picks. Magic (aoeRadius > 0) defs are unaffected — each already damages
// every opposing-faction entity within its own aoeRadius of the target hex,
// no friendly fire, not a single pick. A single-weapon shooter still fires
// exactly one hit, so this is unchanged for every pre-dual-wield board.
// Callers hold w.mu.
func (w *World) resolveGroundTargetedLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity, e *entity, target protocol.Hex, damage map[int64]int,
) {
	defs := rangedDefsFor(e, HexDistance(e.hex, target))
	if len(defs) == 0 {
		// Defensive only (#104): nothing moves between submit validation and
		// attack resolution, so this hex-only path is out of range only via
		// the SetAttackTargetForTest bridge or a mid-turn unequip.
		w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "out_of_range", "attacker", e.id,
			"target_hex", target)

		return
	}

	var (
		victim *entity
		picked bool
	)

	for _, def := range defs {
		dmg := itemDamage(def)

		if def.aoeRadius == 0 {
			if !picked {
				victim = w.stackVictimLocked(rng, byHex, e, target)
				picked = true
			}

			w.resolveBowLocked(rng, e, def, victim, dmg, damage)

			continue
		}

		w.resolveAoELocked(rng, byHex, e, def, target, def.aoeRadius, dmg, damage)
	}
}

// resolveEntityTargetedLocked resolves one entity-targeted attack (item 7,
// playtest batch 2): aims at the victim's pre-move hex (#104 — attacks
// resolve before moves, so entity/hex positions here are as-submitted)
// rather than trusting the hex it happened to occupy at submit time, so a
// sidestepping or retreating target is tracked the way a hex-pinned shot
// never could.
//
// If the victim is ADJACENT (#116, HexDistance == 1), this is a MELEE swing,
// EXCLUSIVELY — the weapon-by-distance identity (a rogue swings the dagger
// adjacent, shoots the bow at range), so ranged/magic defs never also fire
// at distance 1. Every def in meleeDefsFor(attacker) lands its own hit on
// the named victim (dual-wield parity with the monster conversion path in
// attackLocked), each through rollDamageLocked, logged the same way. Melee
// never fizzles for want of a weapon (meleeDefsFor falls back to
// fists/claws) — adjacency alone is reach. Positions are pre-move (#104) and
// nothing moves between submit validation and this phase, so adjacency here
// matches adjacency at submit.
//
// At distance >= 2, resolution is unchanged: every ranged/magic weapon the
// attacker holds that still reaches the victim's pre-move hex
// (rangedDefsFor, task 2 dual-wield) fires as its own hit, in hand order: a
// bow (aoeRadius 0) damages exactly the named victim; a magic weapon
// (aoeRadius > 0) AoEs around the victim's hex (resolveAoELocked — no
// friendly fire, may also catch other hostiles nearby). A single ranged
// weapon still fires exactly one hit at exactly the named victim, so this is
// unchanged for every pre-dual-wield board. Fizzles (reason out_of_range) if
// NO held weapon reaches — same reason the ground-targeted path logs; this
// is now defensive only (#104): nothing moves between submit validation and
// attack resolution, so it is reachable only via the
// SetAttackTargetEntityForTest bridge or a mid-turn unequip (a melee-only
// attacker's distant intent is rejected at submit, queueAttackLocked, and
// never reaches this function). A victim that died or vanished this same
// turn — a simultaneous kill by another attacker, resolved earlier in this
// same damage-accumulation pass — also fizzles (reason target_gone) rather
// than panicking on a missing entity; damage application happens all-at-once
// after every attack accumulates (attackLocked), so "vanished" here really
// means removed by a PRIOR resolution this turn (deaths, not this pass —
// resolveDeathsLocked runs after this), i.e. any entity that already left
// w.entities entirely, which resolveCombatLocked never does mid-attack-phase;
// this guard is therefore mostly defensive, matching resolveBowLocked's own
// empty-hex no-op. Callers hold w.mu.
func (w *World) resolveEntityTargetedLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity, attacker *entity, targetEntityID int64, damage map[int64]int,
) {
	victim, ok := w.entities[targetEntityID]
	if !ok || victim.hp <= 0 {
		w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "target_gone", "attacker", attacker.id)

		return
	}

	// #116: an adjacent victim means this intent is a MELEE swing,
	// exclusively — ranged/magic defs never also fire at distance 1 (the
	// weapon-by-distance identity). Every held melee weapon lands its own hit
	// (dual-wield parity with attackLocked's monster-conversion loop), each
	// through the full pipeline.
	if HexDistance(attacker.hex, victim.hex) == 1 {
		for _, weapon := range meleeDefsFor(attacker) {
			base := itemDamage(weapon)
			dealt := w.rollDamageLocked(rng, attacker, victim, weapon, base)
			damage[victim.id] += dealt

			w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", attacker.id, "victim", victim.id,
				"weapon", weapon.id, "base", base, "dealt", dealt)
		}

		return
	}

	defs := rangedDefsFor(attacker, HexDistance(attacker.hex, victim.hex))
	if len(defs) == 0 {
		w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "out_of_range",
			"attacker", attacker.id, "victim", victim.id)

		return
	}

	for _, weapon := range defs {
		dmg := itemDamage(weapon)

		if weapon.aoeRadius == 0 {
			dealt := w.rollDamageLocked(rng, attacker, victim, weapon, dmg)
			damage[victim.id] += dealt

			w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", attacker.id, "victim", victim.id,
				"weapon", weapon.id, "base", dmg, "dealt", dealt)

			continue
		}

		w.resolveAoELocked(rng, byHex, attacker, weapon, victim.hex, weapon.aoeRadius, dmg, damage)
	}
}

// stackVictimLocked picks the single-target ranged victim at target hex: the
// sole opposing-faction occupant, or one seeded-random hostile if the hex
// holds a stack — nil for an empty or friendly-only hex. Shared by
// resolveGroundTargetedLocked's per-def loop, drawn ONCE per shooter even
// when several single-target (aoeRadius 0) held weapons fire this same
// target, so a dual-wielded pair concentrates on one victim (mirroring
// attackLocked's melee victim pick) instead of splitting a stack across
// weapons via independent picks. Callers hold w.mu.
func (w *World) stackVictimLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity, attacker *entity, target protocol.Hex,
) *entity {
	victims := opposingOccupants(byHex[target], attacker)
	if len(victims) == 0 {
		return nil
	}

	slices.SortFunc(victims, func(a, b *entity) int { return int(a.id - b.id) })

	return victims[rng.IntN(len(victims))]
}

// resolveBowLocked accumulates single-target ranged damage from weapon
// against victim (already picked — stackVictimLocked); a nil victim (empty or
// friendly-only target hex) is a no-op. Callers hold w.mu.
func (w *World) resolveBowLocked(
	rng *mrand.Rand, attacker *entity, weapon *itemDef, victim *entity, dmg int, damage map[int64]int,
) {
	if victim == nil {
		return
	}

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

// levelFor returns the 1-based level for a cumulative XP total: the largest
// L with XPCurveBase*(L-1)^2 <= xp. Integer math only — float sqrt
// mis-rounds near perfect squares.
func levelFor(xp int) int { return 1 + isqrt(xp/protocol.XPCurveBase) }

// xpFloorFor returns the cumulative XP at which the given level starts.
func xpFloorFor(level int) int {
	return protocol.XPCurveBase * (level - 1) * (level - 1)
}

// isqrt returns the integer square root: the largest s with s*s <= n.
func isqrt(n int) int {
	if n <= 0 {
		return 0
	}

	s := int(math.Sqrt(float64(n)))
	for s > 0 && s*s > n {
		s--
	}

	for (s+1)*(s+1) <= n {
		s++
	}

	return s
}

// grantSkillPointsLocked pays a player for every level crossed since the last
// time it was paid, and banks the new high-water mark (#124). Idempotent by
// construction: it pays for levels ABOVE pointsGrantedLevel and never below,
// so calling it twice on the same XP is a no-op, and re-earning XP lost to
// death (levelFloorXP) grants nothing a second time.
//
// This is a species check rather than a rule card on purpose: a per-level
// BANK grant is not a fold over a combat value, and inventing an evLevelUp
// event for a single rider would trip the no-mechanic-wildfire gate.
// Callers hold w.mu.
func grantSkillPointsLocked(e *entity) {
	if e.kind != protocol.EntityPlayer {
		return
	}

	level := levelFor(e.xp)
	if level <= e.pointsGrantedLevel {
		return
	}

	per := protocol.SkillPointsPerLevel
	if e.species == protocol.SpeciesHuman {
		per += protocol.HumanBonusSkillPoints
	}

	e.skillPoints += (level - e.pointsGrantedLevel) * per
	e.pointsGrantedLevel = level
}

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

// levelFloorXP returns the XP at the start of xp's current level (the
// death floor: dying costs progress inside the level, never the level).
func levelFloorXP(xp int) int { return xpFloorFor(levelFor(xp)) }

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

	// A monster loot drop is a single item (count 1) — even a potion, which
	// stacks only once it is in a backpack.
	w.groundItems[at] = append(w.groundItems[at], groundStack{inst: itemInstance{id: w.nextID, defID: defID}, count: 1})
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
// attacks (thinkMonstersLocked's co-location dormancy), so landing a spawn
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
		w.entities[w.nextID] = newMonsterEntity(w.nextID, h, k)
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
	w.entities[w.nextID] = newMonsterEntity(w.nextID, h, k)

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
// The leash (#102) also only binds WORLD-domain monsters: one that has
// strayed beyond leashRadiusFor of its home hex stops chasing and paths back
// home, ignoring players until it arrives (thinkReturnHomeLocked) — checked
// before the no-targets return below so a returning monster keeps walking
// home even in a playerless world.
//
// When adjacent, path[0] is the player's own hex, so the move phase converts
// this into a melee attack (6.3).
func (w *World) thinkMonstersLocked(rng *mrand.Rand, members, targets []*entity, worldDomain bool) {
	for _, m := range members {
		if m.kind != protocol.EntityMonster {
			continue
		}

		if worldDomain && w.thinkReturnHomeLocked(m) {
			continue // beyond leash or already returning: this turn is a step home
		}

		if len(targets) == 0 {
			continue // no players anywhere: paths stay untouched (pre-#102 behavior)
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
		// hex, so the move phase converts this into a melee attack (6.3).
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
		// Folded ONCE per player: aggroRadiusForLocked consumes rng if a card
		// ever carries a chance condition, so calling it twice per player
		// would silently double-consume the turn stream.
		reach := aggroRadiusForLocked(rng, base, p)

		d := HexDistance(m.hex, p.hex)
		if d > reach {
			continue
		}

		// Sight and noticeability are INDEPENDENT gates (#95 Q2, #88): the
		// fold above decides how far this monster could notice this player,
		// and the raycast decides whether terrain lets it — over that same
		// reach, not CombatRadius, since a kind can notice far past bubble
		// range. A booted player behind a rock is hidden twice over, and the
		// two are never folded into one number. A monster no longer charges
		// through a rock wall and snaps into a bubble as it rounds the corner.
		if w.sightBlockedLocked(m.hex, p.hex, reach) {
			continue
		}

		if best == nil || d < bestDist || (d == bestDist && p.id < best.id) {
			best, bestDist = p, d
		}
	}

	return best
}

// baseAggroRadiusFor returns monster m's own base aggro radius before any
// player-side noticeability fold: its kind's effective aggro radius
// (defAggroRadius — the kind's override, else protocol.MonsterAggroRadius).
// m is assumed to be a monster (the only caller,
// nearestAggroedPlayerLocked, only ever calls this for one); kindOf(m) nil
// (a malformed fixture) falls back to the default too.
func baseAggroRadiusFor(m *entity) int {
	return defAggroRadius(kindOf(m))
}

// leashRadiusFor returns monster m's leash radius (#102): its kind's
// effective leash radius (defLeashRadius — the kind's own leashRadius
// override, else protocol.MonsterLeashMultiplier × its base aggro radius).
// The leash is a monster↔home relation with no player in the equation, so
// the per-player evAggroRange noticeability fold (aggroRadiusForLocked)
// deliberately does not apply here.
func leashRadiusFor(m *entity) int {
	return defLeashRadius(kindOf(m))
}

// thinkReturnHomeLocked is the WORLD-domain leash check (#102), run for
// monster m before any aggro targeting. It reports whether m's think for
// this turn was fully handled as a leash return — in which case the caller
// (thinkMonstersLocked) must not run the normal chase logic for m.
//
//   - Not returning and within leashRadiusFor of homeHex: no-op, false.
//   - Beyond the leash: flip returningHome (logged as a "leash" combat
//     event) and fall into the returning case below.
//   - Returning and arrived (arrivedHomeLocked): clear the flag and return
//     false — this same think pass runs the normal aggro check, so a player
//     camping just outside the home hex is noticed immediately, not one turn
//     late.
//   - Returning, not arrived: path one step toward homeHex, ignoring players
//     entirely (no re-aggro mid-return, even once back within leash range).
//
// A returning monster can still be pulled into a combat bubble by walking
// within CombatRadius of a player — bubble membership is positional
// (recomputeBubblesLocked) and overrides world-domain thinking entirely; the
// flag survives the fight so the return resumes if the bubble dissolves.
// Consumes no rng. Callers hold w.mu.
func (w *World) thinkReturnHomeLocked(m *entity) bool {
	if !m.returningHome {
		if HexDistance(m.hex, m.homeHex) <= leashRadiusFor(m) {
			return false
		}

		m.returningHome = true
		w.logger.Info(combatLogMsg, "event", combatEventLeash, "id", m.id,
			"monster_kind", m.monsterKind, "from", m.hex, "home", m.homeHex)
	}

	if w.arrivedHomeLocked(m) {
		m.returningHome = false

		return false
	}

	path := Pathfind(m.hex, m.homeHex, w.walkableLocked)
	if len(path) >= 1 {
		m.path = []protocol.Hex{path[0]}
	} else {
		m.path = nil // home unreachable (cannot happen on static terrain): stand still
	}

	return true
}

// arrivedHomeLocked reports whether returning monster m counts as home
// (#102): standing on its home hex, or adjacent to it while the home hex has
// no room this turn (StackCap — e.g. a monster pile-up on the spawn hex).
// Without the adjacent-and-full case, a returning monster whose home stays
// full would wait one hex away with its flag stuck, passive forever. An
// opposing-held home needs no case of its own: a player on or near the home
// hex would have pulled the monster into a combat bubble (CombatRadius,
// recomputeBubblesLocked) before this world-domain check could run. Callers
// hold w.mu.
func (w *World) arrivedHomeLocked(m *entity) bool {
	if m.hex == m.homeHex {
		return true
	}

	return HexDistance(m.hex, m.homeHex) == 1 && w.occupancyLocked(m.homeHex) >= protocol.StackCap
}

// aggroRadiusForLocked returns the hex radius at which a WORLD-domain
// monster with base aggro radius `base` (baseAggroRadiusFor — per-kind since
// 6c) notices player p: base folded through p's own noticeability rule
// cards (species + every equipped item's rules, in canonicalSlotOrder —
// mirroring rollDamageLocked's victimCards fold: any gear on the entity can
// contribute, not just the "acting" slot) via the evAggroRange event. Live
// content since #88: Padded Boots (×0.75) and Iron Plate Armor (×1.25) shrink
// or grow the radius without touching this call site. Noticeability is
// gear-only by design — no species card feeds this event. Callers hold w.mu.
func aggroRadiusForLocked(rng *mrand.Rand, base int, p *entity) int {
	cards := slices.Concat(speciesCards(p.species), equippedRuleCards(p), skillCards(p))

	return applyRules(evAggroRange, base, cards, ruleCtx{attacker: p, rng: rng})
}

// entityViewsLocked renders every entity for the wire, EXCEPT the own-only
// fields (fillOwnOnlyLocked adds those for the viewer). Unsorted — the caller
// sorts by id. Callers hold w.mu.
func (w *World) entityViewsLocked() []protocol.Entity {
	entities := make([]protocol.Entity, 0, len(w.entities))

	for _, e := range w.entities {
		entities = append(entities, protocol.Entity{
			ID: e.id, Hex: e.hex, Kind: e.kind, Name: entityNameLocked(e), Class: e.class, Species: e.species,
			HP: e.hp, MaxHP: e.maxHP, InCombat: e.bubbleID != 0, XP: e.xp, Level: levelFor(e.xp), PartyID: e.partyID,
			Items: itemViewsLocked(e), MonsterKind: e.monsterKind,
		})
	}

	return entities
}

// fillOwnOnlyLocked stamps the viewer's OWN skills and point bank onto their
// row and nobody else's (#124 task 7, Q9). Split out of SnapshotFor to keep
// that function under the length limit; the viewer is resolved once per
// bundle rather than once per entity. Callers hold w.mu.
func (w *World) fillOwnOnlyLocked(entities []protocol.Entity, viewerToken string) {
	if viewerToken == "" {
		return
	}

	viewer, ok := w.byToken[viewerToken]
	if !ok || viewer == nil {
		return
	}

	for i := range entities {
		if entities[i].ID == viewer.id {
			entities[i].Skills = skillViewsLocked(viewer)
			entities[i].SkillPoints = viewer.skillPoints

			return
		}
	}
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
// walkable, capacity-available hex anywhere in the sanctuary
// (protocol.SanctuaryRadius of the origin) that is not occupied by, or
// within CombatRadius of, a living monster (tooCloseToMonsterLocked) — so a
// spawn can never land a player ON a monster or form an instant combat
// bubble the moment they appear (both observed live, #36). Random, not the
// old spiral-nearest-to-origin search: players (and respawns) no longer pile
// deterministically onto the same hex. Per Q9, the sanctuary is every join's
// and respawn's shared "home" until beds land as a per-player anchor —
// scattering across the whole sanctuary rather than just the small origin
// clearing is intentional.
//
// Four tiers, each engaged only if the one above yields nothing, so a small
// or crowded map never fails a join outright — but "not literally on top of a
// monster" is relaxed dead last, since that specific case can silently stall
// combat forever (occupiedByMonsterLocked's doc comment), not just risk an
// instant bubble:
//  1. sanctuary hexes clear of monsters entirely (the common case)
//  2. sanctuary hexes not occupied by one, ignoring the CombatRadius
//     preference (a monster-dense sanctuary may leave nothing outside it)
//  3. sanctuary hexes at all, ignoring both monster checks (the sanctuary
//     itself is saturated — every hex in it has a monster standing on it)
//  4. spawnHexSpiralLocked over the WHOLE reachable region, ignoring every
//     guard — the pre-#36 search, kept verbatim as the last resort so "a
//     crowded tiny test map must not break joins" still holds
//
// Callers hold w.mu.
func (w *World) spawnHexLocked() (protocol.Hex, error) {
	origin := protocol.Hex{Q: 0, R: 0}

	var sanctuarySafe, sanctuaryUnoccupied, sanctuaryAny []protocol.Hex

	for h := range w.spawnable {
		if HexDistance(origin, h) > protocol.SanctuaryRadius || w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		sanctuaryAny = append(sanctuaryAny, h)

		if w.occupiedByMonsterLocked(h) {
			continue
		}

		sanctuaryUnoccupied = append(sanctuaryUnoccupied, h)

		if !w.tooCloseToMonsterLocked(h) {
			sanctuarySafe = append(sanctuarySafe, h)
		}
	}

	candidates := sanctuarySafe
	if len(candidates) == 0 {
		candidates = sanctuaryUnoccupied
	}

	if len(candidates) == 0 {
		candidates = sanctuaryAny
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
// tier-4 fallback spawnHexLocked reaches for only when none of the three
// sanctuary tiers above yields a single candidate (an extremely crowded or
// tiny map), so a join never hard-fails just because the sanctuary is
// exhausted.
// Callers hold w.mu.
//
// Faction-blind by design in this fallback path: it can land a player on a
// monster-occupied hex (opposing co-occupancy, a §5 MUST in the rare case it
// is ever reached). It is inert only because a co-located monster's think
// step gets Pathfind(from==to)==∅ and holds (never attacks).
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
