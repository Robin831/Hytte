package math

import "fmt"

// Validate checks that (a, b, op) describes a fact within the supported
// 1–10 universe and computes the expected answer. It returns whether the
// supplied user answer matches the expected answer.
//
// For multiplication, both a and b must lie in [MinOperand, MaxOperand].
// For division, b (the divisor) must lie in [MinOperand, MaxOperand], a
// (the dividend) must be divisible by b, and the resulting quotient must
// also lie in [MinOperand, MaxOperand]. This restricts division facts to
// the same 100-fact universe enumerated by AllFacts.
func Validate(a, b int, op string, userAnswer int) (isCorrect bool, expected int, err error) {
	switch op {
	case OpMultiply:
		if a < MinOperand || a > MaxOperand || b < MinOperand || b > MaxOperand {
			return false, 0, fmt.Errorf("multiplication operands out of range [%d,%d]: a=%d b=%d",
				MinOperand, MaxOperand, a, b)
		}
		expected = a * b
	case OpDivide:
		if b < MinOperand || b > MaxOperand {
			return false, 0, fmt.Errorf("division divisor out of range [%d,%d]: b=%d",
				MinOperand, MaxOperand, b)
		}
		if a < MinOperand*MinOperand || a > MaxOperand*MaxOperand {
			return false, 0, fmt.Errorf("division dividend out of range [1,100]: a=%d", a)
		}
		if a%b != 0 {
			return false, 0, fmt.Errorf("division dividend %d not divisible by divisor %d", a, b)
		}
		q := a / b
		if q < MinOperand || q > MaxOperand {
			return false, 0, fmt.Errorf("division quotient out of range [%d,%d]: q=%d",
				MinOperand, MaxOperand, q)
		}
		expected = q
	default:
		return false, 0, fmt.Errorf("unknown op %q (expected %q or %q)", op, OpMultiply, OpDivide)
	}
	return userAnswer == expected, expected, nil
}
