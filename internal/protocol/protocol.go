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
)

// TurnEvent is the payload of an EventTurn frame: the world state after a
// resolved turn. A full entity snapshot every turn keeps clients trivially
// resyncable at this player count; deltas are a later optimization if ever
// needed. It will grow (attacks, deaths, chat) as the game develops.
type TurnEvent struct {
	// Turn is the monotonically increasing world-turn number.
	Turn int64 `json:"turn"`
	// IntervalMs is the runtime turn period in milliseconds (the configured
	// TURN_INTERVAL). The client cannot derive this — TURN_INTERVAL is
	// env-configurable while the cadence constants are fixed — so it rides
	// each bundle and the client re-syncs its playback/input phase clock on
	// every arrival.
	IntervalMs int64 `json:"intervalMs"`
	// Entities is every entity in the world, sorted by ID.
	Entities []Entity `json:"entities"`
}

// Entity is one thing standing on the map. For now every entity is a player;
// kinds (monsters, NPCs) come with later milestones.
type Entity struct {
	ID  int64 `json:"id"`
	Hex Hex   `json:"hex"`
}

// JoinRequest is the body of POST /api/join. A returning client sends its
// stored token to reclaim its entity; an empty token means "new player".
type JoinRequest struct {
	Token string `json:"token"`
}

// JoinResponse identifies the caller's entity. The token is the bearer
// secret for submitting intents — the "name + secret link" auth of the plan,
// minus the name for now.
type JoinResponse struct {
	EntityID int64  `json:"entityId"`
	Token    string `json:"token"`
	Hex      Hex    `json:"hex"`
}

// IntentRequest is the body of POST /api/intent: "walk to Target". Target is
// any walkable hex, not just a neighbor — the server pathfinds from the
// entity's current position and walks the route one hex per turn. A keyboard
// step is simply a Target one hex away. One intent per entity per turn; a
// later submission in the same input window replaces the earlier route.
type IntentRequest struct {
	EntityID int64  `json:"entityId"`
	Token    string `json:"token"`
	Target   Hex    `json:"target"`
}

// ErrorResponse is the JSON body of every non-2xx API response.
type ErrorResponse struct {
	Error string `json:"error"`
}
