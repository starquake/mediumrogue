package game

// snapshot.go: the disk persistence format (spec:
// docs/superpowers/specs/2026-07-11-m10a-persistence-identity-design.md §2).
// MarshalState/RestoreState are the only bridge between the in-memory World
// and a snapshot file; app-level load/save wiring lives in cmd/rogue/app.
//
// Disk shape is snapshot-private: every JSON tag below lives on a DTO in
// this file, NEVER on the unexported entity/quest/characterRecord/
// itemInstance structs — the wire (protocol) and disk formats stay fully
// decoupled, so a protocol change and a disk-shape change are independent
// decisions.

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// snapshotVersion gates the on-disk shape: bump it on ANY breaking change to
// a DTO below (new/removed/retyped field). A mismatched version makes
// RestoreState return errSnapshotMismatch — there are no migrations
// pre-launch; the no-backward-compatibility rule applies to disk exactly as
// it does to the wire (see CLAUDE.md / the no-backward-compatibility memory).
//
// v2 (item 4, playtest feedback batch 3): added WorldID — persisted so a
// restored world keeps its predecessor's identity instead of looking like a
// reset to clients.
const snapshotVersion = 2

// errSnapshotMismatch is RestoreState's sentinel for a snapshot that does not
// describe this process's world: a different snapshotVersion, world seed, or
// world radius. The caller's contract (app.go) is to log and continue with
// the fresh world already under construction — never attempt a migration,
// never crash the process over a stale or foreign snapshot file.
var errSnapshotMismatch = errors.New("snapshot: version/seed/radius does not match this world")

// snapshotDTO is the top-level on-disk shape, exactly the field set the
// design specifies: version/seed/radius (the mismatch gate), the turn and id
// counters (so SSE ids and instance/entity ids stay monotonic and collision-
// free across a restart), every entity (players AND monsters), ground items,
// the quest board, and the disconnect archive. The world map itself is never
// persisted — it regenerates deterministically from (WorldSeed, WorldRadius),
// which is exactly why a mismatch on either invalidates the whole snapshot.
type snapshotDTO struct {
	Version     int    `json:"version"`
	WorldSeed   uint64 `json:"worldSeed"`
	WorldRadius int    `json:"worldRadius"`
	// WorldID is NOT part of the mismatch gate (unlike WorldSeed/WorldRadius):
	// a restored world IS the same world, by definition, whatever id it was
	// minted with — see World.worldID's doc. Round-tripped as-is.
	WorldID      string `json:"worldId"`
	Turn         int64  `json:"turn"`
	NextID       int64  `json:"nextId"`
	NextBubbleID int64  `json:"nextBubbleId"`
	// NextPartyID is not itemized in the design's prose field list (it groups
	// this under "the id/turn counters") but is required alongside a
	// persisted per-entity PartyID: without it, a party minted after restore
	// could collide with a restored one.
	NextPartyID int64                   `json:"nextPartyId"`
	Entities    []entityDTO             `json:"entities"`
	GroundItems []groundItemDTO         `json:"groundItems"`
	Quests      []questDTO              `json:"quests"`
	Archive     map[string]characterDTO `json:"archive"`
}

// itemInstanceDTO mirrors itemInstance for the wire-decoupled disk format.
type itemInstanceDTO struct {
	ID    int64  `json:"id"`
	DefID string `json:"defId"`
}

// entityDTO is the full persisted shape of one entity (player or monster).
// Fields the design calls out as transient — path, attackTarget,
// attackTargetEntity, pendingEquip, bubbleID, streams — are deliberately absent: RestoreState
// leaves them at their Go zero value on every restored entity, and a
// restored PLAYER additionally gets disconnectedAt stamped to load time (see
// RestoreState) rather than persisting the pre-shutdown value.
type entityDTO struct {
	ID          int64             `json:"id"`
	Hex         protocol.Hex      `json:"hex"`
	Token       string            `json:"token"`
	Kind        string            `json:"kind"`
	MonsterKind string            `json:"monsterKind"`
	Name        string            `json:"name"`
	PartyID     int64             `json:"partyId"`
	Class       string            `json:"class"`
	Species     string            `json:"species"`
	HP          int               `json:"hp"`
	MaxHP       int               `json:"maxHp"`
	XP          int               `json:"xp"`
	Items       []itemInstanceDTO `json:"items"`
	CloseSlot   int64             `json:"closeSlot"`
	RangedSlot  int64             `json:"rangedSlot"`
}

// groundItemDTO is every item instance dropped on one hex.
type groundItemDTO struct {
	Hex   protocol.Hex      `json:"hex"`
	Items []itemInstanceDTO `json:"items"`
}

// questDTO mirrors quest in full — id/name/kind/targetN/goalHex/rewardXP are
// deterministic from the world seed (and so would already match a freshly
// generated board when the seed gate passes), but persisting them anyway
// means RestoreState can rebuild the whole board from the snapshot alone,
// with no dependency on generateQuests' output ordering or content.
type questDTO struct {
	ID           int64               `json:"id"`
	Name         string              `json:"name"`
	Kind         string              `json:"kind"`
	TargetN      int                 `json:"targetN"`
	GoalHex      protocol.Hex        `json:"goalHex"`
	RewardXP     int                 `json:"rewardXp"`
	State        protocol.QuestState `json:"state"`
	Progress     int                 `json:"progress"`
	HolderEntity int64               `json:"holderEntity"`
	HolderParty  int64               `json:"holderParty"`
}

// characterDTO mirrors characterRecord for the archive map.
type characterDTO struct {
	Name       string            `json:"name"`
	Class      string            `json:"class"`
	Species    string            `json:"species"`
	XP         int               `json:"xp"`
	Items      []itemInstanceDTO `json:"items"`
	CloseSlot  int64             `json:"closeSlot"`
	RangedSlot int64             `json:"rangedSlot"`
}

// MarshalState serializes the world's persisted state to JSON: every entity
// (players and monsters), ground items, the quest board, the disconnect
// archive, and the turn/id counters — exactly the design's field set. The
// world map is NOT included; it regenerates from (worldSeed, worldRadius),
// which ARE included so a later RestoreState can verify a snapshot actually
// describes this world before trusting it. Takes the world lock itself, so a
// periodic saver goroutine can call it directly without coordinating with
// the control loop.
func (w *World) MarshalState() ([]byte, error) {
	w.mu.Lock()
	dto := w.toDTOLocked()
	w.mu.Unlock()

	data, err := json.Marshal(dto)
	if err != nil {
		return nil, fmt.Errorf("game: marshal snapshot: %w", err)
	}

	return data, nil
}

// toDTOLocked builds the on-disk DTO from live state. Callers hold w.mu.
func (w *World) toDTOLocked() snapshotDTO {
	entities := make([]entityDTO, 0, len(w.entities))
	for _, e := range w.entities {
		entities = append(entities, entityToDTO(e))
	}

	groundItems := make([]groundItemDTO, 0, len(w.groundItems))
	for hex, items := range w.groundItems {
		groundItems = append(groundItems, groundItemDTO{Hex: hex, Items: itemInstancesToDTO(items)})
	}

	quests := make([]questDTO, 0, len(w.quests))
	for _, q := range w.quests {
		quests = append(quests, questToDTO(q))
	}

	archive := make(map[string]characterDTO, len(w.archive))
	for token, rec := range w.archive {
		archive[token] = characterToDTO(rec)
	}

	return snapshotDTO{
		Version: snapshotVersion, WorldSeed: w.worldSeed, WorldRadius: w.radius, WorldID: w.worldID,
		Turn: w.turn, NextID: w.nextID, NextBubbleID: w.nextBubbleID, NextPartyID: w.nextPartyID,
		Entities: entities, GroundItems: groundItems, Quests: quests, Archive: archive,
	}
}

// RestoreState loads a previously marshaled snapshot into w, which MUST be a
// fresh, not-yet-running world (app startup, before World.Run starts the
// control loop) — restoring into a live world would race turn resolution and
// silently corrupt state mid-turn; this is a caller contract, not something
// RestoreState itself can detect.
//
// Returns errSnapshotMismatch if the snapshot's version, world seed, or
// world radius does not match w's — the caller's contract (see app.go) is to
// log and keep the fresh world already under construction, never migrate,
// never crash.
//
// Every restored PLAYER comes back marked disconnected as of THIS call
// (disconnectedAt = now, streams = 0), not the pre-shutdown disconnect time:
// the removal-grace clock restarts at load, so an unclaimed entity sweeps
// (and archives) after one full grace from restart, not instantly. Path,
// attackTarget, attackTargetEntity, pendingEquip, and bubbleID are left at
// their zero value on every entity (players and monsters alike) — bubbles are never persisted
// and are recomputed from positions on the first tick.
func (w *World) RestoreState(data []byte) error {
	var dto snapshotDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("game: unmarshal snapshot: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if dto.Version != snapshotVersion || dto.WorldSeed != w.worldSeed || dto.WorldRadius != w.radius {
		return fmt.Errorf(
			"%w: snapshot has version=%d worldSeed=%d worldRadius=%d, world has version=%d worldSeed=%d worldRadius=%d",
			errSnapshotMismatch, dto.Version, dto.WorldSeed, dto.WorldRadius,
			snapshotVersion, w.worldSeed, w.radius,
		)
	}

	entities, byToken := entitiesFromDTO(dto.Entities, w.now())

	w.turn = dto.Turn
	w.worldID = dto.WorldID
	w.nextID = dto.NextID
	w.nextBubbleID = dto.NextBubbleID
	w.nextPartyID = dto.NextPartyID
	w.entities = entities
	w.byToken = byToken
	w.groundItems = groundItemsFromDTO(dto.GroundItems)
	w.quests = questsFromDTO(dto.Quests)
	w.archive = archiveFromDTO(dto.Archive)
	// Bubbles are never persisted (recomputed from positions on the first
	// tick) and a fresh world already has an empty pendingInvites, but both
	// are reset explicitly here so RestoreState's postcondition does not
	// depend on the caller having constructed w with NewWorld a moment ago.
	w.bubbles = make(map[int64]*bubble)
	w.pendingInvites = make(map[int64]int64)

	return nil
}

// entitiesFromDTO rebuilds the entities/byToken maps from the persisted
// entity list. Every restored PLAYER gets disconnectedAt stamped to now
// (the sweep grace restarts at load time, not the pre-shutdown value) and
// streams reset to 0; monsters are untouched beyond entityFromDTO's zeroing
// of the transient fields it never sets.
func entitiesFromDTO(dtos []entityDTO, now time.Time) (map[int64]*entity, map[string]*entity) {
	entities := make(map[int64]*entity, len(dtos))
	byToken := make(map[string]*entity, len(dtos))

	for _, ed := range dtos {
		e := entityFromDTO(ed)

		if e.kind == protocol.EntityPlayer {
			e.disconnectedAt = now
			e.streams = 0
		}

		entities[e.id] = e

		if e.token != "" {
			byToken[e.token] = e
		}
	}

	return entities, byToken
}

func groundItemsFromDTO(dtos []groundItemDTO) map[protocol.Hex][]itemInstance {
	groundItems := make(map[protocol.Hex][]itemInstance, len(dtos))
	for _, g := range dtos {
		groundItems[g.Hex] = itemInstancesFromDTO(g.Items)
	}

	return groundItems
}

func questsFromDTO(dtos []questDTO) []*quest {
	quests := make([]*quest, len(dtos))
	for i, qd := range dtos {
		quests[i] = questFromDTO(qd)
	}

	return quests
}

func archiveFromDTO(dtos map[string]characterDTO) map[string]characterRecord {
	archive := make(map[string]characterRecord, len(dtos))
	for token, cd := range dtos {
		archive[token] = characterFromDTO(cd)
	}

	return archive
}

func entityToDTO(e *entity) entityDTO {
	return entityDTO{
		ID: e.id, Hex: e.hex, Token: e.token, Kind: e.kind, MonsterKind: e.monsterKind,
		Name: e.name, PartyID: e.partyID, Class: e.class, Species: e.species,
		HP: e.hp, MaxHP: e.maxHP, XP: e.xp,
		Items: itemInstancesToDTO(e.items), CloseSlot: e.closeSlot, RangedSlot: e.rangedSlot,
	}
}

// entityFromDTO rebuilds an entity from its DTO. The caller (RestoreState)
// is responsible for the player-only disconnectedAt/streams reset; every
// other transient field (path, attackTarget, attackTargetEntity, pendingEquip,
// bubbleID) is left
// at its Go zero value by construction.
func entityFromDTO(ed entityDTO) *entity {
	return &entity{
		id: ed.ID, hex: ed.Hex, token: ed.Token, kind: ed.Kind, monsterKind: ed.MonsterKind,
		name: ed.Name, partyID: ed.PartyID, class: ed.Class, species: ed.Species,
		hp: ed.HP, maxHP: ed.MaxHP, xp: ed.XP,
		items: itemInstancesFromDTO(ed.Items), closeSlot: ed.CloseSlot, rangedSlot: ed.RangedSlot,
	}
}

func itemInstancesToDTO(items []itemInstance) []itemInstanceDTO {
	dtos := make([]itemInstanceDTO, len(items))
	for i, it := range items {
		dtos[i] = itemInstanceDTO{ID: it.id, DefID: it.defID}
	}

	return dtos
}

func itemInstancesFromDTO(dtos []itemInstanceDTO) []itemInstance {
	items := make([]itemInstance, len(dtos))
	for i, d := range dtos {
		items[i] = itemInstance{id: d.ID, defID: d.DefID}
	}

	return items
}

func questToDTO(q *quest) questDTO {
	return questDTO{
		ID: q.id, Name: q.name, Kind: q.kind, TargetN: q.targetN, GoalHex: q.goalHex,
		RewardXP: q.rewardXP, State: q.state, Progress: q.progress,
		HolderEntity: q.holderEntity, HolderParty: q.holderParty,
	}
}

func questFromDTO(qd questDTO) *quest {
	return &quest{
		id: qd.ID, name: qd.Name, kind: qd.Kind, targetN: qd.TargetN, goalHex: qd.GoalHex,
		rewardXP: qd.RewardXP, state: qd.State, progress: qd.Progress,
		holderEntity: qd.HolderEntity, holderParty: qd.HolderParty,
	}
}

func characterToDTO(rec characterRecord) characterDTO {
	return characterDTO{
		Name: rec.name, Class: rec.class, Species: rec.species, XP: rec.xp,
		Items: itemInstancesToDTO(rec.items), CloseSlot: rec.closeSlot, RangedSlot: rec.rangedSlot,
	}
}

func characterFromDTO(cd characterDTO) characterRecord {
	return characterRecord{
		name: cd.Name, class: cd.Class, species: cd.Species, xp: cd.XP,
		items: itemInstancesFromDTO(cd.Items), closeSlot: cd.CloseSlot, rangedSlot: cd.RangedSlot,
	}
}
