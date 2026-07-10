package game

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

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
	// ErrInvalidIntentKind rejects a SubmitIntent whose Kind is not
	// IntentMove or IntentAttack.
	ErrInvalidIntentKind = errors.New("invalid intent kind")
)

// tokenBytes sizes the bearer token: 16 random bytes = 128 bits.
const tokenBytes = 16

// entity is the server-side entity record. The wire shape is
// protocol.Entity; the token never leaves this package except via Join.
type entity struct {
	id    int64
	hex   protocol.Hex
	token string
	kind  string
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
	// attackTarget is a pending ranged-attack target hex for this turn, or nil
	// for none. Set by an "attack" intent (which clears path — you shoot, you
	// don't move), resolved and cleared in the attack phase.
	attackTarget *protocol.Hex
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
	// items is every item instance this entity owns (players only; monsters own
	// none — they fight with the flat monsterClawsDef profile). Granted at Join
	// by grantDefaultsLocked; gear survives death (never cleared by respawn).
	items []itemInstance
	// closeSlot and rangedSlot are the instance ids currently equipped in each
	// slot, or 0 for empty. 0 is never a valid instance id (nextID starts
	// counting from 1). See equippedDef/closeDefFor/rangedDefFor (items.go).
	closeSlot, rangedSlot int64
	// pendingEquip is the item instance id queued by an equip intent submitted
	// inside a combat bubble — the swap becomes this turn's action (see
	// queueEquipLocked) instead of applying immediately. 0 means no equip is
	// queued. Applied and cleared by resolveCombatLocked's pending-equip pass;
	// also cleared on death (resolveDeathsLocked) as a safety net for
	// resolution paths that skip that pass (e.g. the ResolveCombatOnlyForTest
	// test bridge).
	pendingEquip int64
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

	return &World{
		interval:        interval,
		ticks:           ticks,
		combatPatience:  combatPatience,
		bubblePoll:      bubblePoll,
		disconnectGrace: disconnectGrace,
		now:             time.Now,
		terrain:         terrain,
		worldMap:        worldMap,
		radius:          radius,
		spawnable:       reachableWalkable(worldMap),
		entities:        make(map[int64]*entity),
		byToken:         make(map[string]*entity),
		pendingInvites:  make(map[int64]int64),
		bubbles:         make(map[int64]*bubble),
		seed:            seed,
		quests:          generateQuests(worldSeed, worldMap),
		announce:        func(string, string) {},
	}
}

// Map returns the immutable world map.
func (w *World) Map() protocol.MapResponse {
	return w.worldMap
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

// Join returns the entity for token, creating a new one (empty or unknown
// token) at a free spawn hex with the given name, class, and species. For a
// new entity, name is required — non-empty after trimming and at most
// protocol.MaxNameLen runes, else Join returns ErrInvalidName; class is
// required — it must be ClassFighter, ClassRogue, or ClassMage, else Join
// returns ErrInvalidClass, and species must be SpeciesHuman, SpeciesElf, or
// SpeciesDwarf, else ErrInvalidSpecies. An unknown token quietly becomes a new
// player rather than an error: the stored identity of a restarted server is
// gone, and the client's right move is always "then give me a fresh entity".
// For a reclaim (known token) name, class, and species are ignored — the
// existing entity already has all three.
func (w *World) Join(token, name, class, species string) (protocol.JoinResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok && token != "" {
		// A reclaim is a fresh sign of life: refresh the grace clock so a sweep
		// can't remove the entity in the gap before its reopened stream calls
		// StreamOpened.
		e.disconnectedAt = w.now()

		return protocol.JoinResponse{EntityID: e.id, Token: e.token, Hex: e.hex}, nil
	}

	name = strings.TrimSpace(name)
	if !validName(name) {
		return protocol.JoinResponse{}, ErrInvalidName
	}

	if !validClass(class) {
		return protocol.JoinResponse{}, ErrInvalidClass
	}

	if !validSpecies(species) {
		return protocol.JoinResponse{}, ErrInvalidSpecies
	}

	spawn, err := w.spawnHexLocked()
	if err != nil {
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
// deterministic. If it removed anyone it recomputes bubbles — a swept entity may
// have been mid-fight — and returns true, so the caller republishes and clients
// despawn the entity. Callers hold w.mu.
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

	switch req.Kind {
	case protocol.IntentMove:
		if err := w.queueMoveLocked(e, req.Target); err != nil {
			return err
		}
	case protocol.IntentAttack:
		if err := w.queueAttackLocked(e, req.Target); err != nil {
			return err
		}
	case protocol.IntentEquip:
		if err := w.queueEquipLocked(e, req.ItemID); err != nil {
			return err
		}
	default:
		return ErrInvalidIntentKind
	}

	// Lock-in: inside a combat bubble, submitting an intent commits this player
	// for the bubble's action-gated turn. Once every player member has locked
	// in, the bubble resolves immediately (rather than waiting for the poll or
	// the patience timeout) and the tick hub is notified.
	if e.bubbleID != 0 {
		if b, ok := w.bubbles[e.bubbleID]; ok {
			b.ready[e.id] = struct{}{}

			if w.allPlayersReadyLocked(b) {
				now := w.now()
				w.resolveBubbleTurnLocked(b, w.bubbleMembersLocked(b), now)
				w.recomputeBubblesLocked(now)
				w.ticks.Publish()
			}
		}
	}

	return nil
}

// queueMoveLocked validates a move intent and sets the entity's route to a
// walkable, reachable target, clearing any pending ranged attack (the latest
// intent in the window wins). Callers hold w.mu.
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

	return nil
}

// queueAttackLocked validates a ranged attack intent and queues it: the entity
// must have a ranged weapon equipped (else ErrNoRangedWeapon) and the target must
// be within its reach at submit time (else ErrOutOfRange). On success it records
// the target and clears the route — a ranged attack replaces the move for this
// turn. The resolution-time re-check is defensive (nothing relocates a shooter
// mid-turn today; it guards a future knockback/forced-move mechanic).
//
// INVARIANT: max over every registered def's rangeHex+aoeRadius must stay <=
// CombatRadius (validateMaxReach, run at content load by mustValidateContent),
// so any entity a ranged attack can reach is always already in the shooter's
// combat bubble. If that invariant were ever violated, a monster could be
// ranged-killed in the WORLD domain (where resolveWorldTurnLocked awards no
// kill-XP) — add an in-bubble/target-in-member-set guard here then. Callers
// hold w.mu.
func (w *World) queueAttackLocked(e *entity, target protocol.Hex) error {
	def := rangedDefFor(e)
	if def == nil {
		return ErrNoRangedWeapon
	}

	if HexDistance(e.hex, target) > def.rangeHex {
		return ErrOutOfRange
	}

	t := target
	e.attackTarget = &t
	e.path = nil

	return nil
}

// queueEquipLocked validates and applies/queues an equip. Outside a bubble the
// swap is immediate and free; inside, it becomes the player's action this turn
// (clearing move/attack — you swap, you don't also act). Callers hold w.mu.
func (w *World) queueEquipLocked(e *entity, itemID int64) error {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	def := itemDefByID[inst.defID]
	if def.class != e.class {
		return ErrWrongClass
	}

	if e.bubbleID == 0 {
		e.applyEquip(inst.id, def.slot)

		return nil
	}

	e.pendingEquip = itemID
	e.path = nil
	e.attackTarget = nil

	return nil
}

// applyEquip swaps instID into slot (protocol.ItemSlotClose or
// ItemSlotRanged), replacing whatever instance was equipped there. Callers
// must have already validated ownership and class (queueEquipLocked, or the
// pending-equip pass in resolveCombatLocked).
func (e *entity) applyEquip(instID int64, slot string) {
	switch slot {
	case protocol.ItemSlotClose:
		e.closeSlot = instID
	case protocol.ItemSlotRanged:
		e.rangedSlot = instID
	}
}

// Snapshot is the current turn bundle: turn number plus every entity,
// sorted by ID for a deterministic wire shape.
func (w *World) Snapshot() protocol.TurnEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	entities := make([]protocol.Entity, 0, len(w.entities))
	for _, e := range w.entities {
		entities = append(entities, protocol.Entity{
			ID: e.id, Hex: e.hex, Kind: e.kind, Name: e.name, Class: e.class, Species: e.species, HP: e.hp, MaxHP: e.maxHP,
			InCombat: e.bubbleID != 0, XP: e.xp, Level: levelFor(e.xp), PartyID: e.partyID,
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

	return protocol.TurnEvent{
		Turn: w.turn, IntervalMs: w.interval.Milliseconds(), Entities: entities, Bubbles: bubbles,
		Quests: questViews,
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
// The monsters-killed count from resolveCombatLocked is deliberately dropped:
// kill XP is scoped to a real fight (a combat bubble), so a monster that dies in
// the world domain — only possible via an anomalous faction-blind spawn/join
// landing a player next to an un-bubbled monster — credits no XP to anyone.
func (w *World) resolveWorldTurnLocked(members []*entity) {
	w.resolveCombatLocked(members, w.allPlayersLocked())
	w.checkReachQuestsLocked()
	w.turn++
}

// resolveBubbleTurnLocked advances one combat bubble a single action-gated turn:
// the phased combat pipeline over the given member set, then the shared kill-XP
// award, then it clears the bubble's lock-ins and restarts its patience deadline
// for the next turn. Like resolveWorldTurnLocked it does NOT recompute — see that
// method. Callers hold w.mu.
func (w *World) resolveBubbleTurnLocked(b *bubble, members []*entity, now time.Time) {
	killed := w.resolveCombatLocked(members, playersOf(members))

	// Kill XP belongs to the fight: every player who survived this bubble-turn
	// earns the FULL MonsterXP for each monster that fell — no last-hit
	// competition, helping always pays, and the award is not divided. A player who
	// died this same turn is not surviving (hp<=0), so earns nothing.
	if killed > 0 {
		for _, e := range members {
			if e.kind == protocol.EntityPlayer && e.hp > 0 {
				award := applyRules(evEarnXP, killed*protocol.MonsterXP, speciesCards(e.species), ruleCtx{})
				e.xp += award
				syncMaxHPLocked(e)
			}
		}

		w.tickKillQuestsLocked(members, killed)
	}

	w.checkReachQuestsLocked()

	clear(b.ready)
	b.deadline = now.Add(w.combatPatience)

	w.turn++
}

// resolveCombatLocked runs the decided phased resolution over a given entity
// set: think → move (faction-aware, with bump deferral) → attack (simultaneous,
// post-move positions) → apply damage & deaths. The set is a whole
// CombatRadius-connected domain (the world domain or one bubble), so no move,
// bump, stack, or attack can reach an entity outside it. It does not recompute
// bubbles or advance the turn — the two resolve callers own that. It returns the
// number of monsters that died this resolution, which the bubble path turns into
// the shared kill-XP award. Callers hold w.mu.
func (w *World) resolveCombatLocked(members, monsterTargets []*entity) int {
	w.thinkMonstersLocked(members, monsterTargets)

	// Pending equips are this turn's action for any member that queued one
	// (queueEquipLocked, inside a bubble): apply the swap before any damage
	// resolves, then clear it — members arrive id-sorted, so this is
	// deterministic like the rest of the phased resolution.
	for _, e := range members {
		if e.pendingEquip == 0 {
			continue
		}

		if inst, ok := e.itemByID(e.pendingEquip); ok {
			e.applyEquip(inst.id, itemDefByID[inst.defID].slot)
		}

		e.pendingEquip = 0
	}

	//nolint:gosec // deterministic per-turn combat RNG, not security-sensitive; reproducibility is required.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), uint64(w.turn)))

	// Evolving board: who is on each hex as moves resolve.
	byHex := make(map[protocol.Hex][]*entity, len(members))
	for _, e := range members {
		byHex[e.hex] = append(byHex[e.hex], e)
	}

	attacks := w.moveAndBumpLocked(rng, byHex, members)
	w.attackLocked(rng, byHex, attacks)

	return w.resolveDeathsLocked(members)
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
// take-damage cards (species + the rules of BOTH the victim's equipped defs,
// close then ranged — a hit lands on the whole entity, not just the slot that
// happens to be attacking). weapon is the attacker's acting weapon def
// (closeDefFor for a bump, rangedDefFor for a shot); it is never nil — every
// combat site resolves a def (fists/claws fallback for close, a real
// equipped item for ranged, since a nil ranged def never reaches here — see
// queueAttackLocked/resolveRangedLocked). Every damage number in the game
// flows through here. Callers hold w.mu.
func (w *World) rollDamageLocked(rng *mrand.Rand, attacker, victim *entity, weapon *itemDef, base int) int {
	ctx := ruleCtx{attacker: attacker, victim: victim, allyInBubble: w.allyInBubbleLocked(attacker), rng: rng}

	attackerCards := slices.Concat(speciesCards(attacker.species), weapon.rules)
	dealt := applyRules(evDealDamage, base, attackerCards, ctx)

	victimCards := slices.Concat(speciesCards(victim.species), closeDefFor(victim).rules)
	if ranged := rangedDefFor(victim); ranged != nil {
		victimCards = slices.Concat(victimCards, ranged.rules)
	}

	return applyRules(evTakeDamage, dealt, victimCards, ctx)
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
// has locked in, or its patience deadline has passed. Callers hold w.mu.
func (w *World) bubbleReadyOrExpiredLocked(b *bubble, now time.Time) bool {
	if !b.deadline.IsZero() && now.After(b.deadline) {
		return true
	}

	return w.allPlayersReadyLocked(b)
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
			byHex[m.hex] = removeEntity(byHex[m.hex], m)
			byHex[next] = append(byHex[next], m)
			m.hex = next
			m.path = m.path[1:]
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
			byHex[b.m.hex] = removeEntity(byHex[b.m.hex], b.m)
			byHex[b.target] = append(byHex[b.target], b.m)
			b.m.hex = b.target
			b.m.path = b.m.path[1:]
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
		// monster's closeDefFor is always monsterClawsDef, reproducing the old
		// flat MonsterAttackDamage (levelFor(0) == 1, so the level-scaling term
		// is 0). Resolved once here (mirroring resolveRangedLocked's def :=
		// rangedDefFor(e) below) so the def is looked up exactly once per hit.
		weapon := closeDefFor(a.attacker)
		damage[victim.id] += w.rollDamageLocked(rng, a.attacker, victim, weapon, itemDamage(weapon, levelFor(a.attacker.xp)))
	}

	w.resolveRangedLocked(rng, byHex, damage)

	for id, dmg := range damage {
		w.entities[id].hp -= dmg
	}
}

// resolveRangedLocked folds every pending ranged attack into the shared damage
// map (against pre-attack HP, so a bow shot lands simultaneously with bumps).
// Shooters are processed in id order so the seeded single-target victim pick is
// reproducible regardless of map iteration order. Range is re-checked against
// post-move positions in byHex: a shot that is now out of range fizzles. A bow
// (aoeRadius 0) damages one opposing occupant at the target hex — a stack picks
// one hostile with rng, mirroring the bump victim pick. Magic (aoeRadius > 0)
// damages every opposing-faction entity within aoeRadius of the target hex — no
// friendly fire. Every shooter's pending target is cleared, hit or fizzle.
// byHex holds exactly the resolving member set, so targets outside the domain
// are naturally unreachable. Callers hold w.mu.
func (w *World) resolveRangedLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity, damage map[int64]int) {
	var shooters []*entity

	for _, occs := range byHex {
		for _, e := range occs {
			if e.attackTarget != nil {
				shooters = append(shooters, e)
			}
		}
	}

	slices.SortFunc(shooters, func(a, b *entity) int { return int(a.id - b.id) })

	for _, e := range shooters {
		target := *e.attackTarget
		e.attackTarget = nil // resolved, whether it hits or fizzles

		def := rangedDefFor(e)
		if def == nil {
			continue // unequipped mid-turn (equip intent, Task 4) → fizzle
		}

		if HexDistance(e.hex, target) > def.rangeHex {
			continue // moved out of range this turn → fizzle
		}

		dmg := itemDamage(def, levelFor(e.xp))

		if def.aoeRadius == 0 {
			w.resolveBowLocked(rng, byHex, e, def, target, dmg, damage)

			continue
		}

		w.resolveAoELocked(rng, byHex, e, def, target, def.aoeRadius, dmg, damage)
	}
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
	damage[victim.id] += w.rollDamageLocked(rng, attacker, victim, weapon, dmg)
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
		damage[o.id] += w.rollDamageLocked(rng, attacker, o, weapon, dmg)
	}
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
// monsters, and respawns dead players (full HP, fresh spawn hex, same id + token
// — the client stays joined) among the given member set. It returns the number of
// monsters that died; the kill-XP award lives in the bubble-resolution path
// (resolveBubbleTurnLocked), so a kill only pays inside a real fight. The
// death-floor here still applies to ANY player death, world or bubble. Callers
// hold w.mu.
func (w *World) resolveDeathsLocked(members []*entity) int {
	var dead []*entity

	monstersKilled := 0

	for _, e := range members {
		if e.hp <= 0 {
			dead = append(dead, e)

			if e.kind == protocol.EntityMonster {
				monstersKilled++
			}
		}
	}

	// Sort by id so simultaneous respawns claim spawn hexes in a deterministic
	// order (the map range above is unordered) — keeps a full turn reproducible.
	slices.SortFunc(dead, func(a, b *entity) int { return int(a.id - b.id) })

	for _, e := range dead {
		if e.kind == protocol.EntityMonster {
			delete(w.entities, e.id)

			continue
		}

		// Player: fall back to the start of the XP level you were in — keep the
		// level, lose the within-level progress — then respawn in place of a
		// re-join.
		e.xp = levelFloorXP(e.xp)

		if spawn, err := w.spawnHexLocked(); err == nil {
			e.hex = spawn
		}

		// Recompute maxHP from the class and post-floor level so a leveled player
		// respawns with its full, level-scaled bar (via the same maxHPFor source).
		e.maxHP = maxHPFor(e.class, levelFor(e.xp))
		e.hp = e.maxHP
		e.path = nil
		e.pendingEquip = 0
	}

	return monstersKilled
}

// spawnStream is a fixed PCG stream for monster placement, distinct from the
// per-turn move-shuffle stream (which uses the turn number).
const spawnStream uint64 = 0x5EED

// SpawnMonsters adds n monster entities at random walkable hexes, chosen with
// the world seed so a given seed is reproducible. Skips hexes already at
// StackCap. Intended for **startup, before any player joins** (server startup
// via MONSTER_COUNT, or tests) — it does not avoid player-occupied hexes, so a
// later caller (continuous spawning, respawn) must add that guard.
func (w *World) SpawnMonsters(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	walkable := make([]protocol.Hex, 0, len(w.worldMap.Tiles))
	for _, t := range w.worldMap.Tiles {
		if w.walkableLocked(t.Hex) {
			walkable = append(walkable, t.Hex)
		}
	}

	slices.SortFunc(walkable, func(a, b protocol.Hex) int {
		if a.Q != b.Q {
			return a.Q - b.Q
		}

		return a.R - b.R
	})

	//nolint:gosec // deterministic seeded placement, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), spawnStream))
	rng.Shuffle(len(walkable), func(i, j int) { walkable[i], walkable[j] = walkable[j], walkable[i] })

	placed := 0

	for _, h := range walkable {
		if placed >= n {
			break
		}

		if w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		w.nextID++
		w.entities[w.nextID] = &entity{
			id: w.nextID, hex: h,
			kind: protocol.EntityMonster, hp: protocol.MonsterMaxHP, maxHP: protocol.MonsterMaxHP,
		}
		placed++
	}
}

// SpawnMonsterAt spawns a single monster at h, returning whether it spawned. It
// refuses a non-walkable hex or one already at StackCap. Unlike SpawnMonsters
// (random, world-seeded placement) it puts a monster at a caller-chosen hex, so
// a caller can seed a known-position monster — e.g. an integration test that
// needs a monster a couple hexes from where players spawn, for a short,
// deterministic chase. It mirrors SpawnMonsters' entity shape (kind monster,
// MonsterMaxHP). Like SpawnMonsters it is a startup primitive meant to run
// before Run: it does not recompute bubbles (Run does that each tick) and does
// not avoid opposing occupants. Holds w.mu.
func (w *World) SpawnMonsterAt(h protocol.Hex) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.walkableLocked(h) || w.occupancyLocked(h) >= protocol.StackCap {
		return false
	}

	w.nextID++
	w.entities[w.nextID] = &entity{
		id: w.nextID, hex: h,
		kind: protocol.EntityMonster, hp: protocol.MonsterMaxHP, maxHP: protocol.MonsterMaxHP,
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
// of a bubbled player (walk-in reinforcement). Callers hold w.mu.
//
// When adjacent, path[0] is the player's own hex, so the move phase converts
// this into a bump-to-attack (6.3).
func (w *World) thinkMonstersLocked(members, targets []*entity) {
	if len(targets) == 0 {
		return
	}

	for _, m := range members {
		if m.kind != protocol.EntityMonster {
			continue
		}

		target := nearestPlayer(m.hex, targets)
		path := Pathfind(m.hex, target.hex, w.walkableLocked)
		// Step toward the nearest player; when adjacent, path[0] is the player's own
		// hex, so the move phase converts this into a bump-to-attack (6.3).
		if len(path) >= 1 {
			m.path = []protocol.Hex{path[0]}
		} else {
			m.path = nil
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

// spawnHexLocked finds the free walkable hex nearest the origin, spiraling
// outward. Callers hold w.mu.
//
// Faction-blind by design (for now): Join and player respawn can land a player
// on a monster-occupied hex (opposing co-occupancy, a §5 MUST). It is inert only
// because a co-located monster's think step gets Pathfind(from==to)==∅ and holds
// (never bumps) — the moment continuous/faction-aware spawning or monster-wander
// logic lands (6b), that dormancy breaks and this must skip opposing-occupied
// hexes. See docs/STATUS.md "known placeholders".
func (w *World) spawnHexLocked() (protocol.Hex, error) {
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
