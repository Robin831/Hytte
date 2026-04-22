package math

import "testing"

func TestValidateMultiplicationCorrect(t *testing.T) {
	ok, expected, err := Validate(7, 8, OpMultiply, 56)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Error("expected isCorrect=true")
	}
	if expected != 56 {
		t.Errorf("expected=%d, want 56", expected)
	}
}

func TestValidateMultiplicationWrong(t *testing.T) {
	ok, expected, err := Validate(7, 8, OpMultiply, 55)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("expected isCorrect=false")
	}
	if expected != 56 {
		t.Errorf("expected=%d, want 56", expected)
	}
}

func TestValidateDivisionCorrect(t *testing.T) {
	ok, expected, err := Validate(56, 7, OpDivide, 8)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Error("expected isCorrect=true for 56÷7=8")
	}
	if expected != 8 {
		t.Errorf("expected=%d, want 8", expected)
	}
}

func TestValidateDivisionWrong(t *testing.T) {
	ok, expected, err := Validate(56, 7, OpDivide, 7)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("expected isCorrect=false")
	}
	if expected != 8 {
		t.Errorf("expected=%d, want 8", expected)
	}
}

func TestValidateInvalidOp(t *testing.T) {
	_, _, err := Validate(2, 3, "+", 5)
	if err == nil {
		t.Fatal("expected error for unsupported op")
	}
}

func TestValidateMultiplicationOutOfRange(t *testing.T) {
	cases := [][2]int{{0, 5}, {5, 0}, {11, 2}, {2, 11}, {-1, 3}}
	for _, c := range cases {
		if _, _, err := Validate(c[0], c[1], OpMultiply, 0); err == nil {
			t.Errorf("expected err for multiplication operands %v", c)
		}
	}
}

func TestValidateDivisionInvalid(t *testing.T) {
	t.Run("non-divisible", func(t *testing.T) {
		if _, _, err := Validate(7, 2, OpDivide, 3); err == nil {
			t.Error("expected err for non-divisible 7/2")
		}
	})
	t.Run("divisor-out-of-range", func(t *testing.T) {
		if _, _, err := Validate(20, 11, OpDivide, 0); err == nil {
			t.Error("expected err for divisor=11")
		}
	})
	t.Run("dividend-out-of-range", func(t *testing.T) {
		if _, _, err := Validate(101, 1, OpDivide, 101); err == nil {
			t.Error("expected err for dividend=101")
		}
	})
	t.Run("quotient-out-of-range", func(t *testing.T) {
		// 100 / 1 = 100 > MaxOperand
		if _, _, err := Validate(100, 1, OpDivide, 100); err == nil {
			t.Error("expected err for quotient=100")
		}
	})
}

func TestValidateAllFactsRoundTrip(t *testing.T) {
	for _, f := range AllFacts() {
		ok, expected, err := Validate(f.A, f.B, f.Op, f.Expected)
		if err != nil {
			t.Errorf("fact %+v: unexpected err %v", f, err)
			continue
		}
		if !ok {
			t.Errorf("fact %+v: expected isCorrect=true", f)
		}
		if expected != f.Expected {
			t.Errorf("fact %+v: expected=%d, got %d", f, f.Expected, expected)
		}
	}
}
