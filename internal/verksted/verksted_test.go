package verksted

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-verksted-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	for _, u := range []struct {
		id    int64
		email string
		name  string
	}{{1, "robin@example.com", "Robin Smith"}, {2, "james@example.com", "James Smith"}} {
		if _, err := d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (?, ?, ?, ?)`, u.id, u.email, u.name, u.email); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func personByUser(t *testing.T, o *Overview, uid int64) Person {
	t.Helper()
	for _, p := range o.People {
		if p.UserID != nil && *p.UserID == uid {
			return p
		}
	}
	t.Fatalf("no person for user %d in %+v", uid, o.People)
	return Person{}
}

func ptr[T any](v T) *T { return &v }

func TestSeedAndSecrecy(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	o, err := LoadOverview(ctx, d, 1, 2026)
	if err != nil {
		t.Fatal(err)
	}
	// The household's users are seeded as the family.
	robin, james := personByUser(t, o, 1), personByUser(t, o, 2)
	if robin.Name != "Robin" || james.Group != "Familie" {
		t.Fatalf("seeded = %+v", o.People)
	}
	// Seeding runs once: a deleted person stays deleted.
	if err := DeletePerson(ctx, d, robin.ID); err != nil {
		t.Fatal(err)
	}
	if o, err = LoadOverview(ctx, d, 1, 2026); err != nil || len(o.People) != 1 {
		t.Fatalf("after delete: %d people, %v", len(o.People), err)
	}
	robinID, err := AddPerson(ctx, d, PersonInput{Name: ptr("Robin"), Group: ptr("Familie")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE verksted_people SET user_id = 1 WHERE id = ?`, robinID); err != nil {
		t.Fatal(err)
	}

	// James wishes for a bike; Robin plans it and the gift reserves the wish.
	wishID, err := AddWish(ctx, d, 2, WishInput{PersonID: ptr(james.ID), Title: ptr("Sykkel"), PriceNOK: ptr(2500.0), Priority: ptr(3)})
	if err != nil {
		t.Fatal(err)
	}
	giftID, err := AddGift(ctx, d, 1, GiftInput{Season: ptr(2026), Title: ptr("Sykkel"), Giver: ptr("Mamma og pappa"),
		PriceNOK: ptr(2300.0), Status: ptr("ordered"), WishID: ptr(wishID), RecipientIDs: &[]int64{james.ID}})
	if err != nil {
		t.Fatal(err)
	}
	// A gift for Robin, planned by James.
	if _, err := AddGift(ctx, d, 2, GiftInput{Season: ptr(2026), Title: ptr("Krus"), RecipientIDs: &[]int64{robinID}}); err != nil {
		t.Fatal(err)
	}

	// Robin sees the bike (reserved) but not his own present.
	o, _ = LoadOverview(ctx, d, 1, 2026)
	if len(o.Gifts) != 1 || o.Gifts[0].Title != "Sykkel" || o.Gifts[0].Giver != "Mamma og pappa" || o.Gifts[0].Status != "ordered" {
		t.Fatalf("robin's gifts = %+v", o.Gifts)
	}
	if len(o.Wishes) != 1 || !o.Wishes[0].Reserved || o.Wishes[0].GiftID == nil || *o.Wishes[0].GiftID != giftID {
		t.Fatalf("robin's view of the wish = %+v", o.Wishes)
	}
	// James sees his wish, not that it's taken, and not the bike.
	o, _ = LoadOverview(ctx, d, 2, 2026)
	if len(o.Wishes) != 1 || o.Wishes[0].Reserved || o.Wishes[0].GiftID != nil {
		t.Fatalf("james's view of his wish = %+v", o.Wishes)
	}
	if len(o.Gifts) != 1 || o.Gifts[0].Title != "Krus" {
		t.Fatalf("james's gifts = %+v", o.Gifts)
	}
	// ...and can't touch it by id either.
	if err := UpdateGift(ctx, d, 2, giftID, GiftInput{Status: ptr("given")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("james editing his own present: %v", err)
	}
	if err := ReserveWish(ctx, d, 2, wishID, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("james reserving his own wish: %v", err)
	}
	// Nobody plans a gift for themselves here.
	if _, err := AddGift(ctx, d, 1, GiftInput{Title: ptr("x"), RecipientIDs: &[]int64{robinID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("gift for self: %v", err)
	}
}

func TestGiftsBudgetsAndSeasons(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	o, err := LoadOverview(ctx, d, 1, 2026)
	if err != nil {
		t.Fatal(err)
	}
	james := personByUser(t, o, 2)
	granny, err := AddPerson(ctx, d, PersonInput{Name: ptr("Mormor"), Group: ptr("Besteforeldre"), Sizes: ptr("M")})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPersonBudget(ctx, d, james.ID, 2026, 3000); err != nil {
		t.Fatal(err)
	}
	if err := SetTotalBudget(ctx, d, 2026, 12000); err != nil {
		t.Fatal(err)
	}
	shared, err := AddGift(ctx, d, 1, GiftInput{Season: ptr(2026), Title: ptr("Brettspill"), RecipientIDs: &[]int64{james.ID, granny}})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateGift(ctx, d, 1, shared, GiftInput{Status: ptr("wrapped"), Shop: ptr("Outland"), RecipientIDs: &[]int64{granny}}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateGift(ctx, d, 1, shared, GiftInput{Status: ptr("lost")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad status: %v", err)
	}
	if _, err := AddGift(ctx, d, 1, GiftInput{Season: ptr(2025), Title: ptr("Lego"), RecipientIDs: &[]int64{james.ID}}); err != nil {
		t.Fatal(err)
	}

	o, err = LoadOverview(ctx, d, 1, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if o.TotalBudgetNOK != 12000 || len(o.Gifts) != 1 || o.Gifts[0].Shop != "Outland" || len(o.Gifts[0].RecipientIDs) != 1 {
		t.Fatalf("overview = %+v", o)
	}
	for _, p := range o.People {
		if p.ID == james.ID && p.BudgetNOK != 3000 {
			t.Fatalf("james budget = %v", p.BudgetNOK)
		}
		if p.ID == granny && (p.Sizes != "M" || p.Group != "Besteforeldre") {
			t.Fatalf("granny = %+v", p)
		}
	}
	if len(o.Seasons) < 2 || o.Seasons[0] < o.Seasons[1] {
		t.Fatalf("seasons = %v", o.Seasons)
	}
	// Removing the only recipient removes the gift.
	if err := DeletePerson(ctx, d, granny); err != nil {
		t.Fatal(err)
	}
	if o, _ = LoadOverview(ctx, d, 1, 2026); len(o.Gifts) != 0 {
		t.Fatalf("orphan gift kept: %+v", o.Gifts)
	}
}

func TestCurrentSeason(t *testing.T) {
	if s := CurrentSeason(time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)); s != 2026 {
		t.Fatalf("January = %d", s)
	}
	if s := CurrentSeason(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)); s != 2026 {
		t.Fatalf("November = %d", s)
	}
}
