package math

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// TestRegistryHasAllExpectedCodes guards the registry shape against
// accidental edits — every code listed in the bead must remain present.
func TestRegistryHasAllExpectedCodes(t *testing.T) {
	want := []string{
		AchMarathonSub10, AchMarathonSub7, AchMarathonSub5, AchMarathonSub4, AchMarathonSub3,
		AchMarathonPerfect,
		AchStreak25, AchStreak50, AchStreak100,
		AchFirstBlood,
	}
	got := map[string]bool{}
	for _, a := range Registry() {
		got[a.Code] = true
		if a.Tier == "" {
			t.Errorf("achievement %s: tier is empty", a.Code)
		}
		if a.Check == nil {
			t.Errorf("achievement %s: Check is nil", a.Code)
		}
	}
	for _, code := range want {
		if !got[code] {
			t.Errorf("registry missing code %q", code)
		}
	}
	if !IsValidAchievementCode(AchMarathonSub5) {
		t.Error("IsValidAchievementCode should accept registered codes")
	}
	if IsValidAchievementCode("nope") {
		t.Error("IsValidAchievementCode should reject unknown codes")
	}
}

func TestMarathonSubCheck(t *testing.T) {
	// 4:59 marathon (full attempt count, zero wrongs) clears Sub-5 but not Sub-3.
	full := MarathonFactCount
	cases := []struct {
		name      string
		summary   Summary
		threshold int64
		want      bool
	}{
		{
			name:      "below threshold",
			summary:   Summary{Mode: ModeMarathon, DurationMs: 4*60*1000 + 59*1000, TotalCorrect: full, TotalWrong: 0},
			threshold: 5 * 60 * 1000,
			want:      true,
		},
		{
			name:      "at threshold rejects",
			summary:   Summary{Mode: ModeMarathon, DurationMs: 5 * 60 * 1000, TotalCorrect: full, TotalWrong: 0},
			threshold: 5 * 60 * 1000,
			want:      false,
		},
		{
			name:      "wrong mode",
			summary:   Summary{Mode: ModeBlitz, DurationMs: 30 * 1000, TotalCorrect: 50, TotalWrong: 0},
			threshold: 5 * 60 * 1000,
			want:      false,
		},
		{
			name:      "partial run",
			summary:   Summary{Mode: ModeMarathon, DurationMs: 60 * 1000, TotalCorrect: 50, TotalWrong: 0},
			threshold: 5 * 60 * 1000,
			want:      false,
		},
		{
			name:      "zero duration",
			summary:   Summary{Mode: ModeMarathon, DurationMs: 0, TotalCorrect: full, TotalWrong: 0},
			threshold: 5 * 60 * 1000,
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := marathonSubCheck(tc.threshold)
			s := tc.summary
			got := fn(CheckContext{Session: &s})
			if got != tc.want {
				t.Errorf("marathonSubCheck(%d) = %v, want %v", tc.threshold, got, tc.want)
			}
		})
	}
}

func TestMarathonPerfectCheck(t *testing.T) {
	full := MarathonFactCount
	if !marathonPerfectCheck(CheckContext{Session: &Summary{
		Mode: ModeMarathon, TotalCorrect: full, TotalWrong: 0, DurationMs: 60 * 1000,
	}}) {
		t.Error("flawless full marathon should unlock perfect")
	}
	if marathonPerfectCheck(CheckContext{Session: &Summary{
		Mode: ModeMarathon, TotalCorrect: full - 1, TotalWrong: 1, DurationMs: 60 * 1000,
	}}) {
		t.Error("marathon with wrongs should not unlock perfect")
	}
	if marathonPerfectCheck(CheckContext{Session: &Summary{
		Mode: ModeMarathon, TotalCorrect: 50, TotalWrong: 0, DurationMs: 60 * 1000,
	}}) {
		t.Error("partial marathon should not unlock perfect")
	}
	if marathonPerfectCheck(CheckContext{Session: &Summary{
		Mode: ModeBlitz, TotalCorrect: 100, TotalWrong: 0, DurationMs: 60 * 1000,
	}}) {
		t.Error("blitz should not unlock marathon perfect")
	}
}

func TestStreakCheck(t *testing.T) {
	fn := streakCheck(50)
	if !fn(CheckContext{Session: &Summary{Mode: ModeBlitz, BestStreak: 50}}) {
		t.Error("streak 50 in Blitz should unlock streak_50")
	}
	if fn(CheckContext{Session: &Summary{Mode: ModeBlitz, BestStreak: 49}}) {
		t.Error("streak 49 should not unlock streak_50")
	}
	if fn(CheckContext{Session: &Summary{Mode: ModeMarathon, BestStreak: 200}}) {
		t.Error("streak in non-Blitz mode should not unlock")
	}
}

func TestFirstBloodCheck(t *testing.T) {
	if !firstBloodCheck(CheckContext{Session: &Summary{}, UserStats: UserAchievementStats{OnTopAnyBoard: true}}) {
		t.Error("user on top of any board should unlock first_blood")
	}
	if firstBloodCheck(CheckContext{Session: &Summary{}, UserStats: UserAchievementStats{OnTopAnyBoard: false}}) {
		t.Error("user not on top should not unlock first_blood")
	}
	if firstBloodCheck(CheckContext{Session: nil, UserStats: UserAchievementStats{OnTopAnyBoard: true}}) {
		t.Error("first_blood requires a session context")
	}
}

func TestEvaluateAchievementsInsertsNewlyEarned(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	// Insert a real session so the FK on math_achievements.session_id holds.
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	sid := finishedMarathon(t, d, 1, 4*60*1000+30*1000, 0, start)
	// Sub-10 (and Sub-7 / Sub-5) marathon: 4:30 with zero wrongs.
	summary := Summary{
		SessionID:    sid,
		Mode:         ModeMarathon,
		DurationMs:   4*60*1000 + 30*1000,
		TotalCorrect: MarathonFactCount,
		TotalWrong:   0,
	}
	unlocked, err := svc.EvaluateAchievements(ctx, 1, summary)
	if err != nil {
		t.Fatalf("EvaluateAchievements: %v", err)
	}
	codes := map[string]bool{}
	for _, u := range unlocked {
		codes[u.Code] = true
	}
	for _, want := range []string{AchMarathonSub10, AchMarathonSub7, AchMarathonSub5, AchMarathonPerfect} {
		if !codes[want] {
			t.Errorf("expected %s to unlock, got %v", want, codes)
		}
	}
	if codes[AchMarathonSub4] || codes[AchMarathonSub3] {
		t.Errorf("4:30 should not unlock Sub-4 or Sub-3, got %v", codes)
	}

	// Re-evaluating the same session should produce no new unlocks.
	again, err := svc.EvaluateAchievements(ctx, 1, summary)
	if err != nil {
		t.Fatalf("second EvaluateAchievements: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second pass should be empty, got %v", again)
	}
}

func TestEvaluateAchievementsUniqueConstraint(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	// Two Blitz sessions both reach a streak of 30 — only the first should
	// insert streak_25.
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	sid1 := finishedBlitz(t, d, 1, 60, start)
	sid2 := finishedBlitz(t, d, 1, 60, start.Add(time.Hour))
	summary1 := Summary{SessionID: sid1, Mode: ModeBlitz, BestStreak: 30, DurationMs: 60_000}
	summary2 := Summary{SessionID: sid2, Mode: ModeBlitz, BestStreak: 30, DurationMs: 60_000}
	unlocked1, err := svc.EvaluateAchievements(ctx, 1, summary1)
	if err != nil {
		t.Fatalf("Evaluate 1: %v", err)
	}
	if !containsCode(unlocked1, AchStreak25) {
		t.Errorf("first session should unlock streak_25, got %v", unlocked1)
	}
	unlocked2, err := svc.EvaluateAchievements(ctx, 1, summary2)
	if err != nil {
		t.Fatalf("Evaluate 2: %v", err)
	}
	if containsCode(unlocked2, AchStreak25) {
		t.Errorf("second session should not re-unlock streak_25, got %v", unlocked2)
	}

	// Verify only one row in math_achievements for streak_25.
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM math_achievements WHERE user_id = ? AND code = ?`, 1, AchStreak25).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row for streak_25, got %d", count)
	}
}

func TestUserOnTopOfAnyBoardSoloDoesNotCount(t *testing.T) {
	// A user with no family links (ranked #1 of a one-row board) must not
	// trigger first_blood — that rivalry milestone requires actually beating
	// somebody. The leaderboard returns the caller alone with rank=1; the
	// helper has to filter that case out.
	d := setupTestDB(t)
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	finishedMarathon(t, d, 1, 200_000, 0, start)
	finishedBlitz(t, d, 1, 50, start)

	svc := NewService(d)
	top, err := svc.userOnTopOfAnyBoard(context.Background(), 1)
	if err != nil {
		t.Fatalf("userOnTopOfAnyBoard: %v", err)
	}
	if top {
		t.Error("solo user should not count as #1 of any leaderboard")
	}
}

func TestUserOnTopOfAnyBoardWhenBeatingFamily(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	// Both run a marathon; child is faster.
	finishedMarathon(t, d, 1, 300_000, 1, start)
	finishedMarathon(t, d, 2, 200_000, 0, start)

	svc := NewService(d)
	topChild, err := svc.userOnTopOfAnyBoard(context.Background(), 2)
	if err != nil {
		t.Fatalf("userOnTopOfAnyBoard child: %v", err)
	}
	if !topChild {
		t.Error("child with fastest time should be #1 on marathon/all")
	}
	topParent, err := svc.userOnTopOfAnyBoard(context.Background(), 1)
	if err != nil {
		t.Fatalf("userOnTopOfAnyBoard parent: %v", err)
	}
	if topParent {
		t.Error("parent should not be #1 when child is faster")
	}
}

func TestEvaluateAchievementsFirstBloodUnlocks(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	// Parent posts a slower marathon; child posts a faster one and is now #1.
	finishedMarathon(t, d, 1, 300_000, 1, start)
	childSess := finishedMarathon(t, d, 2, 200_000, 0, start)

	svc := NewService(d)
	summary := Summary{
		SessionID:    childSess,
		Mode:         ModeMarathon,
		DurationMs:   200_000,
		TotalCorrect: MarathonFactCount,
		TotalWrong:   0,
	}
	unlocked, err := svc.EvaluateAchievements(context.Background(), 2, summary)
	if err != nil {
		t.Fatalf("EvaluateAchievements: %v", err)
	}
	if !containsCode(unlocked, AchFirstBlood) {
		t.Errorf("child should unlock first_blood, got %v", unlocked)
	}
}

func TestListAchievementsPartitionsEarnedAndLocked(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()
	// Pre-seed one earned achievement directly.
	if _, err := d.Exec(`INSERT INTO math_achievements (user_id, code, unlocked_at, session_id) VALUES (?, ?, ?, NULL)`,
		1, AchMarathonSub10, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	resp, err := svc.ListAchievements(ctx, 1)
	if err != nil {
		t.Fatalf("ListAchievements: %v", err)
	}
	if len(resp.Earned) != 1 || resp.Earned[0].Code != AchMarathonSub10 {
		t.Errorf("expected single earned Sub-10 row, got %+v", resp.Earned)
	}
	if resp.Earned[0].Title == "" || resp.Earned[0].Description == "" || resp.Earned[0].Tier != TierMarathon {
		t.Errorf("earned row should be enriched with metadata, got %+v", resp.Earned[0])
	}
	// Locked count should be Registry size minus the one earned.
	if want := len(Registry()) - 1; len(resp.Locked) != want {
		t.Errorf("locked count = %d, want %d", len(resp.Locked), want)
	}
	for _, l := range resp.Locked {
		if l.Code == AchMarathonSub10 {
			t.Errorf("Sub-10 should not appear in Locked: %+v", l)
		}
		if l.Tier == "" {
			t.Errorf("locked row missing tier: %+v", l)
		}
	}
}

func TestAchievementsHandlerReturnsStructure(t *testing.T) {
	d := setupTestDB(t)
	h := AchievementsHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/achievements", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp AchievementsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Earned) != 0 {
		t.Errorf("expected no earned with empty DB, got %v", resp.Earned)
	}
	if len(resp.Locked) != len(Registry()) {
		t.Errorf("locked = %d, want %d", len(resp.Locked), len(Registry()))
	}
	if resp.UserStats.HasMarathon || resp.UserStats.HasBlitz {
		t.Errorf("expected empty user_stats, got %+v", resp.UserStats)
	}
}

func TestAchievementsHandlerUnauthorized(t *testing.T) {
	d := setupTestDB(t)
	h := AchievementsHandler(d)
	r := httptest.NewRequest(http.MethodGet, "/api/math/achievements", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestFinishHandlerReturnsUnlockedAchievements(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	ctx := context.Background()

	id, _, err := svc.Start(ctx, 1, ModeBlitz)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Record a long correct streak — 30 fast-correct attempts to clear streak_25.
	for i := 0; i < 30; i++ {
		if _, _, _, err := svc.RecordAttempt(ctx, id, 1, 3, 4, OpMultiply, 12, 400); err != nil {
			t.Fatalf("RecordAttempt %d: %v", i, err)
		}
	}

	h := FinishSessionHandler(d)
	r := withChi(withUser(newJSONRequest(t, http.MethodPost, "/api/math/sessions/"+strconv.FormatInt(id, 10)+"/finish", nil), testUser), map[string]string{"id": strconv.FormatInt(id, 10)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Summary              Summary                `json:"summary"`
		UnlockedAchievements []EarnedAchievementRow `json:"unlocked_achievements"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !containsCode(resp.UnlockedAchievements, AchStreak25) {
		t.Errorf("expected streak_25 in unlocked, got %v", resp.UnlockedAchievements)
	}
	if resp.Summary.BestStreak != 30 {
		t.Errorf("BestStreak=%d, want 30", resp.Summary.BestStreak)
	}
}

// containsCode is a tiny helper for checking whether a slice of unlocked
// rows includes a particular achievement code.
func containsCode(rows []EarnedAchievementRow, code string) bool {
	for _, r := range rows {
		if r.Code == code {
			return true
		}
	}
	return false
}
