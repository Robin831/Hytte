package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// jpegMagic is the leading marker bytes for image/jpeg. Shared with
// scans_handlers_test.go so the queue handler's MIME-sniffing path can be
// exercised without a real JPEG file on disk.
var jpegMagic = []byte{0xFF, 0xD8, 0xFF, 0xE0}

// enableClaudeForUser flips the per-user claude flag so the scan worker does
// not bail with "claude is not enabled". The CLI path stays "claude" so the
// real binary is never invoked — the test seam intercepts the call first.
func enableClaudeForUser(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	if err := auth.SetPreference(db, userID, "claude_enabled", "true"); err != nil {
		t.Fatalf("enable claude: %v", err)
	}
}

// stubScanPrompt replaces scanRunPromptFn with a fixed-response stub and
// restores the original on cleanup. The returned counter tracks how many
// times the stub was invoked, which lets tests assert that the DB lookup is
// skipped when confidence is below the threshold.
func stubScanPrompt(t *testing.T, response string, stubErr error) *atomic.Int32 {
	t.Helper()
	orig := scanRunPromptFn
	calls := new(atomic.Int32)
	scanRunPromptFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string) (string, error) {
		calls.Add(1)
		return response, stubErr
	}
	t.Cleanup(func() { scanRunPromptFn = orig })
	return calls
}

// TestParseClaudeScanResult exercises the JSON parser directly against the
// payload variants we expect to see in production: a strict object, an
// accidental markdown fence, and a malformed string.
func TestParseClaudeScanResult(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
		conf    float64
	}{
		{
			name:    "strict json",
			raw:     `{"set_name":"x","set_id_hint":"sv1","collector_number":"025","confidence":0.9}`,
			wantErr: false,
			conf:    0.9,
		},
		{
			name:    "code fence",
			raw:     "```json\n{\"set_name\":\"x\",\"set_id_hint\":\"sv1\",\"collector_number\":\"025\",\"confidence\":0.5}\n```",
			wantErr: false,
			conf:    0.5,
		},
		{
			name:    "garbage",
			raw:     "i give up",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseClaudeScanResult(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", r)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Confidence != tc.conf {
				t.Errorf("expected confidence %v, got %v", tc.conf, r.Confidence)
			}
		})
	}
}

// TestFindScanCandidates_PrintedTotalOverridesWrongSetHint exercises the
// disambiguation path that was the original motivation for the bead. Claude
// vision sometimes mis-identifies the set (returning sv1 / "Scarlet & Violet"
// when looking at a Stellar Crown card), but the printed "n/142" on the card
// face is reliable. The denominator is intersected with the set hint, so a
// hint that disagrees with the denominator gets rejected and the search falls
// back to whichever sets actually have printed_total=142.
func TestFindScanCandidates_PrintedTotalOverridesWrongSetHint(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	result := &claudeScanResult{
		SetIDHint:       "sv1",
		SetName:         "Scarlet & Violet",
		CollectorNumber: "021/142",
		Confidence:      0.96,
	}
	candidates, reason, err := findScanCandidates(context.Background(), db, u.ID, result)
	if err != nil {
		t.Fatalf("findScanCandidates: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected match, got reason=%q", reason)
	}
	if len(candidates) != 1 || candidates[0].Card.ID != "sv7-21" {
		t.Fatalf("expected single sv7-21 candidate, got %+v", candidates)
	}
}

// TestScanCandidate_JSON verifies ScanCandidate round-trips through JSON
// symmetrically. A change to any exported field name or type would break this
// check and signal an API shape regression.
func TestScanCandidate_JSON(t *testing.T) {
	c := ScanCandidate{
		Card:  CardDTO{ID: "sv1-25", SetID: "sv1", Name: "Pikachu", Variants: []VariantDTO{}},
		Set:   &SetDTO{ID: "sv1", Name: "SV Base"},
		Score: 0.91,
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ScanCandidate
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Card.ID != c.Card.ID || got.Score != c.Score || got.Set == nil || got.Set.ID != c.Set.ID {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, c)
	}
}
