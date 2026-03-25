package family

import (
	"database/sql"
	"sync"
	"testing"
)

// setupRewardsTestDB extends setupTestDB with the family_rewards and
// reward_claims tables needed for reward/claim tests.
func setupRewardsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	schema := `
	CREATE TABLE IF NOT EXISTS family_rewards (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title        TEXT NOT NULL DEFAULT '',
		description  TEXT NOT NULL DEFAULT '',
		star_cost    INTEGER NOT NULL DEFAULT 0,
		icon_emoji   TEXT NOT NULL DEFAULT '🎁',
		is_active    INTEGER NOT NULL DEFAULT 1,
		max_claims   INTEGER,
		parent_note  TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT '',
		updated_at   TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_family_rewards_parent ON family_rewards(parent_id);

	CREATE TABLE IF NOT EXISTS reward_claims (
		id          INTEGER PRIMARY KEY,
		reward_id   INTEGER NOT NULL REFERENCES family_rewards(id) ON DELETE CASCADE,
		child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		status      TEXT NOT NULL DEFAULT 'pending',
		stars_spent INTEGER NOT NULL DEFAULT 0,
		note        TEXT NOT NULL DEFAULT '',
		resolved_at TEXT,
		created_at  TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_reward_claims_reward ON reward_claims(reward_id);
	CREATE INDEX IF NOT EXISTS idx_reward_claims_child ON reward_claims(child_id);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create rewards schema: %v", err)
	}

	// Give child a star balance (100 stars).
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned, total_spent) VALUES (2, 100, 0)
	`); err != nil {
		t.Fatalf("seed star balance: %v", err)
	}

	return db
}

// linkFamilies creates a parent→child link in the DB (user 1 → user 2).
func linkFamilies(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
}

// TestCreateReward verifies a reward is stored and returned with plaintext fields.
func TestCreateReward(t *testing.T) {
	db := setupRewardsTestDB(t)

	r, err := CreateReward(db, 1, "Ice Cream", "A scoop of ice cream", "🍦", "", 10, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	if r.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if r.Title != "Ice Cream" {
		t.Errorf("title: got %q want %q", r.Title, "Ice Cream")
	}
	if r.StarCost != 10 {
		t.Errorf("star_cost: got %d want 10", r.StarCost)
	}
	if !r.IsActive {
		t.Error("expected is_active=true")
	}
	if r.MaxClaims != nil {
		t.Errorf("expected nil max_claims, got %v", r.MaxClaims)
	}
}

// TestCreateRewardTitleEncrypted verifies the title is stored encrypted in the DB.
func TestCreateRewardTitleEncrypted(t *testing.T) {
	db := setupRewardsTestDB(t)

	if _, err := CreateReward(db, 1, "Secret Prize", "", "🎁", "", 5, true, nil); err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	var rawTitle string
	if err := db.QueryRow(`SELECT title FROM family_rewards WHERE parent_id = 1`).Scan(&rawTitle); err != nil {
		t.Fatalf("scan title: %v", err)
	}
	if len(rawTitle) < 4 || rawTitle[:4] != "enc:" {
		t.Errorf("expected title to be encrypted in DB, got %q", rawTitle[:min(len(rawTitle), 20)])
	}
}

// TestGetRewards verifies that all parent rewards are returned with decrypted fields.
func TestGetRewards(t *testing.T) {
	db := setupRewardsTestDB(t)

	if _, err := CreateReward(db, 1, "Movie Night", "", "🎬", "private note", 20, true, nil); err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	if _, err := CreateReward(db, 1, "Extra Screen Time", "", "📱", "", 15, false, nil); err != nil {
		t.Fatalf("CreateReward 2: %v", err)
	}

	rewards, err := GetRewards(db, 1)
	if err != nil {
		t.Fatalf("GetRewards: %v", err)
	}
	if len(rewards) != 2 {
		t.Fatalf("expected 2 rewards, got %d", len(rewards))
	}
	if rewards[0].Title != "Movie Night" {
		t.Errorf("unexpected title: %q", rewards[0].Title)
	}
	if rewards[0].ParentNote != "private note" {
		t.Errorf("expected parent_note returned by GetRewards, got %q", rewards[0].ParentNote)
	}
}

// TestGetRewardsEmpty verifies an empty slice is returned when there are no rewards.
func TestGetRewardsEmpty(t *testing.T) {
	db := setupRewardsTestDB(t)

	rewards, err := GetRewards(db, 1)
	if err != nil {
		t.Fatalf("GetRewards: %v", err)
	}
	if len(rewards) != 0 {
		t.Errorf("expected 0 rewards, got %d", len(rewards))
	}
}

// TestGetActiveRewards verifies only active rewards are returned and parent_note is absent.
func TestGetActiveRewards(t *testing.T) {
	db := setupRewardsTestDB(t)

	if _, err := CreateReward(db, 1, "Active Reward", "", "✅", "note", 5, true, nil); err != nil {
		t.Fatalf("CreateReward active: %v", err)
	}
	if _, err := CreateReward(db, 1, "Inactive Reward", "", "❌", "", 5, false, nil); err != nil {
		t.Fatalf("CreateReward inactive: %v", err)
	}

	rewards, err := GetActiveRewards(db, 1)
	if err != nil {
		t.Fatalf("GetActiveRewards: %v", err)
	}
	if len(rewards) != 1 {
		t.Fatalf("expected 1 active reward, got %d", len(rewards))
	}
	if rewards[0].Title != "Active Reward" {
		t.Errorf("unexpected title: %q", rewards[0].Title)
	}
	// parent_note must not be returned to kid-facing view.
	if rewards[0].ParentNote != "" {
		t.Errorf("expected empty parent_note for kid view, got %q", rewards[0].ParentNote)
	}
}

// TestGetRewardByID verifies ownership check: wrong parent gets ErrRewardNotFound.
func TestGetRewardByID(t *testing.T) {
	db := setupRewardsTestDB(t)

	// Insert a second parent.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'other@test.com', 'Other', 'g3')`); err != nil {
		t.Fatalf("insert other parent: %v", err)
	}

	r, err := CreateReward(db, 1, "Prize", "", "🎁", "", 10, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	// Correct owner — should succeed.
	got, err := GetRewardByID(db, r.ID, 1)
	if err != nil {
		t.Fatalf("GetRewardByID (correct owner): %v", err)
	}
	if got.Title != "Prize" {
		t.Errorf("unexpected title: %q", got.Title)
	}

	// Wrong owner — should return ErrRewardNotFound.
	_, err = GetRewardByID(db, r.ID, 3)
	if !isErr(err, ErrRewardNotFound) {
		t.Errorf("expected ErrRewardNotFound for wrong owner, got %v", err)
	}
}

// TestUpdateReward verifies fields are updated and ownership is enforced.
func TestUpdateReward(t *testing.T) {
	db := setupRewardsTestDB(t)

	r, err := CreateReward(db, 1, "Old Title", "Old desc", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	updated, err := UpdateReward(db, r.ID, 1, "New Title", "New desc", "🏆", "note", 20, false, nil)
	if err != nil {
		t.Fatalf("UpdateReward: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", updated.Title)
	}
	if updated.StarCost != 20 {
		t.Errorf("expected star_cost 20, got %d", updated.StarCost)
	}
	if updated.IsActive {
		t.Error("expected is_active=false after update")
	}
}

// TestUpdateRewardNotFound verifies ErrRewardNotFound for missing or wrong-owner rewards.
func TestUpdateRewardNotFound(t *testing.T) {
	db := setupRewardsTestDB(t)

	_, err := UpdateReward(db, 999, 1, "Title", "", "🎁", "", 5, true, nil)
	if !isErr(err, ErrRewardNotFound) {
		t.Errorf("expected ErrRewardNotFound, got %v", err)
	}
}

// TestDeleteReward verifies a reward can be deleted by its owner.
func TestDeleteReward(t *testing.T) {
	db := setupRewardsTestDB(t)

	r, err := CreateReward(db, 1, "Temp", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	if err := DeleteReward(db, r.ID, 1); err != nil {
		t.Fatalf("DeleteReward: %v", err)
	}

	rewards, err := GetRewards(db, 1)
	if err != nil {
		t.Fatalf("GetRewards after delete: %v", err)
	}
	if len(rewards) != 0 {
		t.Errorf("expected 0 rewards after delete, got %d", len(rewards))
	}
}

// TestDeleteRewardNotFound verifies ErrRewardNotFound for missing rewards.
func TestDeleteRewardNotFound(t *testing.T) {
	db := setupRewardsTestDB(t)

	err := DeleteReward(db, 999, 1)
	if !isErr(err, ErrRewardNotFound) {
		t.Errorf("expected ErrRewardNotFound, got %v", err)
	}
}

// TestClaimReward verifies stars are deducted and a pending claim is created.
func TestClaimReward(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Candy", "", "🍬", "", 10, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	claim, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	if claim.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", claim.Status)
	}
	if claim.StarsSpent != 10 {
		t.Errorf("expected stars_spent=10, got %d", claim.StarsSpent)
	}

	// Verify balance was reduced.
	var balance int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = 2`).Scan(&balance); err != nil {
		t.Fatalf("scan balance: %v", err)
	}
	if balance != 90 {
		t.Errorf("expected balance 90, got %d", balance)
	}
}

// TestClaimRewardInsufficientStars verifies ErrInsufficientStars is returned.
func TestClaimRewardInsufficientStars(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Expensive", "", "💎", "", 999, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	_, err = ClaimReward(db, 2, r.ID)
	if !isErr(err, ErrInsufficientStars) {
		t.Errorf("expected ErrInsufficientStars, got %v", err)
	}
}

// TestClaimRewardNotActive verifies ErrRewardNotActive is returned.
func TestClaimRewardNotActive(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Disabled", "", "🚫", "", 5, false, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	_, err = ClaimReward(db, 2, r.ID)
	if !isErr(err, ErrRewardNotActive) {
		t.Errorf("expected ErrRewardNotActive, got %v", err)
	}
}

// TestClaimRewardCrossFamilyBlocked verifies a child cannot claim a reward from a different family.
func TestClaimRewardCrossFamilyBlocked(t *testing.T) {
	db := setupRewardsTestDB(t)

	// Insert a second parent (user 3) with its own reward.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'parent2@test.com', 'Parent2', 'g3')`); err != nil {
		t.Fatalf("insert parent2: %v", err)
	}
	// Child (user 2) is NOT linked to parent 3 — no family_links row.
	r, err := CreateReward(db, 3, "Foreign Reward", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward for parent2: %v", err)
	}

	// Child has stars but is not in the same family.
	_, err = ClaimReward(db, 2, r.ID)
	if !isErr(err, ErrRewardNotFound) {
		t.Errorf("expected ErrRewardNotFound for cross-family claim, got %v", err)
	}
}

// TestClaimRewardMaxClaimsReached verifies ErrMaxClaimsReached is returned.
func TestClaimRewardMaxClaimsReached(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	maxOne := 1
	r, err := CreateReward(db, 1, "Limited", "", "🏅", "", 5, true, &maxOne)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	// First claim succeeds.
	if _, err := ClaimReward(db, 2, r.ID); err != nil {
		t.Fatalf("first ClaimReward: %v", err)
	}

	// Second claim should fail — but child needs more balance first.
	// Give more stars so the limit (not balance) is the failure.
	if _, err := db.Exec(`UPDATE star_balances SET total_earned = total_earned + 100 WHERE user_id = 2`); err != nil {
		t.Fatalf("top up stars: %v", err)
	}

	_, err = ClaimReward(db, 2, r.ID)
	if !isErr(err, ErrMaxClaimsReached) {
		t.Errorf("expected ErrMaxClaimsReached, got %v", err)
	}
}

// TestResolveClaimApprove verifies approval does not refund stars.
func TestResolveClaimApprove(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Prize", "", "🎁", "", 10, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	claim, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	resolved, err := ResolveClaim(db, claim.ID, 1, "approved", "well done!")
	if err != nil {
		t.Fatalf("ResolveClaim approve: %v", err)
	}
	if resolved.Status != "approved" {
		t.Errorf("expected status 'approved', got %q", resolved.Status)
	}
	if resolved.ResolvedAt == nil {
		t.Error("expected non-nil resolved_at")
	}

	// Balance should remain at 90 (stars not refunded on approval).
	var balance int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = 2`).Scan(&balance); err != nil {
		t.Fatalf("scan balance: %v", err)
	}
	if balance != 90 {
		t.Errorf("expected balance 90 after approval, got %d", balance)
	}
}

// TestResolveClaimDeny verifies denial refunds the stars.
func TestResolveClaimDeny(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Prize", "", "🎁", "", 10, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	claim, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	if _, err := ResolveClaim(db, claim.ID, 1, "denied", "sorry"); err != nil {
		t.Fatalf("ResolveClaim deny: %v", err)
	}

	// Stars should be refunded — balance back to 100.
	var balance int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = 2`).Scan(&balance); err != nil {
		t.Fatalf("scan balance: %v", err)
	}
	if balance != 100 {
		t.Errorf("expected balance 100 after denial refund, got %d", balance)
	}
}

// TestResolveClaimNotPending verifies ErrClaimNotPending when resolving an already-resolved claim.
func TestResolveClaimNotPending(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Prize", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	claim, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	if _, err := ResolveClaim(db, claim.ID, 1, "approved", ""); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	_, err = ResolveClaim(db, claim.ID, 1, "denied", "")
	if !isErr(err, ErrClaimNotPending) {
		t.Errorf("expected ErrClaimNotPending, got %v", err)
	}
}

// TestResolveClaimWrongParent verifies ErrClaimNotFound when a non-owner tries to resolve.
func TestResolveClaimWrongParent(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	// Insert a second parent.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'other@test.com', 'Other', 'g3')`); err != nil {
		t.Fatalf("insert other parent: %v", err)
	}

	r, err := CreateReward(db, 1, "Prize", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	claim, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	// Parent 3 tries to resolve parent 1's claim.
	_, err = ResolveClaim(db, claim.ID, 3, "approved", "")
	if !isErr(err, ErrClaimNotFound) {
		t.Errorf("expected ErrClaimNotFound for wrong parent, got %v", err)
	}
}

// TestGetClaimsByUser verifies a child can see their own claims.
func TestGetClaimsByUser(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Book", "", "📚", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	if _, err := ClaimReward(db, 2, r.ID); err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	claims, err := GetClaimsByUser(db, 2)
	if err != nil {
		t.Fatalf("GetClaimsByUser: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if claims[0].RewardTitle != "Book" {
		t.Errorf("unexpected reward_title: %q", claims[0].RewardTitle)
	}
	if claims[0].Status != "pending" {
		t.Errorf("expected pending, got %q", claims[0].Status)
	}
}

// TestGetPendingClaims verifies only pending claims are returned to the parent.
func TestGetPendingClaims(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Toy", "", "🧸", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	// Create two claims, then approve one.
	c1, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward 1: %v", err)
	}
	// Give more stars and claim again.
	if _, err := db.Exec(`UPDATE star_balances SET total_earned = total_earned + 50 WHERE user_id = 2`); err != nil {
		t.Fatalf("top up: %v", err)
	}
	if _, err := ClaimReward(db, 2, r.ID); err != nil {
		t.Fatalf("ClaimReward 2: %v", err)
	}

	if _, err := ResolveClaim(db, c1.ID, 1, "approved", ""); err != nil {
		t.Fatalf("ResolveClaim: %v", err)
	}

	pending, err := GetPendingClaims(db, 1)
	if err != nil {
		t.Fatalf("GetPendingClaims: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending claim, got %d", len(pending))
	}
	if pending[0].Status != "pending" {
		t.Errorf("expected pending status, got %q", pending[0].Status)
	}
}

// TestGetAllClaims verifies all claims are returned with correct reward title decryption.
func TestGetAllClaims(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Sticker", "", "⭐", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	if _, err := ClaimReward(db, 2, r.ID); err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	all, err := GetAllClaims(db, 1, "")
	if err != nil {
		t.Fatalf("GetAllClaims: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(all))
	}
	if all[0].RewardTitle != "Sticker" {
		t.Errorf("unexpected reward_title: %q", all[0].RewardTitle)
	}
}

// TestClaimRewardInsufficientStarsNoSideEffects verifies that a failed claim
// due to insufficient stars leaves no DB side effects (no claim row, no transaction,
// no balance change).
func TestClaimRewardInsufficientStarsNoSideEffects(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Expensive", "", "💎", "", 999, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	_, err = ClaimReward(db, 2, r.ID)
	if !isErr(err, ErrInsufficientStars) {
		t.Fatalf("expected ErrInsufficientStars, got %v", err)
	}

	// No claim row should have been inserted.
	var claimCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reward_claims WHERE reward_id = ?`, r.ID).Scan(&claimCount); err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if claimCount != 0 {
		t.Errorf("expected 0 claim rows after failed claim, got %d", claimCount)
	}

	// No star transaction should have been recorded.
	var txCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = 2`).Scan(&txCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txCount != 0 {
		t.Errorf("expected 0 star transactions after failed claim, got %d", txCount)
	}

	// Balance should remain unchanged at 100.
	var balance int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = 2`).Scan(&balance); err != nil {
		t.Fatalf("scan balance: %v", err)
	}
	if balance != 100 {
		t.Errorf("expected balance 100 after failed claim, got %d", balance)
	}
}

// TestClaimRewardRaceCondition verifies that concurrent claims against a
// max_claims=1 reward result in exactly one successful claim. The transaction
// serializes competing claims so the count check is atomic.
func TestClaimRewardRaceCondition(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	// Give the child enough stars for multiple claims.
	if _, err := db.Exec(`UPDATE star_balances SET total_earned = total_earned + 500 WHERE user_id = 2`); err != nil {
		t.Fatalf("top up stars: %v", err)
	}

	maxOne := 1
	r, err := CreateReward(db, 1, "Limited Prize", "", "🏅", "", 5, true, &maxOne)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	const goroutines = 5
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = ClaimReward(db, 2, r.ID)
		}(i)
	}
	wg.Wait()

	// Count successes and failures.
	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		} else if !isErr(e, ErrMaxClaimsReached) && !isErr(e, ErrInsufficientStars) {
			t.Errorf("unexpected error from concurrent claim: %v", e)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful claim, got %d", successes)
	}

	// Exactly 1 claim row should exist.
	var claimCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reward_claims WHERE reward_id = ?`, r.ID).Scan(&claimCount); err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if claimCount != 1 {
		t.Errorf("expected 1 claim row in DB, got %d", claimCount)
	}
}

// TestCreateRewardDescriptionAndParentNoteEncrypted verifies that description
// and parent_note are stored encrypted in the DB (not just title).
func TestCreateRewardDescriptionAndParentNoteEncrypted(t *testing.T) {
	db := setupRewardsTestDB(t)

	if _, err := CreateReward(db, 1, "Title", "Sensitive description", "🎁", "Private note", 5, true, nil); err != nil {
		t.Fatalf("CreateReward: %v", err)
	}

	var rawDesc, rawNote string
	if err := db.QueryRow(`SELECT description, parent_note FROM family_rewards WHERE parent_id = 1`).Scan(&rawDesc, &rawNote); err != nil {
		t.Fatalf("scan fields: %v", err)
	}

	if len(rawDesc) < 4 || rawDesc[:4] != "enc:" {
		t.Errorf("expected description to be encrypted in DB, got %q", rawDesc[:min(len(rawDesc), 20)])
	}
	if len(rawNote) < 4 || rawNote[:4] != "enc:" {
		t.Errorf("expected parent_note to be encrypted in DB, got %q", rawNote[:min(len(rawNote), 20)])
	}
}

// TestResolveClaimNoteEncrypted verifies that the resolution note is stored
// encrypted in the DB when a claim is resolved.
func TestResolveClaimNoteEncrypted(t *testing.T) {
	db := setupRewardsTestDB(t)
	linkFamilies(t, db)

	r, err := CreateReward(db, 1, "Prize", "", "🎁", "", 5, true, nil)
	if err != nil {
		t.Fatalf("CreateReward: %v", err)
	}
	claim, err := ClaimReward(db, 2, r.ID)
	if err != nil {
		t.Fatalf("ClaimReward: %v", err)
	}

	if _, err := ResolveClaim(db, claim.ID, 1, "approved", "Well done, kiddo!"); err != nil {
		t.Fatalf("ResolveClaim: %v", err)
	}

	var rawNote string
	if err := db.QueryRow(`SELECT note FROM reward_claims WHERE id = ?`, claim.ID).Scan(&rawNote); err != nil {
		t.Fatalf("scan note: %v", err)
	}
	if len(rawNote) < 4 || rawNote[:4] != "enc:" {
		t.Errorf("expected resolution note to be encrypted in DB, got %q", rawNote[:min(len(rawNote), 20)])
	}
}
