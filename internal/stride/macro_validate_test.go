package stride

import (
	"encoding/json"
	"strings"
	"testing"
)

// macroTestStartWeek is a Monday, as every block start must be.
const macroTestStartWeek = "2026-01-05"

// macroTestWeekDate returns the date dayOffset days into 0-based block week i.
// i may run past the end of the block: races a few weeks beyond the horizon
// are part of what the coach is shown, so they are part of what is tested.
func macroTestWeekDate(i, dayOffset int) string {
	start, err := parseWeekDate(macroTestStartWeek)
	if err != nil {
		panic(err) // A constant fixture date that stops parsing is a bug here.
	}
	return start.AddDate(0, 0, 7*i+dayOffset).Format(dateLayout)
}

// macroTestWeekStart is the Monday of 0-based block week i, and
// macroTestRaceDay the Saturday of that week.
func macroTestWeekStart(i int) string { return macroTestWeekDate(i, 0) }
func macroTestRaceDay(i int) string   { return macroTestWeekDate(i, 5) }

// macroTestWeeklyCap is the athlete's weekly distance cap for the fixture. The
// curve below peaks exactly on it, so the cap rule is exercised at its edge by
// the valid case as well as by the rejection case.
const macroTestWeeklyCap = 90.0

// macroTestWeekKm is the fixture's volume curve: five 3-up-1-down mesocycles,
// a 3-week sharpening block and a 3-week taper into the goal race. Every step
// is inside +10% except the weeks directly after a deload (indices 4, 8, 12,
// 16, 20), which return to the pre-deload level as the doctrine allows.
var macroTestWeekKm = []float64{
	50, 55, 60, 40,
	60, 66, 70, 45,
	70, 76, 80, 50,
	78, 85, 88, 55,
	85, 88, 90, 55,
	85, 88, 80,
	65, 50, 40,
}

// macroTestMesocycles tiles the 26 weeks with no gaps or overlaps.
var macroTestMesocycles = []struct {
	name  string
	phase string
	weeks int
}{
	{"Base 1", MacroPhaseBase, 4},
	{"Base 2", MacroPhaseBase, 4},
	{"Build 1", MacroPhaseBuild, 4},
	{"Build 2", MacroPhaseBuild, 4},
	{"Peak", MacroPhasePeak, 4},
	{"Sharpen", MacroPhasePeak, 3},
	{"Taper and race", MacroPhaseTaper, 3},
}

// macroTestWeekLoad picks a load level for week i: the last week of each
// 4-week mesocycle deloads, the closing three weeks taper, and the rest build.
func macroTestWeekLoad(i int, phase string) string {
	switch {
	case i >= 23:
		return LoadLevelTaper
	case i < 20 && (i+1)%4 == 0:
		return LoadLevelDeload
	case phase == MacroPhaseBase:
		return LoadLevelNormal
	default:
		return LoadLevelBuild
	}
}

// validMacroPlan builds a plan that satisfies every rule, plus the context it
// was planned against: a half-marathon A race in the final week and a 10 km
// tune-up in the peak block. Each test mutates its own copy.
func validMacroPlan(t *testing.T) (*MacroPlanResponse, MacroValidationContext) {
	t.Helper()

	if _, err := parseMondayWeek(macroTestStartWeek); err != nil {
		t.Fatalf("parse fixture start week: %v", err)
	}
	weekStart, raceDay := macroTestWeekStart, macroTestRaceDay

	if len(macroTestWeekKm) != MacroBlockWeeks {
		t.Fatalf("fixture has %d weekly volumes, want %d", len(macroTestWeekKm), MacroBlockWeeks)
	}

	// Expand the mesocycle table into the plan's periodisation and remember
	// which mesocycle each week belongs to.
	mesocycles := make([]Mesocycle, 0, len(macroTestMesocycles))
	weekMeso := make([]int, 0, MacroBlockWeeks)
	for i, m := range macroTestMesocycles {
		mesocycles = append(mesocycles, Mesocycle{
			Name:      m.name,
			Phase:     m.phase,
			StartWeek: weekStart(len(weekMeso)),
			Weeks:     m.weeks,
			Focus:     "Develop " + strings.ToLower(m.name) + " qualities.",
		})
		for w := 0; w < m.weeks; w++ {
			weekMeso = append(weekMeso, i)
		}
	}
	if len(weekMeso) != MacroBlockWeeks {
		t.Fatalf("fixture mesocycles cover %d weeks, want %d", len(weekMeso), MacroBlockWeeks)
	}

	hmRaceID := int64(1)
	tuneUpRaceID := int64(2)
	hmWeek := MacroBlockWeeks - 1 // index 25
	tuneUpWeek := 17

	weeks := make([]MacroWeekResponse, MacroBlockWeeks)
	for i := range weeks {
		meso := macroTestMesocycles[weekMeso[i]]
		phase := meso.phase
		if i == hmWeek {
			phase = MacroPhaseRace
		}
		w := MacroWeekResponse{
			WeekStart:      weekStart(i),
			Seq:            i + 1,
			Phase:          phase,
			Mesocycle:      meso.name,
			LoadLevel:      macroTestWeekLoad(i, meso.phase),
			TargetKm:       macroTestWeekKm[i],
			TargetSessions: 5,
			KeySessions: []KeySession{
				{Type: "threshold", Focus: "Threshold volume at 2 mmol/l."},
				{Type: "long_run", Focus: "Aerobic durability."},
			},
			Intent: "Build on the previous week without exceeding the ramp limit.",
		}
		switch i {
		case hmWeek:
			w.RaceID = &hmRaceID
		case tuneUpWeek:
			w.RaceID = &tuneUpRaceID
		}
		weeks[i] = w
	}

	plan := &MacroPlanResponse{
		Goal: MacroGoal{
			PrimaryFocus:  "half-marathon development",
			Statement:     "Run a personal best half marathon at the end of the block.",
			TargetHMTimeS: 4920,
			Benchmark:     "Goal half marathon in week 26",
			Rationale:     "Current threshold pace and race predictions support a 1:22 target.",
			AnchorRaceID:  &hmRaceID,
		},
		Mesocycles: mesocycles,
		Weeks:      weeks,
	}

	in := MacroValidationContext{
		StartWeek:         macroTestStartWeek,
		WeeklyDistanceCap: macroTestWeeklyCap,
		Races: []Race{
			{ID: hmRaceID, Name: "Goal Half", Date: raceDay(hmWeek), DistanceM: 21097.5, Priority: "A"},
			{ID: tuneUpRaceID, Name: "Tune-up 10K", Date: raceDay(tuneUpWeek), DistanceM: 10000, Priority: "B"},
		},
	}
	return plan, in
}

func TestValidateMacroPlan(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(plan *MacroPlanResponse, in *MacroValidationContext)
		wantErr bool
		want    []string
	}{
		{
			name:   "valid plan",
			mutate: func(*MacroPlanResponse, *MacroValidationContext) {},
		},
		{
			name: "no weekly distance cap configured",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.WeeklyDistanceCap = 0
			},
		},
		{
			name: "wrong number of weeks",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks = plan.Weeks[:MacroBlockWeeks-1]
			},
			wantErr: true,
			want:    []string{"expected exactly 26 weeks, got 25"},
		},
		{
			name: "week does not start on a Monday",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[3].WeekStart = "2026-01-27"
			},
			wantErr: true,
			want:    []string{"every week must start on a Monday"},
		},
		{
			name: "gap in the week sequence",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[10].WeekStart = "2026-03-23"
			},
			wantErr: true,
			want:    []string{"week_start should be 2026-03-16", "contiguous Mondays"},
		},
		{
			name: "week_start is not a date",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[2].WeekStart = "week three"
			},
			wantErr: true,
			want:    []string{`week_start "week three" is not a YYYY-MM-DD date`},
		},
		{
			name: "seq out of order",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[7].Seq = 9
			},
			wantErr: true,
			want:    []string{"seq is 9, expected 8"},
		},
		{
			name: "unknown phase",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[2].Phase = "sharpen"
			},
			wantErr: true,
			want:    []string{`phase "sharpen" is not one of`, `"recovery"`},
		},
		{
			name: "unknown load level",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[2].LoadLevel = "cruise"
			},
			wantErr: true,
			want:    []string{`load_level "cruise" is not one of`},
		},
		{
			name: "empty load level",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[2].LoadLevel = ""
			},
			wantErr: true,
			want:    []string{`load_level "" is not one of`},
		},
		{
			name: "dangling mesocycle reference",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[9].Mesocycle = "Fartlek block"
			},
			wantErr: true,
			want:    []string{`mesocycle "Fartlek block" is not one of the returned mesocycles`},
		},
		{
			name: "mesocycles leave a week uncovered",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[len(plan.Mesocycles)-1].Weeks = 2
			},
			wantErr: true,
			want:    []string{"week(s) 26 are not covered by any mesocycle"},
		},
		{
			name: "mesocycles overlap",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[1].StartWeek = plan.Mesocycles[0].StartWeek
			},
			wantErr: true,
			want:    []string{"are covered by more than one mesocycle"},
		},
		{
			name: "mesocycle runs past the end of the block",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[len(plan.Mesocycles)-1].Weeks = 5
			},
			wantErr: true,
			want:    []string{"falls outside the 26-week block"},
		},
		{
			name: "mesocycle starts before the block",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				// The Monday one week before the block start.
				plan.Mesocycles[0].StartWeek = "2025-12-29"
			},
			wantErr: true,
			want:    []string{"falls outside the 26-week block"},
		},
		{
			name: "mesocycle name is empty",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[0].Name = ""
			},
			wantErr: true,
			want:    []string{"mesocycle 1: name is empty"},
		},
		{
			name: "two mesocycles share a name",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[2].Name = plan.Mesocycles[0].Name
			},
			wantErr: true,
			want:    []string{`mesocycle "Base 1": name is used by more than one mesocycle`},
		},
		{
			name: "unknown mesocycle phase",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[1].Phase = "sharpen"
			},
			wantErr: true,
			want:    []string{`mesocycle "Base 2": phase "sharpen" is not one of`},
		},
		{
			name: "mesocycle spans no weeks",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[3].Weeks = 0
			},
			wantErr: true,
			want:    []string{`mesocycle "Build 2": weeks is 0; a mesocycle must span at least one week`},
		},
		{
			name: "mesocycle start_week is not a date",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles[2].StartWeek = "mid-March"
			},
			wantErr: true,
			want:    []string{`mesocycle "Build 1": start_week "mid-March" is not a YYYY-MM-DD date`},
		},
		{
			name: "mesocycle start_week is not a block Monday",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				// One day after the Monday this mesocycle should start on.
				plan.Mesocycles[2].StartWeek = "2026-03-03"
			},
			wantErr: true,
			want:    []string{`mesocycle "Build 1": start_week 2026-03-03 is not aligned to the block's Mondays`},
		},
		{
			name: "no mesocycles at all",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Mesocycles = nil
			},
			wantErr: true,
			want:    []string{"mesocycles: none returned"},
		},
		{
			name: "week exceeds the weekly distance cap",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[0].TargetKm = 95
			},
			wantErr: true,
			want:    []string{"exceeds the athlete's weekly distance cap of 90.0 km"},
		},
		{
			name: "volume ramps more than 10 percent",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[5].TargetKm = 80
			},
			wantErr: true,
			want:    []string{"is more than +10% over the previous week's 60.0 km"},
		},
		{
			name: "big jump directly after a deload is allowed",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[4].TargetKm = 90
			},
		},
		{
			name: "A-race week is not a race week",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[MacroBlockWeeks-1].Phase = MacroPhaseTaper
			},
			wantErr: true,
			want:    []string{`phase is "taper", expected "race"`, "contains A-priority race 1"},
		},
		{
			name: "week before the A-race is not a taper week",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[MacroBlockWeeks-2].Phase = MacroPhaseBuild
			},
			wantErr: true,
			want:    []string{`phase is "build", expected "taper"`, "one of the two weeks before A-priority race 1"},
		},
		{
			name: "10k race is given a taper week",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[17].Phase = MacroPhaseTaper
			},
			wantErr: true,
			want:    []string{"never get a taper week"},
		},
		{
			name: "10k tune-up inside the goal race taper is fine",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				// Move the 10k into the second-to-last week, which tapers for
				// the half marathon, not for the 10k.
				in.Races[1].Date = plan.Weeks[MacroBlockWeeks-2].WeekStart
				plan.Weeks[17].RaceID = nil
				plan.Weeks[MacroBlockWeeks-2].RaceID = &in.Races[1].ID
			},
		},
		{
			name: "race already run is ignored",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				result := 4900
				in.Races[0].ResultTime = &result
				plan.Weeks[MacroBlockWeeks-1].Phase = MacroPhasePeak
				// A race with a result is not in the Upcoming Races section,
				// so no week may name it either.
				plan.Weeks[MacroBlockWeeks-1].RaceID = nil
			},
		},
		{
			name: "race outside the block is ignored",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.Races = append(in.Races, Race{
					ID: 3, Name: "Beyond the horizon", Date: "2026-09-05", DistanceM: 21097.5, Priority: "A",
				})
			},
		},
		{
			name: "negative weekly volume",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[6].TargetKm = -5
			},
			wantErr: true,
			want:    []string{"weekly volume cannot be negative"},
		},
		{
			// A 0 km week is legal — nothing in the contract forbids a
			// full-rest week — so the next week cannot be held to +10% of
			// nothing, which no positive volume could ever satisfy.
			name: "week after a zero km week is exempt from the ramp ceiling",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[0].TargetKm = 0
			},
		},
		{
			name: "A race in the first week of the block needs no taper weeks",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				// There is no room for a taper before week 1, so only the
				// race week itself is checked.
				in.Races[0].Date = macroTestRaceDay(0)
				plan.Weeks[0].Phase = MacroPhaseRace
				plan.Weeks[0].RaceID = &in.Races[0].ID
				plan.Weeks[MacroBlockWeeks-1].Phase = MacroPhaseTaper
				plan.Weeks[MacroBlockWeeks-1].RaceID = nil
			},
		},
		{
			name: "A race in the second week tapers only the week that exists",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.Races[0].Date = macroTestRaceDay(1)
				plan.Weeks[0].Phase = MacroPhaseTaper
				plan.Weeks[1].Phase = MacroPhaseRace
				plan.Weeks[1].RaceID = &in.Races[0].ID
				plan.Weeks[MacroBlockWeeks-1].Phase = MacroPhaseTaper
				plan.Weeks[MacroBlockWeeks-1].RaceID = nil
			},
		},
		{
			name: "second A race two weeks before the goal race",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				// The earlier A half owns week 24 as its own race week, so
				// that week is not also required to taper for the goal race —
				// demanding both would make the plan unsatisfiable.
				in.Races = append(in.Races, Race{
					ID: 3, Name: "Sharpener Half", Date: macroTestRaceDay(23),
					DistanceM: 21097.5, Priority: "A",
				})
				plan.Weeks[21].Phase = MacroPhaseTaper
				plan.Weeks[22].Phase = MacroPhaseTaper
				plan.Weeks[23].Phase = MacroPhaseRace
				plan.Weeks[23].RaceID = &in.Races[len(in.Races)-1].ID
			},
		},
		{
			name: "closing weeks taper for an A race just past the horizon",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				// The goal half moves one week past the block's end — still
				// shown to the coach — and a 10 km tune-up takes the final
				// block week. The last two weeks taper for the half, not for
				// the 10 km, so they must not be flagged.
				in.Races[0].Date = macroTestRaceDay(MacroBlockWeeks)
				in.Races[1].Date = macroTestRaceDay(MacroBlockWeeks - 1)
				plan.Weeks[17].RaceID = nil
				plan.Weeks[23].Phase = MacroPhasePeak
				plan.Weeks[MacroBlockWeeks-1].Phase = MacroPhaseTaper
				plan.Weeks[MacroBlockWeeks-1].RaceID = &in.Races[1].ID
			},
		},
		{
			name: "race week does not name the race it contains",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				plan.Weeks[MacroBlockWeeks-1].RaceID = nil
			},
			wantErr: true,
			want:    []string{"week 26 (2026-06-29): race_id is null, expected 1"},
		},
		{
			name: "race id is pinned on a week the race is not in",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				id := in.Races[0].ID
				plan.Weeks[11].RaceID = &id
			},
			wantErr: true,
			want:    []string{"week 12 (2026-03-23): race_id is 1, but race 1 is on 2026-07-04, which is week 26"},
		},
		{
			// race_id is a single value, so a week holding two races can only
			// name one of them. Either answer has to be accepted, or the
			// calendar becomes unplannable.
			name: "two races in one week are satisfied by either id",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.Races = append(in.Races, Race{
					ID: 7, Name: "Sunday 5K", Date: macroTestWeekDate(17, 6),
					DistanceM: 5000, Priority: "C",
				})
			},
		},
		{
			name: "two races in one week are satisfied by the other id too",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.Races = append(in.Races, Race{
					ID: 7, Name: "Sunday 5K", Date: macroTestWeekDate(17, 6),
					DistanceM: 5000, Priority: "C",
				})
				plan.Weeks[17].RaceID = &in.Races[len(in.Races)-1].ID
			},
		},
		{
			name: "week names none of the races it contains",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.Races = append(in.Races, Race{
					ID: 7, Name: "Sunday 5K", Date: macroTestWeekDate(17, 6),
					DistanceM: 5000, Priority: "C",
				})
				plan.Weeks[17].RaceID = nil
			},
			wantErr: true,
			want: []string{
				"week 18 (2026-05-04): race_id is null, expected 2 or 7 — the week contains races 2 on 2026-05-09 and 7 on 2026-05-10",
			},
		},
		{
			// The prompt lists races a few weeks past the end week, so their
			// ids are legal values the model can pin to any week — but no week
			// of the block contains them.
			name: "week names a race past the block's horizon",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				in.Races[0].Date = macroTestRaceDay(MacroBlockWeeks)
				plan.Weeks[23].Phase = MacroPhasePeak
				plan.Weeks[MacroBlockWeeks-1].Phase = MacroPhaseTaper
				plan.Weeks[MacroBlockWeeks-1].RaceID = nil
				plan.Weeks[5].RaceID = &in.Races[0].ID
			},
			wantErr: true,
			want:    []string{"week 6 (2026-02-09): race_id is 1, but no week of this block contains race 1"},
		},
		{
			name: "week names a race that has already been run",
			mutate: func(plan *MacroPlanResponse, in *MacroValidationContext) {
				result := 2400
				in.Races[1].ResultTime = &result
			},
			wantErr: true,
			want:    []string{"week 18 (2026-05-04): race_id is 2, but no week of this block contains race 2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, in := validMacroPlan(t)
			tc.mutate(plan, &in)

			err := ValidateMacroPlan(plan, in)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ValidateMacroPlan() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateMacroPlan() = nil, want an error mentioning %v", tc.want)
			}
			for _, fragment := range tc.want {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("error %q does not contain %q", err, fragment)
				}
			}
		})
	}
}

func TestValidateMacroPlanReportsEveryProblemAtOnce(t *testing.T) {
	plan, in := validMacroPlan(t)
	plan.Weeks[0].Phase = "sharpen"
	plan.Weeks[1].LoadLevel = "cruise"
	plan.Weeks[6].Mesocycle = "Ghost block"

	err := ValidateMacroPlan(plan, in)
	if err == nil {
		t.Fatal("ValidateMacroPlan() = nil, want an aggregated error")
	}
	for _, fragment := range []string{
		"macro plan validation failed (3 problems):",
		`week 1 (2026-01-05): phase "sharpen"`,
		`week 2 (2026-01-12): load_level "cruise"`,
		`week 7 (2026-02-16): mesocycle "Ghost block"`,
	} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q does not contain %q", err, fragment)
		}
	}
}

func TestValidateMacroPlanRejectsUnusableInput(t *testing.T) {
	tests := []struct {
		name      string
		plan      *MacroPlanResponse
		startWeek string
		want      string
	}{
		{name: "nil plan", plan: nil, startWeek: macroTestStartWeek, want: "no plan was returned"},
		{name: "start week is not a Monday", plan: &MacroPlanResponse{}, startWeek: "2026-01-06", want: "block start week is unusable"},
		{name: "start week is not a date", plan: &MacroPlanResponse{}, startWeek: "next monday", want: "block start week is unusable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMacroPlan(tc.plan, MacroValidationContext{StartWeek: tc.startWeek})
			if err == nil {
				t.Fatalf("ValidateMacroPlan() = nil, want an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "(1 problem)") {
				t.Errorf("error %q should report exactly one problem", err)
			}
		})
	}
}

// TestMacroPlanResponseMirrorsOutputContract decodes a response in exactly the
// shape macroOutputContract documents. It is the guard against the Go types and
// the prompt's JSON keys drifting apart.
func TestMacroPlanResponseMirrorsOutputContract(t *testing.T) {
	const raw = `{
	  "goal": {
	    "primary_focus": "half-marathon development",
	    "statement": "Run a personal best half marathon.",
	    "target_hm_time_s": 4920,
	    "benchmark": "Goal half marathon in week 26",
	    "anchor_race_id": 7,
	    "rationale": "Threshold pace supports the target."
	  },
	  "mesocycles": [
	    {"name": "Base 1", "phase": "base", "start_week": "2026-01-05", "weeks": 4, "focus": "Aerobic volume."}
	  ],
	  "weeks": [
	    {
	      "week_start": "2026-01-05",
	      "seq": 1,
	      "phase": "base",
	      "mesocycle": "Base 1",
	      "load_level": "normal",
	      "target_km": 50.5,
	      "target_sessions": 5,
	      "key_sessions": [{"type": "threshold", "focus": "2 mmol/l work", "library_id": 12}],
	      "intent": "Establish the aerobic base.",
	      "race_id": null
	    }
	  ]
	}`

	var plan MacroPlanResponse
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("unmarshal contract sample: %v", err)
	}

	if plan.Goal.TargetHMTimeS != 4920 {
		t.Errorf("goal.target_hm_time_s = %d, want 4920", plan.Goal.TargetHMTimeS)
	}
	if plan.Goal.AnchorRaceID == nil || *plan.Goal.AnchorRaceID != 7 {
		t.Errorf("goal.anchor_race_id = %v, want 7", plan.Goal.AnchorRaceID)
	}
	if len(plan.Mesocycles) != 1 || plan.Mesocycles[0].StartWeek != "2026-01-05" || plan.Mesocycles[0].Weeks != 4 {
		t.Errorf("mesocycles = %+v, want one 4-week mesocycle starting 2026-01-05", plan.Mesocycles)
	}
	if len(plan.Weeks) != 1 {
		t.Fatalf("weeks = %d, want 1", len(plan.Weeks))
	}
	w := plan.Weeks[0]
	if w.WeekStart != "2026-01-05" || w.Seq != 1 || w.Phase != MacroPhaseBase ||
		w.Mesocycle != "Base 1" || w.LoadLevel != LoadLevelNormal ||
		w.TargetKm != 50.5 || w.TargetSessions != 5 || w.Intent == "" || w.RaceID != nil {
		t.Errorf("week = %+v, does not match the contract sample", w)
	}
	if len(w.KeySessions) != 1 || w.KeySessions[0].Type != "threshold" ||
		w.KeySessions[0].LibraryID == nil || *w.KeySessions[0].LibraryID != 12 {
		t.Errorf("key_sessions = %+v, does not match the contract sample", w.KeySessions)
	}
}

// TestMacroFixtureCoversTheWholeBlock guards the fixture itself: a builder that
// silently stopped tiling the block would make every rejection case pass for
// the wrong reason.
func TestMacroFixtureCoversTheWholeBlock(t *testing.T) {
	plan, in := validMacroPlan(t)
	start, err := parseMondayWeek(in.StartWeek)
	if err != nil {
		t.Fatalf("parse start week: %v", err)
	}
	if len(plan.Weeks) != MacroBlockWeeks {
		t.Fatalf("fixture has %d weeks, want %d", len(plan.Weeks), MacroBlockWeeks)
	}
	last := start.AddDate(0, 0, 7*(MacroBlockWeeks-1)).Format(dateLayout)
	if plan.Weeks[MacroBlockWeeks-1].WeekStart != last {
		t.Errorf("last week starts %s, want %s", plan.Weeks[MacroBlockWeeks-1].WeekStart, last)
	}
	if err := ValidateMacroPlan(plan, in); err != nil {
		t.Fatalf("fixture plan must be valid, got: %v", err)
	}
}
