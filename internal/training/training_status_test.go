package training

import (
	"testing"
)

func ptrFloat(v float64) *float64 { return &v }

func makeWeeklyLoads(totals ...float64) []WeeklyLoad {
	loads := make([]WeeklyLoad, len(totals))
	for i, total := range totals {
		loads[i] = WeeklyLoad{TotalLoad: total}
	}
	return loads
}

// --- InsufficientData ---

func TestClassifyTrainingStatus_InsufficientData_NilACR(t *testing.T) {
	loads := makeWeeklyLoads(100, 90, 80, 70)
	got := ClassifyTrainingStatus(loads, nil)
	if got != StatusInsufficientData {
		t.Errorf("want %s, got %s", StatusInsufficientData, got)
	}
}

func TestClassifyTrainingStatus_InsufficientData_ThreeWeeks(t *testing.T) {
	loads := makeWeeklyLoads(100, 90, 80) // only 3 weeks
	got := ClassifyTrainingStatus(loads, ptrFloat(1.0))
	if got != StatusInsufficientData {
		t.Errorf("want %s, got %s", StatusInsufficientData, got)
	}
}

func TestClassifyTrainingStatus_InsufficientData_ZeroWeeks(t *testing.T) {
	got := ClassifyTrainingStatus(nil, ptrFloat(1.0))
	if got != StatusInsufficientData {
		t.Errorf("want %s, got %s", StatusInsufficientData, got)
	}
}

// --- Overreaching ---

func TestClassifyTrainingStatus_Overreaching(t *testing.T) {
	loads := makeWeeklyLoads(200, 100, 80, 80)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.6))
	if got != StatusOverreaching {
		t.Errorf("want %s, got %s", StatusOverreaching, got)
	}
}

func TestClassifyTrainingStatus_Overreaching_JustAbove1_5(t *testing.T) {
	loads := makeWeeklyLoads(200, 100, 80, 80)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.51))
	if got != StatusOverreaching {
		t.Errorf("want %s, got %s", StatusOverreaching, got)
	}
}

func TestClassifyTrainingStatus_NotOverreaching_Exactly1_5(t *testing.T) {
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.5))
	if got == StatusOverreaching {
		t.Errorf("ACR=1.5 should not be %s (boundary is exclusive)", StatusOverreaching)
	}
}

// --- HighLoad ---

func TestClassifyTrainingStatus_HighLoad(t *testing.T) {
	loads := makeWeeklyLoads(150, 100, 80, 80)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.4))
	if got != StatusHighLoad {
		t.Errorf("want %s, got %s", StatusHighLoad, got)
	}
}

func TestClassifyTrainingStatus_HighLoad_JustAbove1_3(t *testing.T) {
	loads := makeWeeklyLoads(150, 100, 80, 80)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.31))
	if got != StatusHighLoad {
		t.Errorf("want %s, got %s", StatusHighLoad, got)
	}
}

func TestClassifyTrainingStatus_NotHighLoad_Exactly1_3(t *testing.T) {
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.3))
	if got == StatusHighLoad {
		t.Errorf("ACR=1.3 should not be %s (boundary is exclusive)", StatusHighLoad)
	}
}

// --- Detraining ---

func TestClassifyTrainingStatus_Detraining(t *testing.T) {
	loads := makeWeeklyLoads(50, 80, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(0.5))
	if got != StatusDetraining {
		t.Errorf("want %s, got %s", StatusDetraining, got)
	}
}

func TestClassifyTrainingStatus_Detraining_JustBelow0_8(t *testing.T) {
	loads := makeWeeklyLoads(50, 80, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(0.79))
	if got != StatusDetraining {
		t.Errorf("want %s, got %s", StatusDetraining, got)
	}
}

func TestClassifyTrainingStatus_NotDetraining_Exactly0_8(t *testing.T) {
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(0.8))
	if got == StatusDetraining {
		t.Errorf("ACR=0.8 should not be %s (boundary is inclusive in optimal range)", StatusDetraining)
	}
}

// --- Optimal ---

func TestClassifyTrainingStatus_Optimal_StableLoad(t *testing.T) {
	// recent (100+100=200) vs previous (100+100=200) → ratio 1.0 → Optimal
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.0))
	if got != StatusOptimal {
		t.Errorf("want %s, got %s", StatusOptimal, got)
	}
}

func TestClassifyTrainingStatus_Optimal_AtLowerACRBoundary(t *testing.T) {
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(0.8))
	if got != StatusOptimal {
		t.Errorf("ACR=0.8: want %s, got %s", StatusOptimal, got)
	}
}

func TestClassifyTrainingStatus_Optimal_AtUpperACRBoundary(t *testing.T) {
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.3))
	if got != StatusOptimal {
		t.Errorf("ACR=1.3: want %s, got %s", StatusOptimal, got)
	}
}

// --- Increasing ---

func TestClassifyTrainingStatus_Increasing(t *testing.T) {
	// recent (130+120=250) vs previous (100+100=200) → ratio 1.25 > 1.1
	loads := makeWeeklyLoads(130, 120, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.1))
	if got != StatusIncreasing {
		t.Errorf("want %s, got %s", StatusIncreasing, got)
	}
}

func TestClassifyTrainingStatus_Increasing_JustAboveThreshold(t *testing.T) {
	// recent (112+112=224) vs previous (100+100=200) → ratio 1.12 > 1.1
	loads := makeWeeklyLoads(112, 112, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.0))
	if got != StatusIncreasing {
		t.Errorf("want %s, got %s", StatusIncreasing, got)
	}
}

// --- Freshening ---

func TestClassifyTrainingStatus_Freshening(t *testing.T) {
	// recent (75+70=145) vs previous (100+100=200) → ratio 0.725 < 0.9
	loads := makeWeeklyLoads(75, 70, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(0.9))
	if got != StatusFreshening {
		t.Errorf("want %s, got %s", StatusFreshening, got)
	}
}

func TestClassifyTrainingStatus_Freshening_JustBelowThreshold(t *testing.T) {
	// recent (89+89=178) vs previous (100+100=200) → ratio 0.89 < 0.9
	loads := makeWeeklyLoads(89, 89, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(0.9))
	if got != StatusFreshening {
		t.Errorf("want %s, got %s", StatusFreshening, got)
	}
}

// --- Edge cases ---

func TestClassifyTrainingStatus_ZeroPreviousLoad_NoDiv0(t *testing.T) {
	// Previous 2 weeks are zero — must not divide by zero; returns Optimal.
	loads := makeWeeklyLoads(100, 90, 0, 0)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.0))
	if got != StatusOptimal {
		t.Errorf("want %s, got %s", StatusOptimal, got)
	}
}

func TestClassifyTrainingStatus_ExactlyFourWeeks(t *testing.T) {
	// Exactly minWeeksForStatus weeks is sufficient.
	loads := makeWeeklyLoads(100, 100, 100, 100)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.0))
	if got == StatusInsufficientData {
		t.Errorf("4 weeks should be sufficient, got %s", got)
	}
}

func TestClassifyTrainingStatus_MoreThanFourWeeks(t *testing.T) {
	// Only the 4 most-recent weeks are used for trend; extra history is ignored.
	loads := makeWeeklyLoads(130, 120, 100, 100, 50, 40)
	got := ClassifyTrainingStatus(loads, ptrFloat(1.1))
	if got != StatusIncreasing {
		t.Errorf("want %s, got %s", StatusIncreasing, got)
	}
}
