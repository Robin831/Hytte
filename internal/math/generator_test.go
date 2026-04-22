package math

import "testing"

func TestAllFactsCount(t *testing.T) {
	facts := AllFacts()
	if len(facts) != 200 {
		t.Fatalf("expected 200 facts, got %d", len(facts))
	}
}

func TestAllFactsEnumeratesEveryMultiplication(t *testing.T) {
	facts := AllFacts()
	got := make(map[[2]int]int) // (a,b) -> expected
	for _, f := range facts {
		if f.Op != OpMultiply {
			continue
		}
		got[[2]int{f.A, f.B}] = f.Expected
	}
	if len(got) != 100 {
		t.Fatalf("expected 100 unique multiplication facts, got %d", len(got))
	}
	for a := MinOperand; a <= MaxOperand; a++ {
		for b := MinOperand; b <= MaxOperand; b++ {
			expected, ok := got[[2]int{a, b}]
			if !ok {
				t.Errorf("missing multiplication fact %d×%d", a, b)
				continue
			}
			if expected != a*b {
				t.Errorf("fact %d×%d expected %d, got %d", a, b, a*b, expected)
			}
		}
	}
}

func TestAllFactsEnumeratesEveryDivision(t *testing.T) {
	facts := AllFacts()
	// Division facts are keyed by (dividend, divisor); each ordered (a,b) pair
	// in the multiplication table produces one division fact (c=a*b)÷b=a, so
	// both divisor variants of every product appear.
	got := make(map[[2]int]int)
	for _, f := range facts {
		if f.Op != OpDivide {
			continue
		}
		got[[2]int{f.A, f.B}] = f.Expected
	}
	if len(got) != 100 {
		t.Fatalf("expected 100 unique division facts, got %d", len(got))
	}
	for a := MinOperand; a <= MaxOperand; a++ {
		for b := MinOperand; b <= MaxOperand; b++ {
			c := a * b
			expected, ok := got[[2]int{c, b}]
			if !ok {
				t.Errorf("missing division fact %d÷%d", c, b)
				continue
			}
			if expected != a {
				t.Errorf("fact %d÷%d expected %d, got %d", c, b, a, expected)
			}
		}
	}
}

func TestAllFactsBothDivisorVariants(t *testing.T) {
	// For any pair a≠b, both c÷a=b and c÷b=a should be present.
	facts := AllFacts()
	have := make(map[[2]int]bool)
	for _, f := range facts {
		if f.Op == OpDivide {
			have[[2]int{f.A, f.B}] = true
		}
	}
	if !have[[2]int{6, 2}] {
		t.Error("missing 6÷2")
	}
	if !have[[2]int{6, 3}] {
		t.Error("missing 6÷3")
	}
	if !have[[2]int{12, 3}] {
		t.Error("missing 12÷3")
	}
	if !have[[2]int{12, 4}] {
		t.Error("missing 12÷4")
	}
}

func TestFactsForMode(t *testing.T) {
	if got := len(FactsForMode(ModeMultiplication)); got != 100 {
		t.Errorf("ModeMultiplication: got %d, want 100", got)
	}
	if got := len(FactsForMode(ModeDivision)); got != 100 {
		t.Errorf("ModeDivision: got %d, want 100", got)
	}
	if got := len(FactsForMode(ModeMixed)); got != 200 {
		t.Errorf("ModeMixed: got %d, want 200", got)
	}
	if got := len(FactsForMode("unknown-mode")); got != 200 {
		t.Errorf("unknown mode should fall back to mixed pool, got %d", got)
	}
}

func TestNextQuestionRespectsMode(t *testing.T) {
	for i := 0; i < 50; i++ {
		q := NextQuestion(ModeMultiplication, nil)
		if q.Op != OpMultiply {
			t.Fatalf("ModeMultiplication produced non-mult fact: %+v", q)
		}
	}
	for i := 0; i < 50; i++ {
		q := NextQuestion(ModeDivision, nil)
		if q.Op != OpDivide {
			t.Fatalf("ModeDivision produced non-div fact: %+v", q)
		}
	}
}

func TestIsValidMode(t *testing.T) {
	for _, m := range []string{ModeMixed, ModeMultiplication, ModeDivision, ModeMarathon, ModeBlitz} {
		if !IsValidMode(m) {
			t.Errorf("expected %q to be a valid mode", m)
		}
	}
	if IsValidMode("") {
		t.Error("empty mode should not be valid")
	}
	if IsValidMode("nope") {
		t.Error("unknown mode should not be valid")
	}
}
