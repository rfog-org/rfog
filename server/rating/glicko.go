// Package rating implements Glicko-2 (Glickman, 2013). One rating per
// player per mode; a rating period is one match, which is the usual
// simplification for online ladders.
package rating

import "math"

// Rating is a Glicko-2 rating on the display scale (1500 ± RD).
type Rating struct {
	R, RD, Vol float64
}

// Default is an unrated player.
var Default = Rating{R: 1500, RD: 350, Vol: 0.06}

// Result is one game against an opponent: Score is 1 (win), 0.5 (draw), 0 (loss).
type Result struct {
	Opp   Rating
	Score float64
}

// Tau constrains volatility change; 0.5 is Glickman's recommended middle.
const Tau = 0.5

const scale = 173.7178

// Update returns the rating after one period containing results. With no
// results the rating deviation grows (the player is less certain).
func Update(r Rating, results []Result) Rating {
	mu := (r.R - 1500) / scale
	phi := r.RD / scale
	sigma := r.Vol
	if len(results) == 0 {
		phi2 := math.Sqrt(phi*phi + sigma*sigma)
		return Rating{R: r.R, RD: phi2 * scale, Vol: sigma}
	}
	// Step 3-4: estimated variance and improvement.
	v := 0.0
	delta := 0.0
	for _, res := range results {
		muj := (res.Opp.R - 1500) / scale
		phij := res.Opp.RD / scale
		gj := g(phij)
		ej := e(mu, muj, phij)
		v += gj * gj * ej * (1 - ej)
		delta += gj * (res.Score - ej)
	}
	v = 1 / v
	delta *= v
	// Step 5: new volatility by Illinois algorithm.
	a := math.Log(sigma * sigma)
	f := func(x float64) float64 {
		ex := math.Exp(x)
		num := ex * (delta*delta - phi*phi - v - ex)
		den := 2 * (phi*phi + v + ex) * (phi*phi + v + ex)
		return num/den - (x-a)/(Tau*Tau)
	}
	const eps = 0.000001
	A := a
	var B float64
	if delta*delta > phi*phi+v {
		B = math.Log(delta*delta - phi*phi - v)
	} else {
		k := 1.0
		for f(a-k*Tau) < 0 {
			k++
		}
		B = a - k*Tau
	}
	fA, fB := f(A), f(B)
	for math.Abs(B-A) > eps {
		C := A + (A-B)*fA/(fB-fA)
		fC := f(C)
		if fC*fB <= 0 {
			A, fA = B, fB
		} else {
			fA /= 2
		}
		B, fB = C, fC
	}
	sigma2 := math.Exp(A / 2)
	// Step 6-7: new deviation and rating.
	phiStar := math.Sqrt(phi*phi + sigma2*sigma2)
	phi2 := 1 / math.Sqrt(1/(phiStar*phiStar)+1/v)
	mu2 := mu + phi2*phi2*delta/v
	return Rating{R: mu2*scale + 1500, RD: phi2 * scale, Vol: sigma2}
}

func g(phi float64) float64 { return 1 / math.Sqrt(1+3*phi*phi/(math.Pi*math.Pi)) }

func e(mu, muj, phij float64) float64 { return 1 / (1 + math.Exp(-g(phij)*(mu-muj))) }

// Expected returns the win probability of a against b (for matchmaking).
func Expected(a, b Rating) float64 {
	return e((a.R-1500)/scale, (b.R-1500)/scale, b.RD/scale)
}
