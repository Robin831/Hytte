package family

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-family-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id        INTEGER PRIMARY KEY,
		email     TEXT UNIQUE NOT NULL,
		name      TEXT NOT NULL,
		picture   TEXT NOT NULL DEFAULT '',
		google_id TEXT UNIQUE NOT NULL,
		is_admin  INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS family_links (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		nickname     TEXT NOT NULL DEFAULT '',
		avatar_emoji TEXT NOT NULL DEFAULT '⭐',
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id),
		UNIQUE(child_id)
	);

	CREATE INDEX IF NOT EXISTS idx_family_links_parent ON family_links(parent_id);
	CREATE INDEX IF NOT EXISTS idx_family_links_child ON family_links(child_id);

	CREATE TABLE IF NOT EXISTS invite_codes (
		id         INTEGER PRIMARY KEY,
		code       TEXT NOT NULL UNIQUE,
		parent_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used       INTEGER NOT NULL DEFAULT 0,
		expires_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_invite_codes_parent ON invite_codes(parent_id);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Insert two test users.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'parent@test.com', 'Parent', 'g1')`); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'child@test.com', 'Child', 'g2')`); err != nil {
		t.Fatalf("insert child: %v", err)
	}

	return db
}

func TestCreateLink(t *testing.T) {
	db := setupTestDB(t)

	link, err := CreateLink(db, 1, 2, "Kiddo", "⭐")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if link.ParentID != 1 || link.ChildID != 2 {
		t.Errorf("unexpected link: parent=%d child=%d", link.ParentID, link.ChildID)
	}
	if link.Nickname != "Kiddo" {
		t.Errorf("expected nickname 'Kiddo', got %q", link.Nickname)
	}
	if link.AvatarEmoji != "⭐" {
		t.Errorf("expected avatar '⭐', got %q", link.AvatarEmoji)
	}
}

func TestCreateLinkDuplicateErrors(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("first CreateLink: %v", err)
	}

	_, err := CreateLink(db, 1, 2, "Kiddo2", "🌟")
	if err == nil {
		t.Error("expected error for duplicate link, got nil")
	}
}

func TestGetChildren(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	children, err := GetChildren(db, 1)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Nickname != "Kiddo" {
		t.Errorf("expected nickname 'Kiddo', got %q", children[0].Nickname)
	}
}

func TestGetChildrenEmpty(t *testing.T) {
	db := setupTestDB(t)

	children, err := GetChildren(db, 1)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("expected 0 children, got %d", len(children))
	}
}

func TestGetParent(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	parent, err := GetParent(db, 2)
	if err != nil {
		t.Fatalf("GetParent: %v", err)
	}
	if parent == nil {
		t.Fatal("expected parent link, got nil")
	}
	if parent.ParentID != 1 {
		t.Errorf("expected parent_id 1, got %d", parent.ParentID)
	}
}

func TestGetParentNotLinked(t *testing.T) {
	db := setupTestDB(t)

	parent, err := GetParent(db, 2)
	if err != nil {
		t.Fatalf("GetParent: %v", err)
	}
	if parent != nil {
		t.Error("expected nil parent for unlinked child")
	}
}

func TestUpdateChild(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	updated, err := UpdateChild(db, 1, 2, "Champion", "🏆")
	if err != nil {
		t.Fatalf("UpdateChild: %v", err)
	}
	if updated.Nickname != "Champion" {
		t.Errorf("expected nickname 'Champion', got %q", updated.Nickname)
	}
	if updated.AvatarEmoji != "🏆" {
		t.Errorf("expected avatar '🏆', got %q", updated.AvatarEmoji)
	}
}

func TestUpdateChildNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := UpdateChild(db, 1, 2, "Champion", "🏆")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestRemoveChild(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Kiddo", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if err := RemoveChild(db, 1, 2); err != nil {
		t.Fatalf("RemoveChild: %v", err)
	}

	children, err := GetChildren(db, 1)
	if err != nil {
		t.Fatalf("GetChildren after remove: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("expected 0 children after remove, got %d", len(children))
	}
}

func TestRemoveChildNotFound(t *testing.T) {
	db := setupTestDB(t)

	err := RemoveChild(db, 1, 2)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestIsParent(t *testing.T) {
	db := setupTestDB(t)

	isParent, err := IsParent(db, 1)
	if err != nil {
		t.Fatalf("IsParent: %v", err)
	}
	if isParent {
		t.Error("expected false before linking")
	}

	if _, err := CreateLink(db, 1, 2, "", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	isParent, err = IsParent(db, 1)
	if err != nil {
		t.Fatalf("IsParent after link: %v", err)
	}
	if !isParent {
		t.Error("expected true after linking")
	}
}

func TestIsChild(t *testing.T) {
	db := setupTestDB(t)

	isChild, err := IsChild(db, 2)
	if err != nil {
		t.Fatalf("IsChild: %v", err)
	}
	if isChild {
		t.Error("expected false before linking")
	}

	if _, err := CreateLink(db, 1, 2, "", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	isChild, err = IsChild(db, 2)
	if err != nil {
		t.Fatalf("IsChild after link: %v", err)
	}
	if !isChild {
		t.Error("expected true after linking")
	}
}

func TestNicknameEncryptedAtRest(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateLink(db, 1, 2, "Secret Name", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	var rawNickname string
	if err := db.QueryRow(`SELECT nickname FROM family_links WHERE parent_id = 1`).Scan(&rawNickname); err != nil {
		t.Fatalf("scan nickname: %v", err)
	}

	// Raw value in DB should be encrypted (enc: prefix).
	if len(rawNickname) >= 4 && rawNickname[:4] != "enc:" {
		t.Errorf("expected nickname to be encrypted in DB, got %q", rawNickname[:min(len(rawNickname), 20)])
	}
}

func TestGenerateInviteCode(t *testing.T) {
	db := setupTestDB(t)

	invite, err := GenerateInviteCode(db, 1)
	if err != nil {
		t.Fatalf("GenerateInviteCode: %v", err)
	}

	if len(invite.Code) != inviteCodeLen {
		t.Errorf("expected code length %d, got %d", inviteCodeLen, len(invite.Code))
	}
	if invite.ParentID != 1 {
		t.Errorf("expected parent_id 1, got %d", invite.ParentID)
	}
	if invite.Used {
		t.Error("expected new invite code to be unused")
	}
	if time.Until(invite.ExpiresAt) < 23*time.Hour {
		t.Error("expected invite to expire in ~24h")
	}
}

func TestAcceptInviteCode(t *testing.T) {
	db := setupTestDB(t)

	invite, err := GenerateInviteCode(db, 1)
	if err != nil {
		t.Fatalf("GenerateInviteCode: %v", err)
	}

	link, err := AcceptInviteCode(db, invite.Code, 2)
	if err != nil {
		t.Fatalf("AcceptInviteCode: %v", err)
	}

	if link.ParentID != 1 || link.ChildID != 2 {
		t.Errorf("unexpected link: parent=%d child=%d", link.ParentID, link.ChildID)
	}

	// Code should now be marked as used.
	_, err = AcceptInviteCode(db, invite.Code, 2)
	if !isErr(err, ErrCodeAlreadyUsed) {
		t.Errorf("expected ErrCodeAlreadyUsed, got %v", err)
	}
}

func TestAcceptInvalidCode(t *testing.T) {
	db := setupTestDB(t)

	_, err := AcceptInviteCode(db, "XXXXXX", 2)
	if !isErr(err, ErrInvalidCode) {
		t.Errorf("expected ErrInvalidCode, got %v", err)
	}
}

func TestAcceptExpiredCode(t *testing.T) {
	db := setupTestDB(t)

	// Insert an already-expired code directly.
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO invite_codes (code, parent_id, expires_at, created_at)
		VALUES ('EXPRD1', 1, ?, ?)
	`, pastTime, pastTime); err != nil {
		t.Fatalf("insert expired code: %v", err)
	}

	_, err := AcceptInviteCode(db, "EXPRD1", 2)
	if !isErr(err, ErrCodeExpired) {
		t.Errorf("expected ErrCodeExpired, got %v", err)
	}
}

func TestAcceptCodeAlreadyLinked(t *testing.T) {
	db := setupTestDB(t)

	// Insert a second parent user.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'parent2@test.com', 'Parent2', 'g3')`); err != nil {
		t.Fatalf("insert second parent: %v", err)
	}

	// Link child to parent 1.
	if _, err := CreateLink(db, 1, 2, "", "⭐"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Generate invite from parent 3 and try to have child 2 accept it.
	invite, err := GenerateInviteCode(db, 3)
	if err != nil {
		t.Fatalf("GenerateInviteCode: %v", err)
	}

	_, err = AcceptInviteCode(db, invite.Code, 2)
	if !isErr(err, ErrAlreadyLinked) {
		t.Errorf("expected ErrAlreadyLinked, got %v", err)
	}
}

func TestAcceptCodeSelfLink(t *testing.T) {
	db := setupTestDB(t)

	invite, err := GenerateInviteCode(db, 1)
	if err != nil {
		t.Fatalf("GenerateInviteCode: %v", err)
	}

	// Parent tries to accept their own invite.
	_, err = AcceptInviteCode(db, invite.Code, 1)
	if !isErr(err, ErrSelfLink) {
		t.Errorf("expected ErrSelfLink, got %v", err)
	}
}

func isErr(err, target error) bool {
	return err != nil && err.Error() == target.Error()
}
