// Package engine holds the pure, deterministic rules of the game.
//
// It has no dependencies beyond the standard library and never performs I/O.
// Step(state, orders, seed) -> (state, events) is the whole contract; the
// server, the native client, bots and tests all call it.
package engine

// Pos is a board coordinate. X grows east, Y grows south.
type Pos struct {
	X, Y int
}

// Terrain types.
const (
	TerrainOpen      = "open"
	TerrainCover     = "cover"
	TerrainWall      = "wall"
	TerrainObjective = "objective"
)

// Tile is one board cell.
type Tile struct {
	Terrain string `json:"t"`
	Z       int    `json:"z"`
}

// Board is the grid.
type Board struct {
	W, H  int
	Tiles []Tile // row-major, index = y*W + x
	// DeployCols is the number of columns on each short edge used for deployment.
	DeployCols int
}

// Team is a side. Teams share vision, objectives and score.
type Team struct {
	ID      int
	Score   int
	Kills   int
	Players []int
}

// Player is one participant. Commander is the unit ID of their commander.
type Player struct {
	ID        int
	Team      int
	Commander int
	Name      string
	Connected bool
	// Committed is true once the player's orders for the current turn are locked.
	Committed bool
}

// Status is a temporary condition on a unit.
type Status struct {
	Kind      string // slow root shield cloak mark silence taunt stat hold
	Stat      string // for kind == stat
	Magnitude int
	Turns     int // turns remaining; -1 = until removed by other means
	Source    int // unit ID that applied it (taunt), or -1
}

// Unit is anything on the board: commander, squad unit, summon.
type Unit struct {
	ID, Owner, Team int
	Kind            string // unit kind id or hero id
	Name            string
	IsCommander     bool
	Level, XP       int
	Pos             Pos
	HP, MaxHP       int
	MV, JMP, RNG    int
	ATK, DEF, INI   int
	VIS             int
	Status          []Status
	Cooldowns       map[string]int
	RespawnIn       int // >0 = dead commander waiting to respawn
	ExpiresIn       int // >0 = summon that expires when it reaches 0
	Held            bool
	Overwatch       bool
	OverwatchRNG    int // 0 = use RNG
	OverwatchShots  int
	Dead            bool
	Kills           int
}

// TileEffect is an effect laid on tiles: smoke, reveal.
type TileEffect struct {
	Kind   string // smoke | reveal
	Tiles  []Pos
	Turns  int
	Team   int // owning team (reveal); -1 for none
	Source int // caster unit ID
}

// DelayedAbility is a telegraphed ability waiting to resolve.
type DelayedAbility struct {
	Caster  int
	Team    int
	Ability string
	Target  Pos // recorded target tile
	TargetU int // recorded target unit (-1 if none)
	Turns   int // turns until it resolves (resolves when 0 at step 2)
}

// Objective is a scoring location: one or more tiles.
type Objective struct {
	ID     string
	Tiles  []Pos
	Holder int // team id currently holding, -1 if none
}

// Sighting is the last known position of an enemy unit for a team.
type Sighting struct {
	Team   int // observing team
	UnitID int
	Pos    Pos
	Turn   int
	Kind   string
}

// Phases.
const (
	PhaseOrders = "orders"
	PhaseEnded  = "ended"
)

// MatchMeta describes the match.
type MatchMeta struct {
	ID            string
	Mode          string
	Map           string
	Turn          int
	Phase         string
	Seed          uint64
	TimeControl   string
	Deterministic bool
	MaxTurns      int
	WinScore      int
	// Result is set when Phase == ended: winning team id, -1 for draw.
	Winner int
	Result string // "score" | "turns" | "kills" | "draw" | "forfeit"
}

// State is the complete match state. It is a value type; Step copies it.
type State struct {
	Match      MatchMeta
	Board      Board
	Teams      []Team
	Players    []Player
	Units      []Unit
	Effects    []TileEffect
	Delayed    []DelayedAbility
	Objectives []Objective
	Sightings  []Sighting
	Log        []Event
	NextUnitID int
	// Visible is filled by View only: tiles the viewing team can see.
	Visible []Pos `json:",omitempty"`
}

// Order actions.
const (
	ActMove      = "move"
	ActAttack    = "attack"
	ActAbility   = "ability"
	ActOverwatch = "overwatch"
	ActHold      = "hold"
)

// Order is one intent issued by a player for one unit.
type Order struct {
	UnitID  int
	Action  string
	Path    []Pos // move: successive tiles, not including the start
	Target  Pos   // attack/ability tile
	TargetU int   // attack/ability unit (0 = none; unit IDs start at 1)
	Ability string
}

// Event kinds.
const (
	EvTurnStart       = "TurnStart"
	EvTurnEnd         = "TurnEnd"
	EvMoved           = "Moved"
	EvAttacked        = "Attacked"
	EvDamaged         = "Damaged"
	EvHealed          = "Healed"
	EvDied            = "Died"
	EvSpawned         = "Spawned"
	EvAbilityCast     = "AbilityCast"
	EvAbilityResolved = "AbilityResolved"
	EvTelegraph       = "Telegraph"
	EvRevealed        = "Revealed"
	EvStatus          = "Status"
	EvPushed          = "Pushed"
	EvTeleported      = "Teleported"
	EvObjectiveScored = "ObjectiveScored"
	EvLevelUp         = "LevelUp"
	EvRespawned       = "Respawned"
	EvOrderRejected   = "OrderRejected"
	EvMatchEnd        = "MatchEnd"
	EvOverwatch       = "Overwatch"
	EvHold            = "Hold"
	EvSmoke           = "Smoke"
	EvExpired         = "Expired"
)

// Event is one thing that happened during resolution. Clients animate only
// from events. Fields are used per kind; unused fields are zero.
type Event struct {
	Kind    string `json:"k"`
	Turn    int    `json:"turn"`
	Unit    int    `json:"u,omitempty"`    // acting unit
	Target  int    `json:"tu,omitempty"`   // target unit
	Team    int    `json:"team,omitempty"` // team involved (scores, sightings)
	From    Pos    `json:"from"`
	To      Pos    `json:"to"`
	Path    []Pos  `json:"path,omitempty"`
	Tiles   []Pos  `json:"tiles,omitempty"`
	Dice    []int  `json:"dice,omitempty"`
	Hits    int    `json:"hits,omitempty"`
	Amount  int    `json:"n,omitempty"`
	Ability string `json:"ab,omitempty"`
	Status  string `json:"st,omitempty"`
	Reason  string `json:"why,omitempty"`
	Order   *Order `json:"order,omitempty"`
}
