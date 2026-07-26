package wardrobe

import (
	"database/sql"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-wardrobe-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(1, 'parent@example.com', 'Parent', '', 'g1', ''),
		(2, 'other@example.com', 'Other', '', 'g2', '')`)
	if err != nil {
		t.Fatalf("insert test users: %v", err)
	}
	return database
}

func mustAddKid(t *testing.T, d *sql.DB, parentID int64, name string) Kid {
	t.Helper()
	kid, err := AddKid(d, Kid{ParentID: parentID, Name: name, AvatarEmoji: "🧒"})
	if err != nil {
		t.Fatalf("add kid: %v", err)
	}
	return kid
}

func TestKidCRUD(t *testing.T) {
	d := setupTestDB(t)

	kid := mustAddKid(t, d, 1, "Ola")
	if kid.ID == 0 {
		t.Fatal("expected generated kid ID")
	}

	kids, err := ListKids(d, 1)
	if err != nil {
		t.Fatalf("list kids: %v", err)
	}
	if len(kids) != 1 || kids[0].Name != "Ola" {
		t.Fatalf("list kids = %+v, want one kid named Ola", kids)
	}

	// Name must be encrypted at rest.
	var raw string
	if err := d.QueryRow("SELECT name FROM wardrobe_kids WHERE id = ?", kid.ID).Scan(&raw); err != nil {
		t.Fatalf("read raw name: %v", err)
	}
	if raw == "Ola" {
		t.Error("kid name stored in plaintext")
	}

	if err := UpdateKid(d, kid.ID, 1, KidRequest{Name: "Kari", Birthdate: "2021-05-01"}); err != nil {
		t.Fatalf("update kid: %v", err)
	}
	// Ownership scoping: another user cannot touch the kid.
	if err := UpdateKid(d, kid.ID, 2, KidRequest{Name: "X"}); err != sql.ErrNoRows {
		t.Errorf("update as other user = %v, want sql.ErrNoRows", err)
	}
	if err := DeleteKid(d, kid.ID, 2); err != sql.ErrNoRows {
		t.Errorf("delete as other user = %v, want sql.ErrNoRows", err)
	}
	if err := DeleteKid(d, kid.ID, 1); err != nil {
		t.Fatalf("delete kid: %v", err)
	}
	kids, _ = ListKids(d, 1)
	if len(kids) != 0 {
		t.Errorf("expected no kids after delete, got %d", len(kids))
	}
}

func TestMeasurementsAndStats(t *testing.T) {
	d := setupTestDB(t)
	kid := mustAddKid(t, d, 1, "Ola")

	for _, m := range []Measurement{
		{KidID: kid.ID, MeasuredAt: "2026-01-01", HeightCM: 100, FootLengthMM: 155},
		{KidID: kid.ID, MeasuredAt: "2026-04-01", HeightCM: 103, FootLengthMM: 160},
	} {
		if _, err := AddMeasurement(d, m); err != nil {
			t.Fatalf("add measurement: %v", err)
		}
	}

	stats, err := KidStats(d, kid)
	if err != nil {
		t.Fatalf("kid stats: %v", err)
	}
	if stats.LatestMeasurement == nil || stats.LatestMeasurement.HeightCM != 103 {
		t.Fatalf("latest measurement = %+v, want height 103", stats.LatestMeasurement)
	}
	if stats.Clothing == nil || stats.Clothing.CurrentSize != 104 {
		t.Errorf("clothing rec = %+v, want current size 104", stats.Clothing)
	}
	if stats.Shoe == nil || stats.Shoe.BuyEU != 27 {
		t.Errorf("shoe rec = %+v, want buy EU 27", stats.Shoe)
	}
	if stats.HeightRateCMPerMonth == nil || *stats.HeightRateCMPerMonth != 1.0 {
		t.Errorf("height rate = %v, want 1.0", stats.HeightRateCMPerMonth)
	}

	// Delete is scoped through kid ownership.
	ms, _ := ListMeasurements(d, kid.ID)
	if err := DeleteMeasurement(d, ms[0].ID, 2); err != sql.ErrNoRows {
		t.Errorf("delete as other user = %v, want sql.ErrNoRows", err)
	}
	if err := DeleteMeasurement(d, ms[0].ID, 1); err != nil {
		t.Fatalf("delete measurement: %v", err)
	}
}

func TestSeedIdempotent(t *testing.T) {
	d := setupTestDB(t)

	if err := SeedDefaultCategories(d, 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SeedDefaultCategories(d, 1); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	cats, err := ListCategories(d, 1)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != len(defaultCategories) {
		t.Errorf("got %d categories after double seed, want %d", len(cats), len(defaultCategories))
	}
	// Seeding is per user.
	other, _ := ListCategories(d, 2)
	if len(other) != 0 {
		t.Errorf("user 2 has %d categories, want 0", len(other))
	}
}

func TestItemsAndNeeds(t *testing.T) {
	d := setupTestDB(t)
	kid := mustAddKid(t, d, 1, "Ola")
	if err := SeedDefaultCategories(d, 1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cats, _ := ListCategories(d, 1)
	var wool, shoes Category // Ullundertøy target 2, Sko target 1
	for _, c := range cats {
		switch c.Name {
		case "Ullundertøy":
			wool = c
		case "Sko":
			shoes = c
		}
	}
	if wool.ID == 0 || shoes.ID == 0 {
		t.Fatal("expected seeded Ullundertøy and Sko categories")
	}

	if _, err := AddMeasurement(d, Measurement{KidID: kid.ID, MeasuredAt: "2026-07-01", HeightCM: 100, FootLengthMM: 160}); err != nil {
		t.Fatalf("add measurement: %v", err)
	}

	// One active wool set (target 2 → shortfall), one too-small pair of shoes.
	if _, err := AddItem(d, Item{
		ParentID: 1, KidID: kid.ID, CategoryID: wool.ID, Name: "Ullsett blå",
		SizeLabel: "98", Quantity: 1, Condition: "good", Status: "active", Location: "home", Season: "winter",
	}); err != nil {
		t.Fatalf("add wool item: %v", err)
	}
	tooSmallShoes, err := AddItem(d, Item{
		ParentID: 1, KidID: kid.ID, CategoryID: shoes.ID, Name: "Joggesko",
		SizeLabel: "EU 25", Quantity: 1, Condition: "worn", Status: "too_small", Location: "home", Season: "all",
	})
	if err != nil {
		t.Fatalf("add shoes item: %v", err)
	}

	needs, tooSmall, err := Needs(d, 1, kid.ID)
	if err != nil {
		t.Fatalf("needs: %v", err)
	}

	byCat := map[int64]NeedEntry{}
	for _, n := range needs {
		byCat[n.CategoryID] = n
	}
	if n, ok := byCat[wool.ID]; !ok || n.Have != 1 || n.Target != 2 || n.RecommendedSize != "104" {
		t.Errorf("wool need = %+v, want have 1 / target 2 / size 104", byCat[wool.ID])
	}
	// Shoes: the only pair is too_small, so active count 0 < target 1.
	if n, ok := byCat[shoes.ID]; !ok || n.Have != 0 || n.RecommendedSize != "EU 27" {
		t.Errorf("shoe need = %+v, want have 0 / size EU 27", byCat[shoes.ID])
	}
	if len(tooSmall) != 1 || tooSmall[0].Item.ID != tooSmallShoes.ID || tooSmall[0].RecommendedSize != "EU 27" {
		t.Errorf("too_small = %+v, want the shoes with size EU 27", tooSmall)
	}

	// Category with items refuses deletion.
	if err := DeleteCategory(d, wool.ID, 1); err != ErrCategoryInUse {
		t.Errorf("delete in-use category = %v, want ErrCategoryInUse", err)
	}

	// Item update scoped by owner.
	if err := UpdateItem(d, tooSmallShoes.ID, 2, ItemRequest{
		KidID: kid.ID, CategoryID: shoes.ID, Name: "X", Quantity: 1,
		Condition: "good", Status: "active", Location: "home", Season: "all",
	}); err != sql.ErrNoRows {
		t.Errorf("update as other user = %v, want sql.ErrNoRows", err)
	}
}
