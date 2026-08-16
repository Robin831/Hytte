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

// minSustainedEffortSeconds is the shortest lap window that counts as a
// sustained effort anchor. 20 minutes is the classic threshold-effort floor;
// anything shorter says little about race fitness at 10K and up.
const minSustainedEffortSeconds = 1200

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

// predictionFacts is everything the estimate is based on.
type predictionFacts struct {
	BestEfforts  []sustainedEffort // best sustained efforts, fastest first (max 5)
	VO2maxLatest float64
	VO2maxTrend  []VO2maxEstimate // most recent first (max 12)
	WeeklyLoads  []WeeklyLoad     // most recent first (max 12)
	ACR          *float64
	RaceResults  []raceResultFact
	GoalRace     string // free-text description from preferences, may be empty
	AsOf         time.Time
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
// predictionFactsMonths, the fastest contiguous lap window of at least
// minSustainedEffortSeconds, and returns the overall best ones (fastest pace
// first, at most limit). Workouts with a net descent are excluded — a fast
// downhill run is not evidence of flat race fitness. Whole-workout averages
// are never used: the lap window is what distinguishes a 25-minute sustained
// effort inside an interval session from a jog with a fast finish.
func bestSustainedEfforts(db *sql.DB, userID int64, limit int) ([]sustainedEffort, error) {
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
		return nil, err
	}
	defer rows.Close()

	var (
		efforts   []sustainedEffort
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

	sort.Slice(efforts, func(i, j int) bool { return efforts[i].PaceSecPerKm < efforts[j].PaceSecPerKm })
	if len(efforts) > limit {
		efforts = efforts[:limit]
	}
	return efforts, nil
}

// predictionLap is the per-lap slice element bestWindow scans over.
type predictionLap struct {
	durS  float64
	distM float64
	hr    int
}

// bestWindow finds the fastest contiguous lap window of at least
// minSustainedEffortSeconds in one workout's laps. Returns nil when no window
// qualifies or the resulting pace is implausible (GPS junk).
func bestWindow(laps []predictionLap, workoutID int64, startedAt string) *sustainedEffort {
	var best *sustainedEffort
	for i := 0; i < len(laps); i++ {
		var durS, distM, hrSum, hrN float64
		for j := i; j < len(laps); j++ {
			durS += laps[j].durS
			distM += laps[j].distM
			if laps[j].hr > 0 {
				hrSum += float64(laps[j].hr)
				hrN++
			}
			if durS < minSustainedEffortSeconds || distM <= 0 {
				continue
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

// buildPredictionFacts gathers everything the estimate is based on. Partial
// data is fine — each section that is empty simply says so in the prompt.
func buildPredictionFacts(db *sql.DB, userID int64) (*predictionFacts, error) {
	facts := &predictionFacts{AsOf: time.Now().UTC()}

	efforts, err := bestSustainedEfforts(db, userID, 5)
	if err != nil {
		return nil, fmt.Errorf("best efforts: %w", err)
	}
	facts.BestEfforts = efforts

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

// baselinePredictions computes the deterministic Riegel envelope centre: the
// best sustained lap-window effort extrapolated per distance. Returns nil when
// no anchor exists.
func baselinePredictions(facts *predictionFacts) map[string]float64 {
	if len(facts.BestEfforts) == 0 {
		return nil
	}
	anchor := facts.BestEfforts[0]
	base := map[string]float64{}
	for _, rd := range raceDistances {
		base[rd.Name] = riegelPredict(
			anchor.DurationSeconds,
			anchor.DistanceMeters,
			rd.M,
		)
	}
	return base
}

// formatFacts renders the facts block shared by the AI prompt and the stored
// inputs summary.
func formatFacts(facts *predictionFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "As of %s\n\n", facts.AsOf.Format("2006-01-02"))

	b.WriteString("Best sustained efforts (contiguous lap windows >= 20 min, outdoor running, net-descent excluded):\n")
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
func buildPredictionPrompt(facts *predictionFacts, baseline map[string]float64) string {
	var b strings.Builder
	b.WriteString("You are a running coach producing an honest race-time prediction for your athlete, based on facts over time — not a single workout, and not wishful thinking.\n\n")
	b.WriteString("## Athlete data\n\n")
	b.WriteString(formatFacts(facts))
	b.WriteString("\n## Baseline (deterministic Riegel from the best sustained effort)\n\n")
	for _, rd := range raceDistances {
		if t, ok := baseline[rd.Name]; ok {
			fmt.Fprintf(&b, "- %s: %s\n", rd.Name, formatRaceTime(int(math.Round(t))))
		}
	}
	b.WriteString(`
## Task

Weigh the whole picture: whether fitness (VO2max trend, recent efforts) is improving or declining, whether training load supports the longer distances (a runner without long-run volume should get a conservative marathon estimate), and how past race results compare to formula expectations (this athlete's personal Riegel deviation). Predictions must be achievable on current fitness on a flat course in good conditions — honest, neither optimistic nor sandbagged. Stay within roughly 10% of the baseline; deviate only where the facts argue for it, and say why in the rationale.

Respond with ONLY a JSON object:
{"predictions": [{"distance": "5K", "time_seconds": 1234, "confidence": "high"}, {"distance": "10K", ...}, {"distance": "Half Marathon", ...}, {"distance": "Marathon", ...}], "rationale": "2-4 sentences on what drives the estimate and its confidence"}

- confidence is "high", "medium" or "low" per distance (e.g. marathon confidence is low when there is no long-run history).
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
	baseline := baselinePredictions(facts)
	if baseline == nil {
		return nil, nil // nothing to anchor a prediction on
	}

	method := "formula"
	rationale := ""
	times := map[string]int{}
	conf := map[string]string{}
	for _, rd := range raceDistances {
		times[rd.Name] = int(math.Round(baseline[rd.Name]))
	}

	if cfg != nil && cfg.Enabled {
		prompt := buildPredictionPrompt(facts, baseline)
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
