package auth

import (
	"errors"
	"testing"
)

func TestSignInIsInviteOnly(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(`CREATE TABLE invited_emails (email TEXT PRIMARY KEY, invited_by INTEGER, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	// A fresh install: the first person in becomes the owner.
	if ok, err := SignInAllowed(db, "g-first", "owner@example.com"); err != nil || !ok {
		t.Fatalf("first user: %v, %v", ok, err)
	}
	owner, err := UpsertUser(db, "g-first", "owner@example.com", "Owner", "")
	if err != nil {
		t.Fatal(err)
	}

	// From then on: existing users yes, strangers no.
	if ok, _ := SignInAllowed(db, "g-first", "owner@example.com"); !ok {
		t.Fatal("an existing user must be able to sign in")
	}
	if ok, _ := SignInAllowed(db, "g-stranger", "stranger@example.com"); ok {
		t.Fatal("a stranger must not get in")
	}

	// An invite opens the door, whatever the capitalisation.
	if err := AddInvite(db, "  Khatiya@Example.com ", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := AddInvite(db, "khatiya@example.com", owner.ID); err != nil {
		t.Fatalf("inviting twice: %v", err)
	}
	if list, _ := ListInvites(db); len(list) != 1 || list[0].Email != "khatiya@example.com" {
		t.Fatalf("invites = %+v", list)
	}
	if ok, _ := SignInAllowed(db, "g-khatiya", "KHATIYA@example.com"); !ok {
		t.Fatal("an invited email must be able to sign in")
	}
	UseInvite(db, "khatiya@example.com")
	if list, _ := ListInvites(db); len(list) != 0 {
		t.Fatalf("a used invite should be gone: %+v", list)
	}

	for _, bad := range []string{"", "not-an-email", "Name <a@b.no>", "a@b"} {
		if err := AddInvite(db, bad, owner.ID); !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("invite %q: %v", bad, err)
		}
	}
	_ = AddInvite(db, "gone@example.com", owner.ID)
	if err := DeleteInvite(db, "GONE@example.com"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := SignInAllowed(db, "g-gone", "gone@example.com"); ok {
		t.Fatal("a withdrawn invite must not work")
	}
}
