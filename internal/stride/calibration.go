package stride

import (
	"database/sql"
	"log"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// treadmillCalibrationPref is the user_preferences key holding the athlete's own
// measured treadmill calibration: belt-to-outdoor speed offset, indoor HR offset,
// and any other figure that would otherwise have to be re-derived from scratch
// every time a coaching prompt is built. Persisting it is the point — without it
// the coach re-invents these numbers each week and can state a confident figure
// that the athlete's own data contradicts. Stored encrypted at rest, like
// stride_custom_prompt.
const treadmillCalibrationPref = "stride_treadmill_calibration"

// treadmillCalibrationHeading is the prompt section header. Prompt text refers to
// it by name so the model knows which section overrides the generic defaults.
const treadmillCalibrationHeading = "## Treadmill Calibration (athlete-measured)"

// treadmillCalibrationFromPrefs pulls the calibration out of an already-loaded
// preferences map and decrypts it. An unprefixed value is legacy plaintext and
// is returned as-is. Corrupt ciphertext is logged and treated as "no calibration
// set" rather than failing the caller: the prompt still works without it, it
// just falls back to the generic guidance.
func treadmillCalibrationFromPrefs(prefs map[string]string) string {
	raw := prefs[treadmillCalibrationPref]
	if raw == "" {
		return ""
	}
	decrypted, err := encryption.DecryptField(raw)
	if err != nil {
		log.Printf("stride: failed to decrypt %s, skipping: %v", treadmillCalibrationPref, err)
		return ""
	}
	return strings.TrimSpace(decrypted)
}

// loadTreadmillCalibration reads the persisted treadmill calibration for a user.
// Non-fatal: a load error is logged and yields an empty string.
func loadTreadmillCalibration(db *sql.DB, userID int64) string {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		log.Printf("stride: load preferences for user %d: %v", userID, err)
		return ""
	}
	return treadmillCalibrationFromPrefs(prefs)
}

// renderTreadmillCalibration returns the prompt section carrying the athlete's
// persisted calibration, or "" when none is set. The preamble is what stops the
// coach re-deriving the numbers: it marks the athlete's own measurements as
// authoritative and forbids substituting or inventing a different figure.
func renderTreadmillCalibration(calibration string) string {
	calibration = strings.TrimSpace(calibration)
	if calibration == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(treadmillCalibrationHeading + "\n")
	sb.WriteString("These figures were measured from this athlete's own matched-HR indoor and outdoor sessions and are persisted between weeks. They are authoritative. Use them exactly as written, do NOT re-derive or recompute them from the data in this prompt, do NOT override them with the generic starting estimates, and do NOT state any belt-speed offset or watch under-read percentage that is not written here.\n\n")
	sb.WriteString(calibration)
	sb.WriteString("\n\n")
	return sb.String()
}
