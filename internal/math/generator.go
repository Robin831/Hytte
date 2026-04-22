// Package math implements the Regnemester math game engine: question
// generation, answer validation, session lifecycle, and per-fact mastery
// aggregation for multiplication (1–10) and the matching division facts.
package math

import (
	mrand "math/rand/v2"
)

// OpMultiply represents a multiplication fact (a × b).
const OpMultiply = "*"

// OpDivide represents a division fact (a ÷ b).
const OpDivide = "/"

// MinOperand and MaxOperand define the inclusive range for the small
// multiplicands that drive both multiplication and division facts.
const (
	MinOperand = 1
	MaxOperand = 10
)

// Mode constants for question generation. New modes can be added without
// breaking older sessions because the engine falls back to ModeMixed for
// unknown values.
const (
	ModeMixed          = "mixed"
	ModeMultiplication = "mult"
	ModeDivision       = "div"
)

// Fact represents a single math fact. For multiplication, A and B are the
// multiplicands and Expected = A*B. For division, A is the dividend, B is
// the divisor and Expected = A/B (which always lies in [MinOperand, MaxOperand]).
type Fact struct {
	A        int    `json:"a"`
	B        int    `json:"b"`
	Op       string `json:"op"`
	Expected int    `json:"expected"`
}

// AllFacts returns the canonical 200-fact universe: 100 multiplication facts
// for every (a, b) with a, b ∈ [MinOperand, MaxOperand], followed by 100
// division facts c÷b=a (one for every (a, b) pair, covering both divisor
// variants because a and b iterate independently).
func AllFacts() []Fact {
	facts := make([]Fact, 0, 200)
	for a := MinOperand; a <= MaxOperand; a++ {
		for b := MinOperand; b <= MaxOperand; b++ {
			facts = append(facts, Fact{A: a, B: b, Op: OpMultiply, Expected: a * b})
		}
	}
	for a := MinOperand; a <= MaxOperand; a++ {
		for b := MinOperand; b <= MaxOperand; b++ {
			c := a * b
			facts = append(facts, Fact{A: c, B: b, Op: OpDivide, Expected: a})
		}
	}
	return facts
}

// FactsForMode returns the slice of facts that the given mode draws from.
// Unknown modes fall back to the mixed pool so that older clients that send
// new mode strings still get a valid question.
func FactsForMode(mode string) []Fact {
	all := AllFacts()
	switch mode {
	case ModeMultiplication:
		out := make([]Fact, 0, 100)
		for _, f := range all {
			if f.Op == OpMultiply {
				out = append(out, f)
			}
		}
		return out
	case ModeDivision:
		out := make([]Fact, 0, 100)
		for _, f := range all {
			if f.Op == OpDivide {
				out = append(out, f)
			}
		}
		return out
	default:
		return all
	}
}

// NextQuestion returns a random fact for the given mode. The history slice is
// accepted for future mastery-weighted selection but is currently unused —
// foundation bead only delivers uniform random sampling.
func NextQuestion(mode string, _ []Fact) Fact {
	pool := FactsForMode(mode)
	return pool[mrand.IntN(len(pool))]
}

// IsValidMode reports whether the given mode is one of the recognised values.
// Unknown modes are still accepted by NextQuestion (which falls back to
// mixed), but the session layer rejects them at Start time.
func IsValidMode(mode string) bool {
	switch mode {
	case ModeMixed, ModeMultiplication, ModeDivision:
		return true
	default:
		return false
	}
}
