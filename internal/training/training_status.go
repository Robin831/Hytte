package training

// TrainingStatus represents a user's current training state derived from the
// Acute:Chronic Workload Ratio (ACR) and the recent load trend.
type TrainingStatus string

const (
	// StatusInsufficientData is returned when there is not enough training
	// history to classify status (fewer than 4 weeks of data or nil ACR).
	StatusInsufficientData TrainingStatus = "insufficient_data"

	// StatusDetraining indicates the acute workload is well below the chronic
	// baseline (ACR < 0.8), suggesting the athlete is doing less than usual.
	StatusDetraining TrainingStatus = "detraining"

	// StatusFreshening indicates load is tapering within a healthy ACR range,
	// typical of a pre-competition recovery block.
	StatusFreshening TrainingStatus = "freshening"

	// StatusOptimal indicates ACR is in the sweet spot (0.8–1.3) with a
	// stable load trend.
	StatusOptimal TrainingStatus = "optimal"

	// StatusIncreasing indicates ACR is in the healthy range but load is
	// trending upward — a controlled build phase.
	StatusIncreasing TrainingStatus = "increasing"

	// StatusHighLoad indicates ACR is elevated (1.3–1.5), requiring
	// monitoring to avoid overreaching.
	StatusHighLoad TrainingStatus = "high_load"

	// StatusOverreaching indicates ACR exceeds 1.5, a threshold associated
	// with increased injury risk.
	StatusOverreaching TrainingStatus = "overreaching"
)

// minWeeksForStatus is the minimum number of weekly load records required to
// classify training status. Four weeks provide the chronic baseline.
const minWeeksForStatus = 4

// ClassifyTrainingStatus determines the training status from recent weekly
// loads and the current ACR. The loads slice must be ordered by week_start
// descending (most recent first), which matches the output of GetWeeklyLoads.
//
// Returns StatusInsufficientData if:
//   - currentACR is nil (no chronic baseline yet), or
//   - fewer than minWeeksForStatus weeks of data are available.
//
// ACR thresholds:
//   - > 1.5  → StatusOverreaching
//   - > 1.3  → StatusHighLoad
//   - < 0.8  → StatusDetraining
//   - 0.8–1.3 → trend-based (StatusIncreasing, StatusFreshening, or StatusOptimal)
func ClassifyTrainingStatus(weeklyLoads []WeeklyLoad, currentACR *float64) TrainingStatus {
	if currentACR == nil || len(weeklyLoads) < minWeeksForStatus {
		return StatusInsufficientData
	}

	acr := *currentACR

	switch {
	case acr > 1.5:
		return StatusOverreaching
	case acr > 1.3:
		return StatusHighLoad
	case acr < 0.8:
		return StatusDetraining
	default:
		return classifyTrend(weeklyLoads)
	}
}

// classifyTrend compares the most-recent two weeks' total load against the
// two preceding weeks to determine the training direction.
//
// Ratio > 1.1  → StatusIncreasing (controlled build)
// Ratio < 0.9  → StatusFreshening (tapering)
// Otherwise    → StatusOptimal    (stable)
func classifyTrend(loads []WeeklyLoad) TrainingStatus {
	if len(loads) < 4 {
		return StatusOptimal
	}

	recent := loads[0].TotalLoad + loads[1].TotalLoad
	previous := loads[2].TotalLoad + loads[3].TotalLoad

	if previous == 0 {
		return StatusOptimal
	}

	ratio := recent / previous
	switch {
	case ratio > 1.1:
		return StatusIncreasing
	case ratio < 0.9:
		return StatusFreshening
	default:
		return StatusOptimal
	}
}
