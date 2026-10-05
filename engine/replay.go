package engine

import (
	"encoding/json"
	"fmt"
)

// ReplayVersion is bumped when the replay format or rules change incompatibly.
const ReplayVersion = 1

// Replay is (seed, initial state, orders[]) — enough to regenerate a match.
type Replay struct {
	Version int               `json:"version"`
	Setup   Setup             `json:"setup"`
	Initial State             `json:"initial"`
	Turns   []map[int][]Order `json:"turns"`
	// FinalHash is the state hash after the last turn, if known.
	FinalHash string `json:"final_hash,omitempty"`
}

// NewReplay starts a replay record for a match.
func NewReplay(st Setup, initial State) *Replay {
	init := initial.Clone()
	init.Log = nil
	return &Replay{Version: ReplayVersion, Setup: st, Initial: init}
}

// Record appends a turn's orders.
func (r *Replay) Record(orders map[int][]Order) {
	cp := make(map[int][]Order, len(orders))
	for k, v := range orders {
		cp[k] = append([]Order(nil), v...)
	}
	r.Turns = append(r.Turns, cp)
}

// Run replays every turn through the engine and returns the final state and
// per-turn events.
func (r *Replay) Run(c *Content) (State, [][]Event, error) {
	if r.Version != ReplayVersion {
		return State{}, nil, fmt.Errorf("replay version %d, want %d", r.Version, ReplayVersion)
	}
	s := r.Initial.Clone()
	var all [][]Event
	for i, orders := range r.Turns {
		if s.Ended() {
			return s, all, fmt.Errorf("turn %d: orders after match end", i+1)
		}
		var ev []Event
		s, ev = Step(c, s, orders, s.Match.Seed)
		all = append(all, ev)
	}
	return s, all, nil
}

// Marshal encodes the replay as JSON.
func (r *Replay) Marshal() ([]byte, error) { return json.MarshalIndent(r, "", " ") }

// UnmarshalReplay decodes a replay.
func UnmarshalReplay(b []byte) (*Replay, error) {
	var r Replay
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
