package training

import (
	"database/sql"
	"math"
	"time"
)

// ComputeHRDrift splits samples by time, averages the HR in each half, and
// returns (second−first)/first×100. Returns nil if there are fewer than 10
// samples with a non-zero HR value.
func ComputeHRDrift(samples []Sample, durationSeconds int) *float64 {
	if durationSeconds <= 0 {
		return nil
	}
	midpointMs := int64(durationSeconds) * 500 // half of durationSeconds in ms

	var firstSum, secondSum float64
	var firstCount, secondCount int
	for _, s := range samples {
		if s.HeartRate == 0 {
			continue
		}
		if s.OffsetMs < midpointMs {
			firstSum += float64(s.HeartRate)
			firstCount++
		} else {
			secondSum += float64(s.HeartRate)
			secondCount++
		}
	}

	total := firstCount + secondCount
	if total < 10 || firstCount == 0 || secondCount == 0 {
		return nil
	}

	firstAvg := firstSum / float64(firstCount)
	secondAvg := secondSum / float64(secondCount)
	drift := (secondAvg - firstAvg) / firstAvg * 100
	return &drift
}

// ComputePaceCV converts SpeedMPerS to sec/km pace and returns the coefficient
// of variation (stddev/mean×100). Returns nil if there are fewer than 10
// samples with a non-zero speed.
func ComputePaceCV(samples []Sample) *float64 {
	var paces []float64
	for _, s := range samples {
		if s.SpeedMPerS <= 0 {
			continue
		}
		// Convert m/s to sec/km: 1000 / speed_m_per_s
		paces = append(paces, 1000.0/s.SpeedMPerS)
	}

	if len(paces) < 10 {
		return nil
	}

	var sum float64
	for _, p := range paces {
		sum += p
	}
	mean := sum / float64(len(paces))
	if mean == 0 {
		return nil
	}

	var variance float64
	for _, p := range paces {
		d := p - mean
		variance += d * d
	}
	variance /= float64(len(paces))
	cv := math.Sqrt(variance) / mean * 100
	return &cv
}

// ComputeTrainingLoad returns durationMinutes × (avgHR / maxHR). Returns nil
// if avgHR or maxHR is zero.
func ComputeTrainingLoad(durationMinutes float64, avgHR int, maxHR int) *float64 {
	if avgHR == 0 || maxHR == 0 {
		return nil
	}
	load := durationMinutes * float64(avgHR) / float64(maxHR)
	return &load
}

// ComputeACR queries the last 28 days of training_load values for the given
// user and computes the Acute:Chronic Workload Ratio. It returns the ratio
// (nil if chronic is zero), acute load (7-day sum), and chronic load
// (28-day average scaled to 7 days, i.e. total/4).
func ComputeACR(db *sql.DB, userID int64, asOfDate time.Time) (*float64, float64, float64, error) {
	cutoff := asOfDate.AddDate(0, 0, -28).Format("2006-01-02")
	asOf := asOfDate.Format("2006-01-02")

	rows, err := db.Query(`
		SELECT date(started_at) AS day, training_load
		FROM workouts
		WHERE user_id = ?
		  AND training_load IS NOT NULL
		  AND date(started_at) >= ?
		  AND date(started_at) <= ?
		ORDER BY started_at`,
		userID, cutoff, asOf,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	sevenDayCutoff := asOfDate.AddDate(0, 0, -7).Format("2006-01-02")

	var acute, chronic float64
	for rows.Next() {
		var day string
		var load float64
		if err := rows.Scan(&day, &load); err != nil {
			return nil, 0, 0, err
		}
		chronic += load
		if day > sevenDayCutoff {
			acute += load
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}

	// chronic is 28-day/4 (i.e. average weekly chronic)
	chronic = chronic / 4.0

	if chronic == 0 {
		return nil, acute, chronic, nil
	}

	ratio := acute / chronic
	return &ratio, acute, chronic, nil
}
