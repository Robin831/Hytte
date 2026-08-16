package training

// Race prediction snapshots: an honest, fact-based estimate of current race
// fitness, refreshed weekly (just before the Stride plan generates, so the
// coach sees it) and stored append-only so the number has a trend.
//
// This replaces the old on-the-fly Riegel extrapolation from the single
// fastest-average-pace workout of the last 3 months — an anchor that rewarded
// short blasts, downhill runs and miscalibrated treadmills, ignored the VO2max
// it was handed, and produced a different answer whenever one workout aged out
// of the window.
//
// The estimate here is grounded in facts over time:
//   - best sustained efforts extracted from lap data (contiguous lap windows,
//     outdoor running only, net-descent runs excluded) rather than
//     whole-workout averages;
//   - the VO2max estimate history and its recent trend;
//   - twelve weeks of training load plus the acute:chronic ratio;
//   - actual race results, which calibrate how this athlete deviates from the
//     Riegel exponent.
//
// When Claude is enabled the facts go to the model, which weighs them into
// per-distance predictions with a confidence and a short rationale — clamped
// to a deterministic Riegel envelope around the best real sustained effort so
// a hallucinated number can never leave the range the athlete's own data
// supports. With Claude disabled (or on failure) the deterministic baseline
// itself is stored, so the feature degrades to a better version of the old
// formula rather than to nothing.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// predictionEnvelopePct bounds how far the AI's prediction may deviate from
// the deterministic Riegel baseline per distance. Wide enough for the model to
// meaningfully weigh trend/load/race-calibration, tight enough that the
// prediction always stays anchored to a measured effort.
const predictionEnvelopePct = 0.10

// predictionFactsMonths is how far back the facts window reaches. A year of
// efforts and races captures a full season without letting stale fitness
// dominate — the prompt labels every fact with its date so recency is the
// model's to weigh.
const predictionFactsMonths = 12

// baselineRecencyDays bounds the deterministic anchor: only efforts from the
// last 12 weeks may set the baseline, even though the facts window reaches 12
// months. Without this the fastest effort won regardless of age and an April
// interval block outweighed everything the athlete did in August. When no
// effort exists inside the window, the best older one is used and the whole
// snapshot is degraded to low confidence.
const baselineRecencyDays = 84

// minSustainedEffortSeconds is the shortest lap window that counts as a
// sustained effort anchor. 20 minutes is the classic threshold-effort floor;
// anything shorter says little about race fitness at 10K and up.
const minSustainedEffortSeconds = 1200

// maxLapPaceSpread is the max relative spread (fastest vs slowest lap pace)
// inside a window before it is rejected as a mixed reps+recovery block. A
// window of 6-min reps with float jogs averages to a fictional "sustained"
// pace that is neither the work pace nor anything the athlete ran; a genuine
// tempo's laps sit well inside 12% of each other.
const maxLapPaceSpread = 0.12

// spreadMinLapSeconds excludes micro-laps (button presses, transitions) from
// the spread check — their pace is noise — while still counting their time
// and distance in the window totals.
const spreadMinLapSeconds = 45

// Work-lap clustering bounds for interval sessions: a work rep is a lap of
// 2-15 minutes whose pace sits within workLapPaceBand of the session's
// fastest such lap. Recovery jogs fall far outside the band.
const (
	workLapMinSeconds = 120
	workLapMaxSeconds = 900
	workLapPaceBand   = 0.08
	// minIntervalWorkSeconds is the cumulative work time an interval session
	// needs before its work pace counts as threshold evidence.
	minIntervalWorkSeconds = 900
)

// Indoor work-lap selection. Indoors the watch's pace field tracks CADENCE,
// not belt speed, so clustering laps by pace does not merely blur work and
// recovery — it inverts them. Measured on this athlete 2026-08-14: the warmup
// at belt 9.8 km/h read 314 s/km while the 11.8-12.0 km/h threshold reps read
// 340-350 s/km, so a pace-keyed cluster anchored on the warmup and threw the
// reps out, discarding the session entirely — while two easy treadmill runs
// (whose 1 km auto-laps are naturally even) were reported as interval work.
// HR is the only trustworthy indoor signal, so indoor work laps are selected
// by HR against the athlete's own threshold and never by pace.
const (
	// indoorWorkHRMargin is how far below threshold HR an indoor lap may sit
	// and still count as work. Sub-threshold reps are deliberately run a few
	// bpm under; recovery jogs and easy runs sit far further down.
	indoorWorkHRMargin = 10
	// indoorWorkHRFallbackFraction derives the same floor from max HR when the
	// athlete has no threshold HR set.
	indoorWorkHRFallbackFraction = 0.88
	// indoorWorkLapMaxSeconds is the longest lap still treated as a rep
	// indoors. Treadmill sessions routinely use 10-15 min blocks, which the
	// outdoor 15 min cap would drop.
	indoorWorkLapMaxSeconds = 1500
	// indoorMinWorkLapSeconds keeps a recovery jog out of the cluster when HR
	// lag leaves it sitting on the floor: the jog after a threshold rep can
	// still read 153 while the rep read 158-163. Reps are minutes long, jogs
	// are not, so duration separates them where HR alone cannot.
	indoorMinWorkLapSeconds = 180
	// indoorMinReps is the rep floor indoors. Two 15-min blocks at threshold
	// HR is real quality work even though it is only two laps.
	indoorMinReps = 2
)

// indoorWorkHRFloor is the lap-HR threshold above which an indoor lap counts
// as work. Returns 0 when the athlete has neither threshold nor max HR set —
// without an HR anchor there is no trustworthy indoor signal at all, and a
// guess would be worse than reporting nothing.
func indoorWorkHRFloor(thresholdHR, maxHR int) int {
	if thresholdHR > 0 {
		return thresholdHR - indoorWorkHRMargin
	}
	if maxHR > 0 {
		return int(math.Round(float64(maxHR) * indoorWorkHRFallbackFraction))
	}
	return 0
}

// intervalThresholdAdjustment converts an interval work pace into a
// threshold-pace estimate, gated on the HR the work actually cost. A flat
// +6 s/km "reps are faster than continuous" penalty applied blind to HR
// double-counted the conservatism: an athlete running reps AT or just under
// threshold HR (because that is what the plan prescribed) was already at
// threshold effort, and reps run clearly BELOW threshold HR understate the
// pace — the correction runs the other way. Unknown HR keeps the conservative
// default.
func intervalThresholdAdjustment(workHR, thresholdHR int) float64 {
	if workHR <= 0 || thresholdHR <= 0 {
		return 6 // no HR context — conservative default
	}
	gap := thresholdHR - workHR
	switch {
	case gap <= 0:
		// At/above threshold HR: genuinely harder than a continuous hour.
		return 6
	case gap <= 3:
		// Right under threshold: the work pace is ~threshold pace as-is.
		return 0
	default:
		// Clearly sub-threshold: the athlete had more; threshold pace is a
		// little faster than the reps were run.
		return -3
	}
}

// StoredPrediction is one distance's predicted time inside a snapshot.
type StoredPrediction struct {
	Distance      string  `json:"distance"`
	DistanceM     float64 `json:"distance_m"`
	TimeSeconds   int     `json:"time_seconds"`
	PredictedTime string  `json:"predicted_time"`
	PacePerKm     string  `json:"pace_per_km"`
	Confidence    string  `json:"confidence,omitempty"` // low | medium | high
}

// StoredRacePrediction is one persisted prediction snapshot.
type StoredRacePrediction struct {
	ID          int64              `json:"id"`
	UserID      int64              `json:"-"`
	CreatedAt   string             `json:"created_at"`
	Method      string             `json:"method"` // "ai" or "formula"
	Predictions []StoredPrediction `json:"predictions"`
	Rationale   string             `json:"rationale,omitempty"`
	// InputsSummary is the human-readable facts block the snapshot was based
	// on — stored so a prediction can always answer "based on what?".
	InputsSummary string `json:"inputs_summary,omitempty"`
}

// sustainedEffort is a best contiguous lap-window effort extracted from one
// workout.
type sustainedEffort struct {
	WorkoutID       int64
	Date            string
	DurationSeconds float64
	DistanceMeters  float64
	PaceSecPerKm    float64
	AvgHeartRate    int
}

// intervalEffort is the work-lap cluster of one interval session: the reps'
// time-weighted pace and HR with the recovery jogs excluded — the honest
// reading of a session a naive lap window would smear into a fictional
// "sustained" pace.
type intervalEffort struct {
	WorkoutID        int64
	Date             string
	Reps             int
	TotalWorkSeconds float64
	WorkPaceSecPerKm float64
	AvgHeartRate     int
}

// longestRun is the longest recent run — the durability signal for
// half-marathon and marathon confidence. Indoor runs count: duration is real
// time on feet wherever it was run. Indoor DISTANCE is not real, hence the
// flag — see formatFacts, which suppresses it.
type longestRun struct {
	Date            string
	DurationSeconds float64
	DistanceMeters  float64
	Indoor          bool
}

// predictionFacts is everything the estimate is based on.
type predictionFacts struct {
	BestEfforts     []sustainedEffort // best clean sustained efforts, fastest first (max 5)
	IntervalEfforts []intervalEffort  // best recent outdoor interval work paces, fastest first (max 3)
	// IndoorIntervals are recent treadmill quality sessions — HR and duration
	// are trustworthy, watch pace is not, so they inform the AI (alongside
	// the calibration) but never the deterministic anchor.
	IndoorIntervals      []intervalEffort
	TreadmillCalibration string      // athlete-measured, verbatim from prefs
	LongestRecent        *longestRun // longest run in the recency window (indoor included — duration is real)
	ThresholdHR          int         // athlete profile, 0 when unset
	MaxHR                int
	ThresholdPace        int // profile threshold pace in sec/km, 0 when unset
	VO2maxLatest         float64
	VO2maxTrend          []VO2maxEstimate // most recent first (max 12)
	WeeklyLoads          []WeeklyLoad     // most recent first (max 12)
	ACR                  *float64
	RaceResults          []raceResultFact
	GoalRace             string // free-text description from preferences, may be empty
	AsOf                 time.Time
}

// raceResultFact is a completed race from stride_races.
type raceResultFact struct {
	Name       string
	Date       string
	DistanceM  float64
	ResultTime int
}

// raceDistances is the fixed set every snapshot predicts.
var raceDistances = []struct {
	Name string
	M    float64
}{
	{"5K", raceDistance5K},
	{"10K", raceDistance10K},
	{"Half Marathon", raceDistanceHalfMarathon},
	{"Marathon", raceDistanceMarathon},
}

// bestSustainedEfforts extracts, per outdoor running workout of the last
// predictionFactsMonths, the fastest CLEAN contiguous lap window of at least
// minSustainedEffortSeconds (see bestWindow's spread rejection) plus the
// session's interval work-lap cluster (see extractIntervalEffort), and returns
// the overall best of each (fastest pace first; efforts capped at limit,
// intervals at 3). Workouts with a net descent are excluded — a fast downhill
// run is not evidence of flat race fitness. Whole-workout averages are never
// used.
func bestSustainedEfforts(db *sql.DB, userID int64, limit int) ([]sustainedEffort, []intervalEffort, error) {
	since := time.Now().UTC().AddDate(0, -predictionFactsMonths, 0).Format(time.RFC3339)
	rows, err := db.Query(`
		SELECT w.id, w.started_at,
		       l.lap_number, l.duration_seconds, l.distance_meters, l.avg_heart_rate
		FROM workouts w
		JOIN workout_laps l ON l.workout_id = w.id
		WHERE w.user_id = ?
		  AND w.sport = 'running'
		  AND COALESCE(w.is_indoor, 0) = 0
		  AND COALESCE(w.descent_meters, 0) <= COALESCE(w.ascent_meters, 0) + 30
		  AND w.started_at >= ?
		ORDER BY w.id, l.lap_number`,
		userID, since,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var (
		efforts   []sustainedEffort
		intervals []intervalEffort
		currentID int64
		startedAt string
		laps      []predictionLap
	)
	flush := func() {
		if currentID == 0 || len(laps) == 0 {
			return
		}
		if e := bestWindow(laps, currentID, startedAt); e != nil {
			efforts = append(efforts, *e)
		}
		if ie := extractIntervalEffort(laps, currentID, startedAt); ie != nil {
			intervals = append(intervals, *ie)
		}
		laps = laps[:0]
	}
	for rows.Next() {
		var (
			wid   int64
			start string
			n     int
			l     predictionLap
		)
		if err := rows.Scan(&wid, &start, &n, &l.durS, &l.distM, &l.hr); err != nil {
			return nil, nil, err
		}
		if wid != currentID {
			flush()
			currentID, startedAt = wid, start
		}
		laps = append(laps, l)
	}
	flush()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	sort.Slice(efforts, func(i, j int) bool { return efforts[i].PaceSecPerKm < efforts[j].PaceSecPerKm })
	if len(efforts) > limit {
		efforts = efforts[:limit]
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].WorkPaceSecPerKm < intervals[j].WorkPaceSecPerKm })
	if len(intervals) > 3 {
		intervals = intervals[:3]
	}
	return efforts, intervals, nil
}

// predictionLap is the per-lap slice element bestWindow scans over.
type predictionLap struct {
	durS  float64
	distM float64
	hr    int
}

// bestWindow finds the fastest CLEAN contiguous lap window of at least
// minSustainedEffortSeconds in one workout's laps. A window whose lap paces
// spread more than maxLapPaceSpread is a reps+recovery block, not a sustained
// effort — averaging the float jogs in produced a fictional pace nobody ran —
// so mixed windows are rejected outright (interval sessions contribute via
// extractIntervalEffort instead). Returns nil when no window qualifies or the
// resulting pace is implausible (GPS junk).
func bestWindow(laps []predictionLap, workoutID int64, startedAt string) *sustainedEffort {
	var best *sustainedEffort
	for i := 0; i < len(laps); i++ {
		var durS, distM, hrSum, hrN float64
		minPace, maxPace := math.MaxFloat64, 0.0
		for j := i; j < len(laps); j++ {
			durS += laps[j].durS
			distM += laps[j].distM
			if laps[j].hr > 0 {
				hrSum += float64(laps[j].hr)
				hrN++
			}
			// Track the spread over real laps only; micro-laps are noise.
			if laps[j].durS >= spreadMinLapSeconds && laps[j].distM > 0 {
				p := laps[j].durS / (laps[j].distM / 1000.0)
				if p < minPace {
					minPace = p
				}
				if p > maxPace {
					maxPace = p
				}
			}
			if durS < minSustainedEffortSeconds || distM <= 0 {
				continue
			}
			// Mixed window: extending further only widens the spread, so the
			// whole j-loop from this point is reps+recovery. Move on.
			if minPace < math.MaxFloat64 && maxPace/minPace-1 > maxLapPaceSpread {
				break
			}
			pace := durS / (distM / 1000.0)
			// Discard implausible paces: faster than 2:30/km is GPS junk,
			// slower than 8:00/km is not an effort anchor.
			if pace < 150 || pace > 480 {
				continue
			}
			if best == nil || pace < best.PaceSecPerKm {
				hr := 0
				if hrN > 0 {
					hr = int(math.Round(hrSum / hrN))
				}
				best = &sustainedEffort{
					WorkoutID:       workoutID,
					Date:            startedAt,
					DurationSeconds: durS,
					DistanceMeters:  distM,
					PaceSecPerKm:    pace,
					AvgHeartRate:    hr,
				}
			}
		}
	}
	return best
}

// extractIntervalEffort clusters one workout's work laps: laps of 2-15 min
// whose pace sits within workLapPaceBand of the session's fastest such lap.
// When the cluster carries at least minIntervalWorkSeconds of cumulative work
// it is returned as the session's honest quality reading — work pace and HR
// with the recovery jogs excluded.
func extractIntervalEffort(laps []predictionLap, workoutID int64, startedAt string) *intervalEffort {
	fastest := math.MaxFloat64
	for _, l := range laps {
		if l.durS < workLapMinSeconds || l.durS > workLapMaxSeconds || l.distM <= 0 {
			continue
		}
		p := l.durS / (l.distM / 1000.0)
		if p >= 150 && p < fastest {
			fastest = p
		}
	}
	if fastest == math.MaxFloat64 {
		return nil
	}
	var durS, distM, hrSum, hrN float64
	reps := 0
	for _, l := range laps {
		if l.durS < workLapMinSeconds || l.durS > workLapMaxSeconds || l.distM <= 0 {
			continue
		}
		p := l.durS / (l.distM / 1000.0)
		if p > fastest*(1+workLapPaceBand) {
			continue
		}
		reps++
		durS += l.durS
		distM += l.distM
		if l.hr > 0 {
			hrSum += float64(l.hr) * l.durS
			hrN += l.durS
		}
	}
	if reps < 3 || durS < minIntervalWorkSeconds || distM <= 0 {
		return nil
	}
	hr := 0
	if hrN > 0 {
		hr = int(math.Round(hrSum / hrN))
	}
	return &intervalEffort{
		WorkoutID:        workoutID,
		Date:             startedAt,
		Reps:             reps,
		TotalWorkSeconds: durS,
		WorkPaceSecPerKm: durS / (distM / 1000.0),
		AvgHeartRate:     hr,
	}
}

// extractIndoorIntervalEffort clusters one INDOOR session's work laps by HR.
// A work lap is a lap of 2-25 min whose average HR reaches hrFloor. Distance
// is deliberately never consulted and the returned WorkPaceSecPerKm is always
// zero: indoors both are cadence artifacts, so this effort can only ever
// report what a treadmill cannot fake — rep count, work time, and HR.
func extractIndoorIntervalEffort(laps []predictionLap, workoutID int64, startedAt string, hrFloor int) *intervalEffort {
	if hrFloor <= 0 {
		return nil
	}
	var durS, hrSum, hrN float64
	reps := 0
	for _, l := range laps {
		if l.durS < indoorMinWorkLapSeconds || l.durS > indoorWorkLapMaxSeconds {
			continue
		}
		if l.hr < hrFloor {
			continue
		}
		reps++
		durS += l.durS
		hrSum += float64(l.hr) * l.durS
		hrN += l.durS
	}
	if reps < indoorMinReps || durS < minIntervalWorkSeconds || hrN <= 0 {
		return nil
	}
	return &intervalEffort{
		WorkoutID:        workoutID,
		Date:             startedAt,
		Reps:             reps,
		TotalWorkSeconds: durS,
		WorkPaceSecPerKm: 0, // never valid indoors
		AvgHeartRate:     int(math.Round(hrSum / hrN)),
	}
}

// indoorIntervalEfforts extracts work-lap clusters from recent INDOOR running
// workouts, keyed on HR rather than pace (see indoorWorkHRFloor). What we
// report onward is only what the treadmill cannot fake: rep count, cumulative
// work time, and HR.
func indoorIntervalEfforts(db *sql.DB, userID int64, thresholdHR, maxHR int) ([]intervalEffort, error) {
	hrFloor := indoorWorkHRFloor(thresholdHR, maxHR)
	if hrFloor <= 0 {
		return nil, nil
	}
	since := time.Now().UTC().AddDate(0, 0, -baselineRecencyDays).Format(time.RFC3339)
	rows, err := db.Query(`
		SELECT w.id, w.started_at,
		       l.lap_number, l.duration_seconds, l.distance_meters, l.avg_heart_rate
		FROM workouts w
		JOIN workout_laps l ON l.workout_id = w.id
		WHERE w.user_id = ?
		  AND w.sport = 'running'
		  AND COALESCE(w.is_indoor, 0) = 1
		  AND w.started_at >= ?
		ORDER BY w.id, l.lap_number`,
		userID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		out       []intervalEffort
		currentID int64
		startedAt string
		laps      []predictionLap
	)
	flush := func() {
		if currentID == 0 || len(laps) == 0 {
			return
		}
		if ie := extractIndoorIntervalEffort(laps, currentID, startedAt, hrFloor); ie != nil {
			out = append(out, *ie)
		}
		laps = laps[:0]
	}
	for rows.Next() {
		var (
			wid   int64
			start string
			n     int
			l     predictionLap
		)
		if err := rows.Scan(&wid, &start, &n, &l.durS, &l.distM, &l.hr); err != nil {
			return nil, err
		}
		if wid != currentID {
			flush()
			currentID, startedAt = wid, start
		}
		laps = append(laps, l)
	}
	flush()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Newest first; cap at 4 so a treadmill-heavy block doesn't flood the prompt.
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	if len(out) > 4 {
		out = out[:4]
	}
	return out, nil
}

// loadTreadmillCalibrationText reads the athlete's measured treadmill
// calibration (same encrypted preference the Stride plan/eval prompts use).
// Empty when unset or undecryptable.
func loadTreadmillCalibrationText(db *sql.DB, userID int64) string {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return ""
	}
	raw := prefs["stride_treadmill_calibration"]
	if raw == "" {
		return ""
	}
	dec, err := encryption.DecryptField(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(dec)
}

// buildPredictionFacts gathers everything the estimate is based on. Partial
// data is fine — each section that is empty simply says so in the prompt.
func buildPredictionFacts(db *sql.DB, userID int64) (*predictionFacts, error) {
	facts := &predictionFacts{AsOf: time.Now().UTC()}

	efforts, intervals, err := bestSustainedEfforts(db, userID, 5)
	if err != nil {
		return nil, fmt.Errorf("best efforts: %w", err)
	}
	facts.BestEfforts = efforts
	facts.IntervalEfforts = intervals

	// Athlete profile anchors: threshold/max HR put every effort's HR in
	// context (an anchor at HR 159 against threshold 163 is sub-maximal, and
	// both the model and the confidence logic must know that).
	if prefs, err := auth.GetPreferences(db, userID); err == nil {
		facts.ThresholdHR = parseIntPref(prefs, "threshold_hr")
		facts.MaxHR = parseIntPref(prefs, "max_hr")
		facts.ThresholdPace = parseIntPref(prefs, "threshold_pace")
	}

	// Durability: the longest run inside the recency window — indoor runs
	// INCLUDED. A treadmill's watch pace is a cadence artifact, but its
	// duration is real time on feet, which is what durability is; excluding
	// indoor here made an 85-minute treadmill Sunday run invisible and pinned
	// half-marathon confidence to low for no reason.
	recentSince := facts.AsOf.AddDate(0, 0, -baselineRecencyDays).Format(time.RFC3339)
	var lr longestRun
	err = db.QueryRow(`
		SELECT started_at, duration_seconds, distance_meters, COALESCE(is_indoor, 0)
		FROM workouts
		WHERE user_id = ? AND sport = 'running'
		  AND started_at >= ? AND duration_seconds > 0
		ORDER BY duration_seconds DESC LIMIT 1`,
		userID, recentSince,
	).Scan(&lr.Date, &lr.DurationSeconds, &lr.DistanceMeters, &lr.Indoor)
	if err == nil {
		facts.LongestRecent = &lr
	}

	// Indoor quality sessions, HR and duration only: watch pace is invalid on
	// a treadmill, so they never anchor the deterministic baseline — but with
	// the athlete's measured calibration they are real evidence the model can
	// cross-check (belt speed × offset from the calibration's own numbers).
	facts.IndoorIntervals, err = indoorIntervalEfforts(db, userID, facts.ThresholdHR, facts.MaxHR)
	if err != nil {
		log.Printf("race prediction: indoor intervals for user %d: %v", userID, err)
	}
	facts.TreadmillCalibration = loadTreadmillCalibrationText(db, userID)

	if latest, err := GetLatestVO2max(db, userID); err == nil && latest != nil {
		facts.VO2maxLatest = latest.VO2max
	}
	if hist, err := GetVO2maxHistory(db, userID, 12); err == nil {
		facts.VO2maxTrend = hist
	}
	if loads, err := GetWeeklyLoads(db, userID, 12); err == nil {
		facts.WeeklyLoads = loads
	}
	if acr, _, _, err := ComputeACR(db, userID, time.Now().UTC()); err == nil {
		facts.ACR = acr
	}

	rows, err := db.Query(`
		SELECT name, date, distance_m, result_time
		FROM stride_races
		WHERE user_id = ? AND result_time IS NOT NULL AND result_time > 0
		ORDER BY date DESC LIMIT 6`,
		userID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r raceResultFact
			var encName string
			if err := rows.Scan(&encName, &r.Date, &r.DistanceM, &r.ResultTime); err == nil {
				r.Name = decryptOrRaw(encName)
				facts.RaceResults = append(facts.RaceResults, r)
			}
		}
	}

	prefs := loadGoalRacePrefs(db, userID)
	facts.GoalRace = prefs
	return facts, nil
}

// decryptOrRaw decrypts a field, tolerating legacy plaintext.
func decryptOrRaw(s string) string {
	if dec, err := encryption.DecryptField(s); err == nil {
		return dec
	}
	return s
}

// loadGoalRacePrefs renders the settings goal race as one line, or "".
func loadGoalRacePrefs(db *sql.DB, userID int64) string {
	rows, err := db.Query(`
		SELECT key, value FROM user_preferences
		WHERE user_id = ? AND key IN ('goal_race_name', 'goal_race_date', 'goal_race_distance', 'goal_race_target_time')`,
		userID,
	)
	if err != nil {
		return ""
	}
	defer rows.Close()
	vals := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			vals[k] = v
		}
	}
	if vals["goal_race_name"] == "" && vals["goal_race_distance"] == "" {
		return ""
	}
	parts := []string{}
	if v := vals["goal_race_name"]; v != "" {
		parts = append(parts, v)
	}
	if v := vals["goal_race_distance"]; v != "" {
		parts = append(parts, v)
	}
	if v := vals["goal_race_date"]; v != "" {
		parts = append(parts, "on "+v)
	}
	if v := vals["goal_race_target_time"]; v != "" {
		parts = append(parts, "target "+v)
	}
	return strings.Join(parts, ", ")
}

// baselineAnchor is what set the deterministic baseline: a threshold-pace
// estimate plus the provenance the confidence logic and the rationale need.
type baselineAnchor struct {
	ThresholdPaceSecPerKm float64
	Description           string
	Stale                 bool // no evidence inside baselineRecencyDays
	WorkDerived           bool // derived from interval work laps
}

// deriveBaselineAnchor turns the facts into a threshold-pace estimate,
// preferring evidence from the last baselineRecencyDays: the best clean
// sustained effort's pace, or the best interval work pace plus a small
// penalty (reps with recoveries run slightly faster than a continuous hour).
// Only when the recency window is empty does older evidence anchor the
// baseline, and then the snapshot is flagged stale (low confidence). Returns
// nil when there is no usable effort at all.
func deriveBaselineAnchor(facts *predictionFacts) *baselineAnchor {
	cutoff := facts.AsOf.AddDate(0, 0, -baselineRecencyDays).Format("2006-01-02")
	pick := func(recentOnly bool) *baselineAnchor {
		var best *baselineAnchor
		consider := func(pace float64, desc string, work bool, date string) {
			if len(date) >= 10 && recentOnly && date[:10] < cutoff {
				return
			}
			if pace <= 0 {
				return
			}
			if best == nil || pace < best.ThresholdPaceSecPerKm {
				best = &baselineAnchor{ThresholdPaceSecPerKm: pace, Description: desc, WorkDerived: work}
			}
		}
		for _, e := range facts.BestEfforts {
			date := e.Date
			if len(date) >= 10 {
				date = date[:10]
			}
			consider(e.PaceSecPerKm,
				fmt.Sprintf("%.1f km sustained in %s on %s (%s/km)", e.DistanceMeters/1000, formatRaceTime(int(e.DurationSeconds)), date, formatPacePerKm(e.PaceSecPerKm)),
				false, e.Date)
		}
		for _, ie := range facts.IntervalEfforts {
			date := ie.Date
			if len(date) >= 10 {
				date = date[:10]
			}
			adj := intervalThresholdAdjustment(ie.AvgHeartRate, facts.ThresholdHR)
			hrNote := "no HR context"
			if ie.AvgHeartRate > 0 && facts.ThresholdHR > 0 {
				hrNote = fmt.Sprintf("work HR %d vs threshold %d", ie.AvgHeartRate, facts.ThresholdHR)
			}
			consider(ie.WorkPaceSecPerKm+adj,
				fmt.Sprintf("%d work reps totalling %s on %s (work pace %s/km, %s, %+.0fs/km continuous adjustment)", ie.Reps, formatRaceTime(int(ie.TotalWorkSeconds)), date, formatPacePerKm(ie.WorkPaceSecPerKm), hrNote, adj),
				true, ie.Date)
		}
		return best
	}
	if a := pick(true); a != nil {
		return a
	}
	if a := pick(false); a != nil {
		a.Stale = true
		a.Description += " (older than the 12-week recency window)"
		return a
	}
	return nil
}

// baselinePredictions computes the deterministic Riegel envelope centre from
// the anchor's threshold pace, treated as a 60-minute race effort — the
// textbook threshold definition. Anchoring on a synthesized one-hour effort
// keeps the Riegel extrapolation ratio small (~1.6x to the half marathon)
// instead of stretching a 20-minute window 4-5x, which is where the exponent
// stops being trustworthy.
func baselinePredictions(anchor *baselineAnchor) map[string]float64 {
	if anchor == nil {
		return nil
	}
	refDistM := 3600.0 / anchor.ThresholdPaceSecPerKm * 1000.0
	base := map[string]float64{}
	for _, rd := range raceDistances {
		base[rd.Name] = riegelPredict(3600.0, refDistM, rd.M)
	}
	return base
}

// formatFacts renders the facts block shared by the AI prompt and the stored
// inputs summary.
func formatFacts(facts *predictionFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "As of %s\n\n", facts.AsOf.Format("2006-01-02"))

	if facts.ThresholdHR > 0 || facts.MaxHR > 0 || facts.ThresholdPace > 0 {
		b.WriteString("Athlete profile: ")
		parts := []string{}
		if facts.ThresholdHR > 0 {
			parts = append(parts, fmt.Sprintf("threshold HR %d", facts.ThresholdHR))
		}
		if facts.MaxHR > 0 {
			parts = append(parts, fmt.Sprintf("max HR %d", facts.MaxHR))
		}
		if facts.ThresholdPace > 0 {
			parts = append(parts, fmt.Sprintf("profile threshold pace %s/km (self-reported, may be optimistic)", formatPacePerKm(float64(facts.ThresholdPace))))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n\n")
	}

	b.WriteString("Best clean sustained efforts (contiguous lap windows >= 20 min with even lap paces, outdoor running, net-descent excluded):\n")
	if len(facts.BestEfforts) == 0 {
		b.WriteString("- none found in the window\n")
	}
	for _, e := range facts.BestEfforts {
		date := e.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		fmt.Fprintf(&b, "- %s: %.1f km in %s (%s/km", date, e.DistanceMeters/1000, formatRaceTime(int(e.DurationSeconds)), formatPacePerKm(e.PaceSecPerKm))
		if e.AvgHeartRate > 0 {
			fmt.Fprintf(&b, ", avg HR %d", e.AvgHeartRate)
		}
		b.WriteString(")\n")
	}

	if len(facts.IntervalEfforts) > 0 {
		b.WriteString("\nOutdoor interval sessions, work laps only (recovery jogs excluded):\n")
		for _, ie := range facts.IntervalEfforts {
			date := ie.Date
			if len(date) >= 10 {
				date = date[:10]
			}
			fmt.Fprintf(&b, "- %s: %d reps, %s total work at %s/km", date, ie.Reps, formatRaceTime(int(ie.TotalWorkSeconds)), formatPacePerKm(ie.WorkPaceSecPerKm))
			if ie.AvgHeartRate > 0 {
				fmt.Fprintf(&b, " (avg work HR %d)", ie.AvgHeartRate)
			}
			b.WriteString("\n")
		}
	}

	if len(facts.IndoorIntervals) > 0 {
		b.WriteString("\nIndoor (treadmill) quality sessions — watch pace is a cadence artifact and is NOT shown; HR and durations are reliable:\n")
		for _, ie := range facts.IndoorIntervals {
			date := ie.Date
			if len(date) >= 10 {
				date = date[:10]
			}
			fmt.Fprintf(&b, "- %s: %d work reps, %s total work", date, ie.Reps, formatRaceTime(int(ie.TotalWorkSeconds)))
			if ie.AvgHeartRate > 0 {
				fmt.Fprintf(&b, " (avg work HR %d)", ie.AvgHeartRate)
			}
			b.WriteString("\n")
		}
	}

	if facts.TreadmillCalibration != "" {
		b.WriteString("\nTreadmill calibration (athlete-measured, authoritative — use these numbers verbatim, e.g. belt speed × the stated offset gives the outdoor-equivalent pace for indoor sessions):\n")
		b.WriteString(facts.TreadmillCalibration)
		b.WriteString("\n")
	}

	if facts.LongestRecent != nil {
		date := facts.LongestRecent.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		if facts.LongestRecent.Indoor {
			// Duration only: a treadmill's distance is derived from the same
			// cadence artifact as its pace, so quoting it here would smuggle
			// back in the exact number the indoor rules elsewhere discard.
			fmt.Fprintf(&b, "\nLongest run in the last %d days: %s on %s (treadmill — time on feet is real, distance is not measured and is omitted)\n",
				baselineRecencyDays, formatRaceTime(int(facts.LongestRecent.DurationSeconds)), date)
		} else {
			fmt.Fprintf(&b, "\nLongest run in the last %d days: %s / %.1f km on %s\n",
				baselineRecencyDays, formatRaceTime(int(facts.LongestRecent.DurationSeconds)),
				facts.LongestRecent.DistanceMeters/1000, date)
		}
	}

	if facts.VO2maxLatest > 0 {
		fmt.Fprintf(&b, "\nVO2max estimate: %.1f (latest)\n", facts.VO2maxLatest)
		if len(facts.VO2maxTrend) > 1 {
			b.WriteString("VO2max history (newest first): ")
			vals := make([]string, 0, len(facts.VO2maxTrend))
			for _, v := range facts.VO2maxTrend {
				vals = append(vals, fmt.Sprintf("%.1f", v.VO2max))
			}
			b.WriteString(strings.Join(vals, ", "))
			b.WriteString("\n")
		}
	}

	if len(facts.WeeklyLoads) > 0 {
		b.WriteString("\nWeekly training load (newest first, total load / workouts):\n")
		for _, wl := range facts.WeeklyLoads {
			fmt.Fprintf(&b, "- %s: %.0f / %d\n", wl.WeekStart, wl.TotalLoad, wl.WorkoutCount)
		}
	}
	if facts.ACR != nil {
		fmt.Fprintf(&b, "Acute:chronic load ratio: %.2f\n", *facts.ACR)
	}

	if len(facts.RaceResults) > 0 {
		b.WriteString("\nActual race results:\n")
		for _, r := range facts.RaceResults {
			fmt.Fprintf(&b, "- %s (%s): %.1f km in %s (%s/km)\n",
				r.Name, r.Date, r.DistanceM/1000, formatRaceTime(r.ResultTime),
				formatPacePerKm(float64(r.ResultTime)/(r.DistanceM/1000)))
		}
	}

	if facts.GoalRace != "" {
		fmt.Fprintf(&b, "\nGoal race: %s\n", facts.GoalRace)
	}
	return b.String()
}

// buildPredictionPrompt asks the model for honest per-distance predictions
// grounded in the facts, with the deterministic baseline named so the model
// knows the envelope it works within.
func buildPredictionPrompt(facts *predictionFacts, anchor *baselineAnchor, baseline map[string]float64) string {
	var b strings.Builder
	b.WriteString("You are a running coach producing an honest race-time prediction for your athlete, based on facts over time — not a single workout, and not wishful thinking.\n\n")
	b.WriteString("## Athlete data\n\n")
	b.WriteString(formatFacts(facts))
	b.WriteString("\n## Baseline (deterministic)\n\n")
	fmt.Fprintf(&b, "Threshold pace estimated at %s/km from: %s.\nRiegel from that pace treated as a 60-minute effort:\n", formatPacePerKm(anchor.ThresholdPaceSecPerKm), anchor.Description)
	for _, rd := range raceDistances {
		if t, ok := baseline[rd.Name]; ok {
			fmt.Fprintf(&b, "- %s: %s\n", rd.Name, formatRaceTime(int(math.Round(t))))
		}
	}
	b.WriteString(`
## Task

Weigh the whole picture. Rules that matter:
- Recency beats magnitude: evidence from the last 12 weeks outweighs anything older, however fast the older effort was.
- Read HR against the profile: an effort at HR well below threshold HR was sub-maximal — the athlete has more than that pace suggests, but without a recent maximal effort or race you cannot verify how much. Never report "high" confidence for any distance unless the window contains a race result or a near-maximal effort (HR at/above threshold sustained).
- Durability gates the long distances: compare the longest recent run to the race duration. A half-marathon or marathon far beyond anything recently run gets a conservative time and at most "low"/"medium" confidence, whatever the speed evidence says.
- Use past race results as calibration: how this athlete actually raced versus what the formula said.
- Training load: an elevated ACR (>1.5) is a risk note for the rationale, not a fitness gain.

Predictions must be achievable on current fitness on a flat course in good conditions — honest, neither optimistic nor sandbagged. Stay within roughly 10% of the baseline; deviate only where the facts argue for it, and say why in the rationale.

Respond with ONLY a JSON object:
{"predictions": [{"distance": "5K", "time_seconds": 1234, "confidence": "high"}, {"distance": "10K", ...}, {"distance": "Half Marathon", ...}, {"distance": "Marathon", ...}], "rationale": "2-4 sentences on what drives the estimate and its confidence"}

- confidence is "high", "medium" or "low" per distance.
- rationale is plain prose for the athlete.
`)
	return b.String()
}

// parsePredictionResponse extracts the model's JSON.
func parsePredictionResponse(response string) (map[string]int, map[string]string, string, error) {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	// Fall back to the first {...} block when the model added prose.
	if !strings.HasPrefix(strings.TrimSpace(response), "{") {
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start < 0 || end <= start {
			return nil, nil, "", errors.New("no JSON object in response")
		}
		response = response[start : end+1]
	}
	var parsed struct {
		Predictions []struct {
			Distance    string `json:"distance"`
			TimeSeconds int    `json:"time_seconds"`
			Confidence  string `json:"confidence"`
		} `json:"predictions"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &parsed); err != nil {
		return nil, nil, "", fmt.Errorf("decode prediction JSON: %w", err)
	}
	times := map[string]int{}
	conf := map[string]string{}
	for _, p := range parsed.Predictions {
		if p.TimeSeconds > 0 {
			times[p.Distance] = p.TimeSeconds
			conf[p.Distance] = p.Confidence
		}
	}
	if len(times) == 0 {
		return nil, nil, "", errors.New("response carried no predictions")
	}
	return times, conf, strings.TrimSpace(parsed.Rationale), nil
}

// formulaConfidences grades the deterministic path's honesty per distance.
// The formula never claims "high": that grade requires a race result or a
// near-maximal effort, which only the AI pass verifies against the facts. A
// stale or work-lap-derived anchor drops everything to low, and the long
// distances drop to low when the longest recent run does not support them.
func formulaConfidences(facts *predictionFacts, anchor *baselineAnchor) map[string]string {
	conf := map[string]string{}
	base := "medium"
	if anchor == nil || anchor.Stale {
		base = "low"
	}
	for _, rd := range raceDistances {
		conf[rd.Name] = base
	}
	conf["Marathon"] = "low"
	// Durability gate: a half prediction needs a recent run at least ~75 min
	// long to be more than a speed extrapolation.
	if facts.LongestRecent == nil || facts.LongestRecent.DurationSeconds < 4500 {
		conf["Half Marathon"] = "low"
	}
	return conf
}

// clampToEnvelope bounds an AI time to ±predictionEnvelopePct of the baseline.
func clampToEnvelope(aiSeconds int, baselineSeconds float64) int {
	lo := baselineSeconds * (1 - predictionEnvelopePct)
	hi := baselineSeconds * (1 + predictionEnvelopePct)
	t := float64(aiSeconds)
	if t < lo {
		t = lo
	}
	if t > hi {
		t = hi
	}
	return int(math.Round(t))
}

// RefreshRacePrediction computes and stores a new prediction snapshot for the
// user. When cfg is enabled the estimate is AI-set within the deterministic
// envelope; otherwise (or when the model call/parse fails) the deterministic
// baseline is stored as method "formula". Returns the stored snapshot, or
// (nil, nil) when the user has no usable effort data at all.
func RefreshRacePrediction(ctx context.Context, db *sql.DB, userID int64, cfg *ClaudeConfig) (*StoredRacePrediction, error) {
	facts, err := buildPredictionFacts(db, userID)
	if err != nil {
		return nil, err
	}
	anchor := deriveBaselineAnchor(facts)
	baseline := baselinePredictions(anchor)
	if baseline == nil {
		return nil, nil // nothing to anchor a prediction on
	}

	method := "formula"
	times := map[string]int{}
	conf := formulaConfidences(facts, anchor)
	for _, rd := range raceDistances {
		times[rd.Name] = int(math.Round(baseline[rd.Name]))
	}
	// The formula path explains itself too — a bare number with an empty
	// rationale reads as authoritative when it is only the envelope centre.
	rationale := fmt.Sprintf(
		"Formula baseline: threshold pace ~%s/km estimated from %s, extrapolated as a 60-minute effort. No AI weighting of trend, load or race calibration has been applied to this snapshot.",
		formatPacePerKm(anchor.ThresholdPaceSecPerKm), anchor.Description,
	)

	if cfg != nil && cfg.Enabled {
		prompt := buildPredictionPrompt(facts, anchor, baseline)
		if response, aiErr := RunPrompt(ctx, cfg, prompt); aiErr == nil {
			if aiTimes, aiConf, aiRationale, perr := parsePredictionResponse(response); perr == nil {
				for _, rd := range raceDistances {
					if t, ok := aiTimes[rd.Name]; ok {
						times[rd.Name] = clampToEnvelope(t, baseline[rd.Name])
					}
				}
				conf = aiConf
				rationale = aiRationale
				method = "ai"
			} else {
				log.Printf("race prediction: parse AI response for user %d: %v (falling back to formula)", userID, perr)
			}
		} else {
			log.Printf("race prediction: AI call for user %d: %v (falling back to formula)", userID, aiErr)
		}
	}

	preds := make([]StoredPrediction, 0, len(raceDistances))
	for _, rd := range raceDistances {
		t := times[rd.Name]
		preds = append(preds, StoredPrediction{
			Distance:      rd.Name,
			DistanceM:     rd.M,
			TimeSeconds:   t,
			PredictedTime: formatRaceTime(t),
			PacePerKm:     formatPacePerKm(float64(t) / (rd.M / 1000.0)),
			Confidence:    conf[rd.Name],
		})
	}

	stored := &StoredRacePrediction{
		UserID:        userID,
		CreatedAt:     facts.AsOf.Format(time.RFC3339),
		Method:        method,
		Predictions:   preds,
		Rationale:     rationale,
		InputsSummary: formatFacts(facts),
	}
	if err := insertRacePrediction(db, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

// insertRacePrediction persists a snapshot (encrypted payloads).
func insertRacePrediction(db *sql.DB, p *StoredRacePrediction) error {
	predBytes, err := json.Marshal(p.Predictions)
	if err != nil {
		return fmt.Errorf("marshal predictions: %w", err)
	}
	encPreds, err := encryption.EncryptField(string(predBytes))
	if err != nil {
		return fmt.Errorf("encrypt predictions: %w", err)
	}
	encRationale := ""
	if p.Rationale != "" {
		if encRationale, err = encryption.EncryptField(p.Rationale); err != nil {
			return fmt.Errorf("encrypt rationale: %w", err)
		}
	}
	encInputs := ""
	if p.InputsSummary != "" {
		if encInputs, err = encryption.EncryptField(p.InputsSummary); err != nil {
			return fmt.Errorf("encrypt inputs: %w", err)
		}
	}
	res, err := db.Exec(`
		INSERT INTO race_predictions (user_id, created_at, method, predictions_json, rationale, inputs_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		p.UserID, p.CreatedAt, p.Method, encPreds, encRationale, encInputs,
	)
	if err != nil {
		return err
	}
	if id, err := res.LastInsertId(); err == nil {
		p.ID = id
	}
	return nil
}

// GetRacePredictionHistory returns the user's latest n snapshots, newest
// first. The returned slice is never nil.
func GetRacePredictionHistory(db *sql.DB, userID int64, n int) ([]StoredRacePrediction, error) {
	rows, err := db.Query(`
		SELECT id, user_id, created_at, method, predictions_json, rationale, inputs_json
		FROM race_predictions
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`,
		userID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]StoredRacePrediction, 0, n)
	for rows.Next() {
		var p StoredRacePrediction
		var encPreds, encRationale, encInputs string
		if err := rows.Scan(&p.ID, &p.UserID, &p.CreatedAt, &p.Method, &encPreds, &encRationale, &encInputs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(decryptOrRaw(encPreds)), &p.Predictions); err != nil {
			log.Printf("race prediction: decode snapshot %d: %v", p.ID, err)
			continue
		}
		if encRationale != "" {
			p.Rationale = decryptOrRaw(encRationale)
		}
		if encInputs != "" {
			p.InputsSummary = decryptOrRaw(encInputs)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetLatestRacePrediction returns the newest snapshot, or nil when none
// exists.
func GetLatestRacePrediction(db *sql.DB, userID int64) (*StoredRacePrediction, error) {
	hist, err := GetRacePredictionHistory(db, userID, 1)
	if err != nil {
		return nil, err
	}
	if len(hist) == 0 {
		return nil, nil
	}
	return &hist[0], nil
}
