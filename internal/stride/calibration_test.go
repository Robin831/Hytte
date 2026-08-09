package stride

import (
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

func TestRenderTreadmillCalibration_EmptyWhenUnset(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t "} {
		if got := renderTreadmillCalibration(in); got != "" {
			t.Errorf("renderTreadmillCalibration(%q) = %q, want empty", in, got)
		}
	}
}

func TestRenderTreadmillCalibration_MarksNumbersAuthoritative(t *testing.T) {
	const calibration = "Belt sits ~3% below outdoor km/h at matched HR. Indoor HR runs 5-8 bpm high."

	got := renderTreadmillCalibration("  " + calibration + "  ")

	if !strings.HasPrefix(got, treadmillCalibrationHeading) {
		t.Errorf("section should start with %q, got %q", treadmillCalibrationHeading, got)
	}
	if !strings.Contains(got, calibration) {
		t.Error("section should carry the calibration text verbatim")
	}
	// Without these the coach happily recomputes its own offsets, which is the
	// bug this section exists to prevent.
	for _, want := range []string{
		"authoritative",
		"do NOT re-derive or recompute them",
		"watch under-read percentage that is not written here",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("section should contain %q, but does not", want)
		}
	}
}

func TestTreadmillCalibrationFromPrefs(t *testing.T) {
	const plaintext = "Belt sits ~3% below outdoor km/h at matched HR."
	enc, err := encryption.EncryptField(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	tests := []struct {
		name  string
		prefs map[string]string
		want  string
	}{
		{"unset", map[string]string{}, ""},
		{"empty", map[string]string{treadmillCalibrationPref: ""}, ""},
		{"encrypted", map[string]string{treadmillCalibrationPref: enc}, plaintext},
		// An unprefixed value is legacy plaintext, not a decrypt failure — it is
		// returned as-is so a pre-encryption value still reaches the prompt.
		{"legacy plaintext", map[string]string{treadmillCalibrationPref: plaintext}, plaintext},
		// Corrupt ciphertext must degrade to "no calibration" rather than leaking
		// an unreadable blob into the coaching prompt.
		{"corrupt ciphertext", map[string]string{treadmillCalibrationPref: "enc:not-real-ciphertext"}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := treadmillCalibrationFromPrefs(tc.prefs); got != tc.want {
				t.Errorf("treadmillCalibrationFromPrefs() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Round-trip through the DB: what the settings API persists is what the
// coaching prompts read back.
func TestLoadTreadmillCalibration_RoundTrip(t *testing.T) {
	db := setupTestDB(t)
	const plaintext = "Belt sits ~3% below outdoor km/h at matched HR."

	if got := loadTreadmillCalibration(db, 1); got != "" {
		t.Errorf("expected empty calibration before any is stored, got %q", got)
	}

	enc, err := encryption.EncryptField(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := auth.SetPreference(db, 1, treadmillCalibrationPref, enc); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	if got := loadTreadmillCalibration(db, 1); got != plaintext {
		t.Errorf("loadTreadmillCalibration() = %q, want %q", got, plaintext)
	}
}
