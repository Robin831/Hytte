package stride

import "strings"

// The types in this file mirror the JSON contract in macroOutputContract
// (macro_prompt.go) field for field: same keys, same order, same nullability.
// The contract is the single source of truth for the shape — change the two
// together, and never rename a json tag here without renaming the key there.
//
// They are deliberately separate from the persisted MacroPlan / MacroWeek in
// macro.go. The coach never authors ids, ownership, status or timestamps, so
// those columns must not be readable off its output; the goal, mesocycle and
// key-session shapes are identical in both directions, so those three types
// are shared rather than duplicated.

// MacroPlanResponse is the whole JSON object the macro coach returns for a
// block: the goal it is built around, its periodisation, and one entry per
// week of the MacroBlockWeeks-week horizon.
type MacroPlanResponse struct {
	Goal       MacroGoal           `json:"goal"`
	Mesocycles []Mesocycle         `json:"mesocycles"`
	Weeks      []MacroWeekResponse `json:"weeks"`
}

// MacroWeekResponse is one planned week as the coach returns it — the
// persisted MacroWeek minus every field the store owns.
type MacroWeekResponse struct {
	WeekStart      string       `json:"week_start"` // YYYY-MM-DD, Monday
	Seq            int          `json:"seq"`        // 1-based position within the block
	Phase          string       `json:"phase"`
	Mesocycle      string       `json:"mesocycle"` // Name of the mesocycle this week belongs to
	LoadLevel      string       `json:"load_level"`
	TargetKm       float64      `json:"target_km"`
	TargetSessions int          `json:"target_sessions"`
	KeySessions    []KeySession `json:"key_sessions"`
	Intent         string       `json:"intent"`
	RaceID         *int64       `json:"race_id"`
}

// macroPhaseValues and macroLoadLevelValues are the enums a returned week (and
// a mesocycle, for the phase) has to draw from, in block order. They back both
// the membership checks and the "not one of ..." text the validator hands back
// to the model, so the allowed set is written down once.
var (
	macroPhaseValues = []string{
		MacroPhaseBase,
		MacroPhaseBuild,
		MacroPhasePeak,
		MacroPhaseTaper,
		MacroPhaseRace,
		MacroPhaseRecovery,
	}
	macroLoadLevelValues = []string{
		LoadLevelDeload,
		LoadLevelNormal,
		LoadLevelBuild,
		LoadLevelPeak,
		LoadLevelTaper,
	}
)

// isMacroPhase reports whether phase is one of the allowed macro phases.
func isMacroPhase(phase string) bool {
	return containsString(macroPhaseValues, phase)
}

// isMacroLoadLevel reports whether load is one of the allowed macro week load
// levels. Unlike the store's column check, an empty value is not accepted: the
// coach must state the load of every week it plans.
func isMacroLoadLevel(load string) bool {
	return containsString(macroLoadLevelValues, load)
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// joinQuoted renders an enum as `"base", "build", ...` for an error message.
func joinQuoted(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = `"` + v + `"`
	}
	return strings.Join(quoted, ", ")
}
