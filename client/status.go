package client

import (
	"context"
	"errors"
	"rfog/proto"
)

// StatusReport is what `rfog status` prints: enough for a status bar to
// say "two matches waiting on you" without opening the client.
type StatusReport struct {
	Server  string                      `json:"server"`
	Name    string                      `json:"name"`
	Guest   bool                        `json:"guest"`
	Online  int                         `json:"online"`
	Ratings map[string]proto.RatingInfo `json:"ratings,omitempty"`
	Live    []proto.LiveMatch           `json:"live"`
	// Waiting is how many live matches want orders from you now.
	Waiting int `json:"waiting"`
}

// Status connects, reads the welcome, and returns the report. It never
// joins a queue or a match, so running it from a status bar is harmless.
func Status(ctx context.Context, addr string, set Settings) (StatusReport, error) {
	if addr == "" {
		return StatusReport{}, errors.New("no server configured")
	}
	type result struct {
		r   *remote
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := connect(DialTCP, addr, "", proto.Auth{Name: set.Name, Token: set.Token})
		ch <- result{r, err}
	}()
	var res result
	select {
	case res = <-ch:
	case <-ctx.Done():
		return StatusReport{}, ctx.Err()
	}
	if res.err != nil {
		return StatusReport{}, res.err
	}
	defer res.r.close()
	w := res.r.welcome
	rep := StatusReport{Server: addr, Name: w.Name, Guest: w.Guest, Online: w.Online, Ratings: w.Ratings, Live: w.Live}
	if rep.Live == nil {
		rep.Live = []proto.LiveMatch{}
	}
	for _, m := range rep.Live {
		if m.YourTurn {
			rep.Waiting++
		}
	}
	return rep, nil
}
