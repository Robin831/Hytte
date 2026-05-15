package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
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
		CardName:        "Pansear",
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

// TestFindScanCandidates_NameRejectsWrongSetMatch exercises the name-as-
// sanity-check path: when Claude's (set_id_hint, collector) tuple would pin
// to a card whose stored name disagrees with what Claude read at the top of
// the card, the worker drops the match instead of silently keeping a wrong
// one. The catalogue's sv1-25 is Pikachu; Claude saying "Charizard" at #25
// of sv1 is a clear set-symbol misread.
func TestFindScanCandidates_NameRejectsWrongSetMatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	result := &claudeScanResult{
		CardName:        "Charizard",
		SetIDHint:       "sv1",
		SetName:         "Scarlet & Violet Base",
		CollectorNumber: "025",
		Confidence:      0.91,
	}
	candidates, reason, err := findScanCandidates(context.Background(), db, u.ID, result)
	if err != nil {
		t.Fatalf("findScanCandidates: %v", err)
	}
	if reason == "" || len(candidates) != 0 {
		t.Fatalf("expected no_match with reason, got candidates=%+v reason=%q", candidates, reason)
	}
	// The reason should surface both the catalogue name (Pikachu) and what
	// Claude said it read, so the user can tell which side is wrong.
	if !strings.Contains(strings.ToLower(reason), "charizard") || !strings.Contains(strings.ToLower(reason), "pikachu") {
		t.Errorf("expected reason to mention both Charizard and Pikachu, got %q", reason)
	}
}

// TestFindScanCandidates_NameTolerantOfSuffix verifies the name check accepts
// minor printed-vs-catalogue differences. Catalogue has "Pikachu V" but
// Claude reads just "Pikachu" (or the other way around) — substring match in
// either direction lets that pass.
func TestFindScanCandidates_NameTolerantOfSuffix(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	cases := []struct {
		name string
		hint string
	}{
		{"Pikachu", "swsh1"},    // catalogue has "Pikachu V"
		{"Pikachu V", "swsh1"},  // exact
		{"Pikachu VMAX", "sv1"}, // catalogue "Pikachu" — Claude added VMAX; "pikachu" still contained
	}
	for _, c := range cases {
		result := &claudeScanResult{
			CardName:        c.name,
			SetIDHint:       c.hint,
			CollectorNumber: "025",
			Confidence:      0.9,
		}
		got, reason, err := findScanCandidates(context.Background(), db, u.ID, result)
		if err != nil {
			t.Fatalf("%q: %v", c.name, err)
		}
		if reason != "" || len(got) != 1 {
			t.Errorf("%q (hint=%s): expected single match, got candidates=%+v reason=%q", c.name, c.hint, got, reason)
		}
	}
}

// TestFindScanCandidates_NameDisambiguatesAcrossSets verifies that when the
// set hint is ambiguous (or absent) and multiple candidates share the same
// collector number, the name reading narrows to the right one. Both sv1-25
// (Pikachu) and swsh1-25 (Pikachu V) match collector 25; Claude reading
// "Pikachu V" should drop sv1-25 because the catalogue's Pikachu doesn't
// contain "Pikachu V" and the substring-either-way check rejects it.
func TestFindScanCandidates_NameDisambiguatesAcrossSets(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	result := &claudeScanResult{
		CardName:        "Pikachu V",
		CollectorNumber: "025",
		Confidence:      0.9,
	}
	got, reason, err := findScanCandidates(context.Background(), db, u.ID, result)
	if err != nil {
		t.Fatalf("findScanCandidates: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected match, got reason=%q", reason)
	}
	if len(got) != 1 || got[0].Card.ID != "swsh1-25" {
		t.Errorf("expected only swsh1-25, got %+v", got)
	}
}

// TestFindScanCandidates_NameNarrowsWhenSetUnknown covers the realistic
// failure mode of last-resort matching: Claude can't read the set symbol or
// denominator clearly (empty set_id_hint, empty set_name, no /n in the
// collector), but can read both the card name and the collector number. The
// name filter narrows the otherwise-cross-set candidate list to the single
// card that actually matches.
func TestFindScanCandidates_NameNarrowsWhenSetUnknown(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	result := &claudeScanResult{
		CardName:        "Pansear",
		CollectorNumber: "21",
		Confidence:      0.88,
	}
	got, reason, err := findScanCandidates(context.Background(), db, u.ID, result)
	if err != nil {
		t.Fatalf("findScanCandidates: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected match, got reason=%q", reason)
	}
	if len(got) != 1 || got[0].Card.ID != "sv7-21" {
		t.Errorf("expected only sv7-21, got %+v", got)
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
