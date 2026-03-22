# Hytte AI Workout Analysis Expansion Plan

## 1. Current State Assessment

### What Exists Today

Hytte has three AI-powered analysis features, all using the Claude CLI as the LLM backend:

**a) Workout Classification (`analysis.go`, `BuildClassificationPrompt`)**
- Classifies a single workout into a type (easy_run, tempo, threshold, intervals, etc.)
- Generates a concise tag (e.g. "6x6min (r1m)"), a one-sentence summary, and a short title
- Stores result in `workout_analyses` table; applies `ai:` prefixed tags to the workout
- Prompt includes: sport, sub-sport, indoor flag, duration, distance, avg HR, and lap table (duration, distance, avg HR, pace/km)
- Does NOT include: max HR, time-series samples, user thresholds, or historical context

**b) Coaching Insights (`insights.go`, `buildInsightsPrompt`)**
- Generates effort_summary, pacing_analysis, hr_zones text, observations[], and suggestions[]
- Prompt includes: date, sport, duration, distance, avg/max HR, avg pace, avg cadence, elevation, and lap table (with max HR per lap)
- Cached in `training_insights` table
- Does NOT include: user's max HR, threshold HR, pace zones, historical workouts, or samples data

**c) Comparison Analysis (`compare_analyze_handler.go`)**
- Compares two workouts side by side with AI-generated summary, strengths, weaknesses, observations
- Includes structural lap-by-lap delta table when workouts are compatible
- Does NOT include: user context, thresholds, or trend data

### Data Available but Unused in AI Prompts

| Data | Available | Used in Prompts |
|------|-----------|-----------------|
| Time-series samples (HR, speed, cadence, altitude, distance) | Yes (`Samples.Points`) | No |
| Max heart rate (per workout) | Yes | Only in insights prompt |
| User's configured max HR | Yes (settings `max_hr`) | No |
| Lactate threshold HR / speed | Yes (lactate module `zones.go`) | No |
| Training zones (Olympiatoppen / Norwegian) | Yes (computed from lactate tests) | No |
| Weekly summaries (volume, count, avg HR) | Yes (`WeeklySummaries`) | No |
| Progression groups (tag-based trend data) | Yes (`GetProgression`) | No |
| Auto-tags (interval structure detection) | Yes (`autotag.go`) | No |
| Workout tags (user + AI) | Yes | No |
| Calories | Yes | No |
| Zone distribution per workout | Yes (`ZoneDistribution`) | No |
| Similar workouts | Yes (`SimilarHandler`) | No |

### Current Settings

- `max_hr` -- user's maximum heart rate (integer, 100-230)
- `claude_enabled` / `claude_cli_path` / `claude_model` -- Claude configuration
- No HR threshold setting
- No pace threshold/zone settings

---

## 2. Competitor Research Findings

### Strava -- Athlete Intelligence (launched 2024, out of beta 2025)

- **30-day trend analysis**: compares current workout to rolling 30-day window; spots gains and patterns in pace, HR, elevation, power, and Relative Effort
- **Zone time distribution**: breaks down time in HR zones and pace zones with insights on building speed/endurance
- **Milestone detection**: surfaces highlights like fastest pace, longest distance, highest effort, biggest climb
- **Multi-sport**: supports run, trail run, ride, gravel ride, MTB, walk, hike; includes virtual run/ride
- **Segment analysis**: AI insights on segment performance
- **Power insights**: for cycling and running power meters
- **Training Focus advice**: personalized recommendations tailored to goals
- **Privacy**: insights visible only to account owner

Sources:
- [Strava Athlete Intelligence Support](https://support.strava.com/hc/en-us/articles/26786795557005-Athlete-Intelligence-on-Strava)
- [Strava Press Release](https://press.strava.com/articles/stravas-athlete-intelligence-translates-workout-data-into-simple-and)
- [Strava Subscriber Updates](https://press.strava.com/articles/strava-enhances-subscriber-experience-with-updates-to-key-features)

### Garmin -- Training Status & Connect+

- **Training Status**: interpretive layer combining Training Load + VO2max; categories include Productive, Maintaining, Peaking, Recovery, Unproductive, Detraining, Overreaching
- **Training Load**: 7-day EPOC sum distributed across anaerobic, high aerobic, low aerobic categories; 4-week distribution view
- **VO2max estimation**: continuously updated from running/cycling activities
- **Recovery Time**: estimates hours until ready for next hard workout
- **Body Battery**: energy level tracking throughout the day (HRV + stress + sleep + activity)
- **Race Predictor**: estimated finish times for 5K, 10K, half marathon, marathon
- **Training Effect**: aerobic and anaerobic training effect per activity (1.0-5.0 scale)
- **Connect+ AI coaching**: adaptive training plans that adjust based on training history and fatigue levels; personalized suggestions via "Active Intelligence"

Sources:
- [Garmin Training Status](https://www.garmin.com/en-US/garmin-technology/running-science/physiological-measurements/training-status/)
- [Garmin Connect+ Launch](https://www.stocktitan.net/news/GRMN/elevate-your-health-and-fitness-goals-with-garmin-lydjxi0nexj8.html)
- [Garmin 2026 Roadmap](https://www.garminnews.com/garmin-q1-2026-update-new-gear-tracking-circadian-sleep-alignment-and-ai-coaching-debuts/)

### Coros -- Training Metrics & EvoLab

- **Training Load**: with terrain-adjusted calculations for hilly running
- **Training Effect & Training Focus**: per-activity intensity classification
- **VO2max**: integrated into Running Fitness widget; more responsive to recent training
- **Structured workout load prediction**: estimates training load before workout completion
- **RPE + voice notes**: post-workout voice recording automatically transcribed to text
- **Conservative AI approach**: AI assistant (Cara) limited to support queries, not training analysis

Sources:
- [Coros January 2026 Update](https://coros.com/stories/coros-metrics/c/january-2026)
- [Coros July 2025 Update](https://support.coros.com/hc/en-us/articles/37715516062740-July-2025-Feature-Update-Highlights)
- [Best Coros Features 2025](https://corosnordic.com/blogs/coros-stories/your-favorite-features-of-2025)

### Industry Trends (2025-2026)

- **Readiness-based training**: HRV-guided training (push/modify/recover decisions) outperforms fixed plans
- **Multi-signal recovery**: stacking sleep, HRV trend, resting HR drift, recent load, missed sessions
- **Holistic fitness**: expanding beyond workouts to sleep, recovery, mental health
- **AI form analysis**: computer vision for technique feedback (primarily strength training)
- **Personalization**: AI that adapts to individual physiology, not just population averages

Sources:
- [Best AI Fitness Apps 2026 - Fitbod](https://fitbod.me/blog/best-ai-fitness-apps-in-2026-which-ones-actually-use-real-data-not-just-buzzwords/)
- [AI Fitness Trends - SensAI](https://www.sensai.fit/blog/best-ai-fitness-apps-2026-fitbod-freeletics-future-trainiac-alternatives)
- [AI Transforming Fitness Apps](https://www.healthandfitness.org/how-ai-is-transforming-fitness-apps/)

---

## 3. Proposed Improvements

### 3.1 Personalized HR Zone Analysis

**Problem**: Current insights prompt mentions HR zones but has no reference to the user's actual zones. Claude guesses based on population averages.

**Solution**: Inject the user's max HR (from settings) and lactate-derived threshold HR / zones (from lactate module) into every AI prompt.

**Prompt addition**:
```
User Profile:
- Max HR: 195 bpm
- Threshold HR: 172 bpm (from lactate test)
- Training Zones (Olympiatoppen):
  Zone 1 (Recovery): 0-140 bpm
  Zone 2 (Endurance): 140-160 bpm
  Zone 3 (Tempo): 160-168 bpm
  Zone 4 (Threshold): 168-180 bpm
  Zone 5 (VO2max): 181-195 bpm
```

**AI output change**: Instead of generic "you spent time in moderate zones", get "You spent 22 minutes in Zone 4 (threshold), which is ideal for this tempo workout given your threshold HR of 172."

### 3.2 Personalized Pace Zone Analysis

**Problem**: No pace thresholds exist in settings. Pace analysis is generic.

**Solution**: Add pace threshold settings (threshold pace, easy pace range, interval pace range). If lactate test data exists, derive pace zones from threshold speed.

**New settings**:
- `threshold_pace` -- e.g. "4:30/km" (sec per km stored)
- `easy_pace_min` / `easy_pace_max` -- optional overrides
- Auto-derive from lactate test threshold speed when available

### 3.3 Trend Detection Over Time

**Problem**: Existing progression data (tag-grouped workout history) and weekly summaries are displayed in charts but never fed to AI.

**Solution**: Create a new "Training Trend Analysis" endpoint that feeds recent history (4-12 weeks) to Claude for pattern detection.

**Prompt approach**: Include the last N weeks of summary data plus progression points for the workout type being analyzed:
```
Training History (last 8 weeks):
| Week | Workouts | Duration | Distance | Avg HR |
| 2026-W10 | 5 | 6:30:00 | 52.3 km | 148 |
| 2026-W11 | 4 | 5:15:00 | 43.1 km | 145 |
...

Similar Past Workouts (threshold intervals):
| Date | Tag | Avg HR | Avg Pace | Recovery HR |
| 2026-02-15 | 6x6min (r1m) | 168 | 4:32/km | - |
| 2026-03-01 | 6x6min (r1m) | 165 | 4:28/km | - |
| 2026-03-15 | 6x6min (r1m) | 163 | 4:25/km | - |  <-- THIS WORKOUT
```

**AI output**: "Your threshold intervals show a clear improving trend: over the last month, your average pace has dropped from 4:32 to 4:25/km while your HR has decreased from 168 to 163 bpm. This cardiac drift improvement suggests your aerobic fitness is building nicely."

### 3.4 Training Load & Volume Analysis

**Problem**: No training load concept exists. Weekly summaries show raw volume but provide no interpretation.

**Solution**: Compute a simple training load metric (duration x intensity, using HR zones or RPE) and track acute (7-day) vs chronic (28-day) load ratio.

**Implementation**:
- Calculate per-workout load: `duration_minutes * (avg_hr / max_hr)` (TRIMP-like)
- Track 7-day (acute) and 28-day (chronic) rolling sums
- Acute:Chronic ratio (ACR) indicates injury risk: < 0.8 = undertrained, 0.8-1.3 = sweet spot, > 1.5 = danger zone
- Feed ACR + load trend into AI prompts

### 3.5 Race Predictions

**Problem**: No race time predictions exist.

**Solution**: Use recent workout data (especially tempo/threshold workouts and long runs) to estimate race finish times. Can be purely AI-driven or use Jack Daniels / Riegel formulas as a baseline that AI contextualizes.

**Approach**:
- Compute predictions using standard formulas (Riegel: T2 = T1 * (D2/D1)^1.06)
- Feed the computed estimates + recent training context to Claude
- AI provides nuanced predictions accounting for training trends, not just a single race result

**Predicted distances**: 5K, 10K, half marathon, marathon

### 3.6 Workout Comparison to Historical Self

**Problem**: The current comparison feature requires manually selecting two workouts. There is no automatic "how does this compare to your average?"

**Solution**: When generating insights for a workout, automatically include stats from similar past workouts (same tag/type, last 3-6 months).

**Prompt addition**:
```
Historical Context (same workout type: threshold intervals, last 6 months):
- Average pace: 4:35/km (this workout: 4:25/km, 3.6% faster)
- Average HR: 167 bpm (this workout: 163 bpm, 2.4% lower)
- You've done 8 similar workouts in this period
- Best pace: 4:22/km (2026-01-10), Worst: 4:48/km (2025-10-05)
```

### 3.7 Weekly/Monthly Training Summary

**Problem**: Weekly data is charted but not analyzed by AI.

**Solution**: New endpoint: `POST /api/training/summary/analyze` that generates an AI summary of the past week/month.

**Prompt includes**:
- Weekly totals (distance, duration, workout count)
- Workout type distribution (e.g. "3 easy runs, 1 interval, 1 long run")
- Load progression (compared to previous weeks)
- Notable achievements or concerns

**AI output**: Structured JSON with `volume_assessment`, `intensity_balance`, `recovery_adequacy`, `recommendations[]`, `highlights[]`

### 3.8 Overtraining / Injury Risk Detection

**Problem**: No warning system for overtraining indicators.

**Solution**: Monitor and flag:
- Sudden volume spikes (> 10% week-over-week increase)
- Elevated HR at same pace (cardiac drift across workouts)
- Acute:Chronic load ratio > 1.3
- Declining performance at maintained or increased effort
- Consecutive hard days without recovery

Feed these signals into the AI prompt for contextual warnings.

### 3.9 VO2max Estimation

**Problem**: No VO2max tracking.

**Solution**: Estimate VO2max from running workouts using the Cooper/ACSM formula or Jack Daniels VDOT tables. Track over time.

**Approach**:
- Use steady-state runs (easy/tempo) where HR and pace are stable
- Formula: VO2max ~ 15.3 * (max HR / resting HR) -- simplified
- Better: use pace-at-threshold + max HR with Daniels tables
- Track estimated VO2max per qualifying workout; show trend

### 3.10 Workout Type Distribution & Recommendations

**Problem**: No analysis of whether the user's training mix is balanced.

**Solution**: Analyze the distribution of workout types over recent weeks and compare to established training principles (e.g. 80/20 polarized training).

**AI prompt includes**:
```
Last 4 weeks workout distribution:
- Easy/Recovery: 12 workouts (58%)
- Tempo/Threshold: 4 workouts (19%)
- Intervals/VO2max: 3 workouts (14%)
- Long runs: 2 workouts (10%)

Recommended distribution (80/20 polarized):
- Easy: ~80%
- Hard (tempo+intervals+race): ~20%
```

### 3.11 Enhanced Single-Workout Analysis

**Problem**: Current insights are generic because they lack the time-series data that tells the real story.

**Solution**: Include computed metrics from the samples data in the prompt:
- HR drift (first half avg vs second half avg)
- Pace consistency (coefficient of variation)
- Cadence patterns
- Elevation-adjusted effort
- Negative/positive split analysis

---

## 4. Settings Additions Needed

| Setting Key | Type | Description | Default |
|-------------|------|-------------|---------|
| `max_hr` | int | Maximum heart rate | (existing) |
| `threshold_hr` | int | Lactate threshold HR (bpm) | auto from lactate test |
| `threshold_pace` | int | Threshold pace (sec/km) | auto from lactate test |
| `easy_pace_min` | int | Easy pace floor (sec/km) | derived from threshold |
| `easy_pace_max` | int | Easy pace ceiling (sec/km) | derived from threshold |
| `resting_hr` | int | Resting heart rate (bpm) | - |
| `ai_trend_weeks` | int | Weeks of history to include in trend analysis | 8 |
| `ai_auto_analyze` | bool | Auto-run AI analysis on upload | false |

**Preference key allowlist update** in `settings_handlers.go`:
```go
"threshold_hr":    true,
"threshold_pace":  true,
"easy_pace_min":   true,
"easy_pace_max":   true,
"resting_hr":      true,
"ai_trend_weeks":  true,
"ai_auto_analyze": true,
```

**Auto-derivation**: When a user has lactate test results, threshold HR and pace should be auto-populated. The lactate module already computes `ThresholdHR` and `ThresholdSpeed` in `zones.go`. Add a helper that reads the latest lactate test and fills missing threshold settings.

---

## 5. Prompt Engineering Improvements

### 5.1 User Profile Context Block

Every AI prompt should include a standard "User Profile" section:

```go
func buildUserProfileBlock(db *sql.DB, userID int64) string {
    // Load max_hr, threshold_hr, threshold_pace, resting_hr from preferences
    // Load latest lactate test zones if available
    // Format into structured text block
}
```

### 5.2 Historical Context Block

For insights and analysis prompts, include recent history:

```go
func buildHistoricalContext(db *sql.DB, userID int64, workout *Workout) string {
    // Load similar workouts (same sport + similar tag)
    // Load last N weeks of summaries
    // Load recent load metrics
    // Format comparison stats
}
```

### 5.3 Richer Workout Data Block

Include computed metrics from samples:

```go
func buildEnrichedWorkoutBlock(w *Workout) string {
    // Compute HR drift from samples
    // Compute pace variability
    // Compute split analysis (negative/positive)
    // Compute time-in-zone distribution
    // Include elevation profile summary
}
```

### 5.4 Structured Output Schemas

Expand the JSON response schemas to capture richer analysis:

```json
{
  "effort_summary": "...",
  "pacing_analysis": "...",
  "hr_analysis": {
    "zone_assessment": "...",
    "drift_assessment": "...",
    "threshold_context": "..."
  },
  "trend_analysis": {
    "fitness_direction": "improving|stable|declining",
    "comparison_to_recent": "...",
    "notable_changes": ["..."]
  },
  "training_load": {
    "session_load": 85,
    "load_context": "..."
  },
  "risk_flags": ["..."],
  "observations": ["..."],
  "suggestions": ["..."],
  "race_predictions": {
    "5k": "22:15",
    "10k": "46:30",
    "half": "1:42:00",
    "marathon": "3:35:00"
  }
}
```

---

## 6. Data Pipeline Changes

### 6.1 New Computed Metrics (server-side, not AI)

These should be computed deterministically so they're fast and consistent:

| Metric | Source | Storage |
|--------|--------|---------|
| HR drift (%) | Samples | Per-workout, computed on demand or at upload |
| Pace variability (CV%) | Samples | Per-workout |
| Time in each HR zone | Samples + user zones | Per-workout (existing `ZoneDistribution`) |
| Training load (TRIMP-like) | Duration + HR + max HR | New column on `workouts` or separate table |
| Acute load (7-day sum) | Aggregation | Computed on demand |
| Chronic load (28-day sum) | Aggregation | Computed on demand |
| ACR (acute:chronic ratio) | Derived | Computed on demand |
| Estimated VO2max | Pace + HR + formulas | New table `vo2max_estimates` |
| Negative/positive split | Samples or laps | Computed on demand |

### 6.2 New Storage

```sql
-- Per-workout computed metrics (populated on upload or first analysis)
ALTER TABLE workouts ADD COLUMN training_load REAL;
ALTER TABLE workouts ADD COLUMN hr_drift_pct REAL;
ALTER TABLE workouts ADD COLUMN pace_cv_pct REAL;

-- VO2max estimates over time
CREATE TABLE vo2max_estimates (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL,
    workout_id INTEGER NOT NULL,
    estimated_vo2max REAL NOT NULL,
    method TEXT NOT NULL,  -- 'daniels', 'cooper', etc.
    created_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (workout_id) REFERENCES workouts(id)
);

-- Weekly training load cache
CREATE TABLE weekly_load (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL,
    week_start TEXT NOT NULL,
    total_load REAL NOT NULL,
    easy_load REAL,
    hard_load REAL,
    workout_count INTEGER,
    UNIQUE(user_id, week_start)
);
```

### 6.3 Auto-Analysis on Upload

When `ai_auto_analyze` is enabled, trigger classification + insights generation asynchronously after a successful FIT file upload. Use a goroutine with a detached context (the pattern already exists in `RunClaudeAnalysis`).

---

## 7. UI Changes for Richer Analysis Display

### 7.1 Enhanced Insights Panel (TrainingDetail.tsx)

Currently the insights display is a simple text block. Expand to structured cards:

- **Effort & Pacing Card**: effort summary + pacing analysis + split visualization
- **HR Zone Card**: zone distribution bar chart + threshold context + drift assessment
- **Trend Card**: "Compared to your last 5 similar workouts..." with mini sparkline
- **Risk Flags**: yellow/red warning badges for overtraining indicators
- **Race Predictions Card**: estimated finish times (when applicable)
- **Suggestions Card**: actionable recommendations

### 7.2 Training Dashboard (new page or extend TrainingTrends.tsx)

- **Training Load Chart**: acute vs chronic load over time with ACR overlay
- **VO2max Trend**: line chart of estimated VO2max over months
- **Workout Distribution Pie**: easy/moderate/hard breakdown
- **Weekly AI Summary**: generated text analysis of the week
- **Fitness Status Badge**: "Productive" / "Maintaining" / "Overreaching" etc.

### 7.3 Settings Page Updates

- New "Training Zones" section:
  - Threshold HR input (with "auto-detect from lactate test" button)
  - Threshold pace input
  - Easy pace range inputs
  - Resting HR input
- New "AI Preferences" section:
  - Trend analysis window (weeks)
  - Auto-analyze on upload toggle

---

## 8. Implementation Phases

### Phase 1: Personalized Prompts (Low effort, high impact)

**Goal**: Make existing AI features significantly better by injecting user context.

1. Load `max_hr` from user preferences and include in all AI prompts
2. Load latest lactate test threshold HR/speed and include in prompts
3. Add `threshold_hr` and `threshold_pace` to settings allowlist
4. Create `buildUserProfileBlock()` helper used by all prompt builders
5. Include the existing `ZoneDistribution` data in insights prompt
6. Update prompt response schemas to reference personalized zones

**Estimated effort**: 2-3 days

### Phase 2: Historical Context (Medium effort, high impact)

**Goal**: Enable trend detection by feeding history into prompts.

1. Create `buildHistoricalContext()` that queries similar past workouts
2. Include last 8 weeks of `WeeklySummaries` in insights prompts
3. Include progression data (same tag) in insights prompts
4. Add new response fields: `trend_analysis`, `fitness_direction`
5. Update `TrainingInsights` model and frontend to display trend data
6. Add auto-derive of threshold settings from lactate module

**Estimated effort**: 3-5 days

### Phase 3: Computed Metrics (Medium effort, medium impact)

**Goal**: Server-side metrics that don't require AI but enrich everything.

1. Implement HR drift computation from samples
2. Implement pace variability (CV%) from samples
3. Implement TRIMP-like training load per workout
4. Compute ACR (acute:chronic load ratio)
5. Add `training_load`, `hr_drift_pct`, `pace_cv_pct` columns
6. Populate on upload; backfill existing workouts
7. Include computed metrics in AI prompts

**Estimated effort**: 5-7 days

### Phase 4: Training Load Dashboard (Higher effort, high impact)

**Goal**: Garmin-like training status visualization.

1. Implement `weekly_load` table and aggregation
2. Create training load chart (acute vs chronic)
3. Implement training status classification (Productive / Maintaining / Overreaching / etc.)
4. Create `POST /api/training/summary/analyze` for weekly AI summary
5. Build dashboard UI components
6. Add risk flags / overtraining warnings

**Estimated effort**: 7-10 days

### Phase 5: Advanced Features (Higher effort, differentiating)

**Goal**: Features that go beyond what competitors offer.

1. VO2max estimation and tracking
2. Race predictions (formula-based + AI-contextualized)
3. Workout type distribution analysis + 80/20 balance check
4. Enhanced single-workout analysis with samples-derived metrics
5. Auto-analysis on upload
6. Comparison to historical self (automatic, not manual selection)

**Estimated effort**: 10-14 days

### Phase 6: Polish & Intelligence (Ongoing)

**Goal**: Refine AI quality and add delightful touches.

1. A/B test different prompt structures for quality
2. Add confidence scoring to AI outputs
3. Seasonal/weather context (if weather data available)
4. Goal-aware analysis (user sets a goal race date + target time)
5. Multi-sport correlation (e.g. cycling impact on running fitness)
6. Export/share training reports

---

## 9. Key Design Principles

1. **Compute deterministically, narrate with AI**: Hard metrics (load, ACR, VO2max, zone time) should be computed server-side with standard formulas. AI adds interpretation, context, and natural language -- not math.

2. **Personalization over population averages**: Every prompt should include the user's actual thresholds. Generic "your HR was moderate" is useless; "you spent 18 minutes above your threshold of 172 bpm" is actionable.

3. **Cache aggressively**: AI calls are expensive and slow. Cache all results. Invalidate only when the underlying workout data changes or the user explicitly requests re-analysis.

4. **Progressive enhancement**: Features should degrade gracefully. No lactate test? Use max HR with standard zone percentages. No max HR? Fall back to age-based estimate or omit zone context. No history? Skip trend analysis.

5. **Privacy first**: Training data and AI analyses remain user-scoped. Following Strava's lead -- insights are never shared publicly.

6. **Prompt efficiency**: Keep prompts concise. Include computed summaries rather than raw sample arrays. A 10,000-point sample array is wasteful; "HR drift: +5.2%, Pace CV: 3.1%, Time in Z4: 22 min" is sufficient.
