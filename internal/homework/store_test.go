package homework

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-homework-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })

	_, err = d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'parent@example.com', 'Parent', 'g1')`)
	if err != nil {
		t.Fatalf("insert parent user: %v", err)
	}
	_, err = d.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'kid@example.com', 'Kid', 'g2')`)
	if err != nil {
		t.Fatalf("insert kid user: %v", err)
	}
	return d
}

func TestProfileCRUD(t *testing.T) {
	d := setupTestDB(t)

	// Create profile.
	p, err := CreateProfile(d, HomeworkProfile{
		KidID:             2,
		Age:               10,
		GradeLevel:        "5th",
		Subjects:          []string{"math", "science"},
		PreferredLanguage: "nb",
		SchoolName:        "Test School",
		CurrentTopics:     []string{"fractions", "plants"},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("expected non-zero profile ID")
	}
	if p.CreatedAt == "" || p.UpdatedAt == "" {
		t.Fatal("expected timestamps to be set")
	}

	// Get profile.
	got, err := GetProfileByKidID(d, 2)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if got == nil {
		t.Fatal("expected profile, got nil")
	}
	if got.Age != 10 || got.GradeLevel != "5th" {
		t.Fatalf("unexpected profile values: age=%d grade=%s", got.Age, got.GradeLevel)
	}
	if got.PreferredLanguage != "nb" {
		t.Fatalf("expected preferred_language 'nb', got %q", got.PreferredLanguage)
	}
	if got.SchoolName != "Test School" {
		t.Fatalf("expected school 'Test School', got %q", got.SchoolName)
	}
	if len(got.Subjects) != 2 || got.Subjects[0] != "math" {
		t.Fatalf("unexpected subjects: %v", got.Subjects)
	}
	if len(got.CurrentTopics) != 2 || got.CurrentTopics[0] != "fractions" {
		t.Fatalf("unexpected topics: %v", got.CurrentTopics)
	}

	// Update profile.
	err = UpdateProfile(d, HomeworkProfile{
		KidID:             2,
		Age:               11,
		GradeLevel:        "6th",
		Subjects:          []string{"math", "science", "english"},
		PreferredLanguage: "en",
		SchoolName:        "New School",
		CurrentTopics:     []string{"decimals"},
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}

	got, err = GetProfileByKidID(d, 2)
	if err != nil {
		t.Fatalf("get updated profile: %v", err)
	}
	if got.Age != 11 || got.GradeLevel != "6th" {
		t.Fatalf("profile not updated: age=%d grade=%s", got.Age, got.GradeLevel)
	}
	if got.PreferredLanguage != "en" {
		t.Fatalf("expected preferred_language 'en' after update, got %q", got.PreferredLanguage)
	}
	if len(got.Subjects) != 3 {
		t.Fatalf("expected 3 subjects, got %d", len(got.Subjects))
	}

	// Get non-existent profile.
	none, err := GetProfileByKidID(d, 999)
	if err != nil {
		t.Fatalf("get missing profile: %v", err)
	}
	if none != nil {
		t.Fatal("expected nil for missing profile")
	}
}

func TestProfileUniqueKid(t *testing.T) {
	d := setupTestDB(t)

	_, err := CreateProfile(d, HomeworkProfile{
		KidID:         2,
		Age:           10,
		GradeLevel:    "5th",
		Subjects:      []string{},
		CurrentTopics: []string{},
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = CreateProfile(d, HomeworkProfile{
		KidID:         2,
		Age:           11,
		GradeLevel:    "6th",
		Subjects:      []string{},
		CurrentTopics: []string{},
	})
	if err == nil {
		t.Fatal("expected error for duplicate kid_id profile")
	}
}

func TestUpdateProfileNotFound(t *testing.T) {
	d := setupTestDB(t)

	err := UpdateProfile(d, HomeworkProfile{
		KidID:         999,
		Age:           10,
		GradeLevel:    "5th",
		Subjects:      []string{},
		CurrentTopics: []string{},
	})
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows, got: %v", err)
	}
}

func TestConversationCRUD(t *testing.T) {
	d := setupTestDB(t)

	// Create conversation.
	conv, err := CreateConversation(d, HomeworkConversation{
		KidID:   2,
		Subject: "Math homework chapter 5",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if conv.ID == 0 {
		t.Fatal("expected non-zero conversation ID")
	}

	// Get conversation.
	got, err := GetConversation(d, conv.ID, 2)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got == nil {
		t.Fatal("expected conversation, got nil")
	}
	if got.Subject != "Math homework chapter 5" {
		t.Fatalf("expected subject 'Math homework chapter 5', got %q", got.Subject)
	}

	// Get non-existent conversation.
	none, err := GetConversation(d, 999, 2)
	if err != nil {
		t.Fatalf("get missing conversation: %v", err)
	}
	if none != nil {
		t.Fatal("expected nil for missing conversation")
	}

	// Get conversation with wrong kid_id returns nil (access control).
	wrongKid, err := GetConversation(d, conv.ID, 1)
	if err != nil {
		t.Fatalf("get conversation wrong kid: %v", err)
	}
	if wrongKid != nil {
		t.Fatal("expected nil when accessing conversation with wrong kid_id")
	}

	// List conversations.
	conv2, err := CreateConversation(d, HomeworkConversation{
		KidID:   2,
		Subject: "Science project",
	})
	if err != nil {
		t.Fatalf("create second conversation: %v", err)
	}

	list, err := ListConversationsByKid(d, 2)
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(list))
	}
	// Newest first.
	if list[0].ID != conv2.ID {
		t.Fatalf("expected newest conversation first, got id=%d", list[0].ID)
	}

	// Empty list for user with no conversations.
	empty, err := ListConversationsByKid(d, 1)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 conversations, got %d", len(empty))
	}
}

func TestMessageCRUD(t *testing.T) {
	d := setupTestDB(t)

	conv, err := CreateConversation(d, HomeworkConversation{
		KidID:   2,
		Subject: "Math",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	// Add messages.
	msg1, err := AddMessage(d, HomeworkMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "How do I solve 3/4 + 1/2?",
		HelpLevel:      "",
	})
	if err != nil {
		t.Fatalf("add message 1: %v", err)
	}
	if msg1.ID == 0 {
		t.Fatal("expected non-zero message ID")
	}

	msg2, err := AddMessage(d, HomeworkMessage{
		ConversationID: conv.ID,
		Role:           "assistant",
		Content:        "First, find a common denominator...",
		HelpLevel:      HelpLevelHint,
	})
	if err != nil {
		t.Fatalf("add message 2: %v", err)
	}

	_, err = AddMessage(d, HomeworkMessage{
		ConversationID: conv.ID,
		Role:           "assistant",
		Content:        "The common denominator of 4 and 2 is 4...",
		HelpLevel:      HelpLevelExplain,
	})
	if err != nil {
		t.Fatalf("add message 3: %v", err)
	}

	// Get messages.
	msgs, err := GetMessages(d, conv.ID, 2)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "How do I solve 3/4 + 1/2?" {
		t.Fatalf("unexpected first message content: %q", msgs[0].Content)
	}
	if msgs[1].HelpLevel != HelpLevelHint {
		t.Fatalf("expected hint help level, got %q", msgs[1].HelpLevel)
	}
	if msgs[1].ID != msg2.ID {
		t.Fatalf("expected message ID %d, got %d", msg2.ID, msgs[1].ID)
	}

	// Empty messages for non-existent conversation.
	empty, err := GetMessages(d, 999, 2)
	if err != nil {
		t.Fatalf("get empty messages: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(empty))
	}

	// Messages with wrong kid_id returns empty (access control).
	wrongKid, err := GetMessages(d, conv.ID, 1)
	if err != nil {
		t.Fatalf("get messages wrong kid: %v", err)
	}
	if len(wrongKid) != 0 {
		t.Fatalf("expected 0 messages for wrong kid, got %d", len(wrongKid))
	}
}

func TestConversationUpdatedAtOnMessage(t *testing.T) {
	d := setupTestDB(t)

	conv, err := CreateConversation(d, HomeworkConversation{
		KidID:   2,
		Subject: "Math",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	originalUpdatedAt := conv.UpdatedAt

	// Ensure a distinct timestamp for the next operation even when the
	// underlying clock or stored timestamp format has coarse resolution.
	time.Sleep(1100 * time.Millisecond)

	_, err = AddMessage(d, HomeworkMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "Help me",
	})
	if err != nil {
		t.Fatalf("add message: %v", err)
	}

	got, err := GetConversation(d, conv.ID, 2)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.UpdatedAt == originalUpdatedAt {
		t.Fatal("expected updated_at to change after adding a message")
	}
}

func TestParentReview(t *testing.T) {
	d := setupTestDB(t)

	conv, err := CreateConversation(d, HomeworkConversation{
		KidID:   2,
		Subject: "Math",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	_, err = AddMessage(d, HomeworkMessage{ConversationID: conv.ID, Role: "user", Content: "Q1"})
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}
	_, err = AddMessage(d, HomeworkMessage{ConversationID: conv.ID, Role: "assistant", Content: "A1", HelpLevel: HelpLevelHint})
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}
	_, err = AddMessage(d, HomeworkMessage{ConversationID: conv.ID, Role: "assistant", Content: "A2", HelpLevel: HelpLevelHint})
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}
	_, err = AddMessage(d, HomeworkMessage{ConversationID: conv.ID, Role: "assistant", Content: "A3", HelpLevel: HelpLevelExplain})
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}

	summaries, err := GetConversationsForParentReview(d, 2)
	if err != nil {
		t.Fatalf("parent review: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.MessageCount != 4 {
		t.Fatalf("expected 4 messages, got %d", s.MessageCount)
	}
	if s.HelpLevels["hint"] != 2 {
		t.Fatalf("expected 2 hints, got %d", s.HelpLevels["hint"])
	}
	if s.HelpLevels["explain"] != 1 {
		t.Fatalf("expected 1 explain, got %d", s.HelpLevels["explain"])
	}

	// Empty review for user with no conversations.
	empty, err := GetConversationsForParentReview(d, 1)
	if err != nil {
		t.Fatalf("empty review: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(empty))
	}
}

func TestMessageWithImage(t *testing.T) {
	d := setupTestDB(t)

	conv, err := CreateConversation(d, HomeworkConversation{
		KidID:   2,
		Subject: "Science",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	msg, err := AddMessage(d, HomeworkMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "What is this diagram?",
		ImagePath:      "/uploads/homework/diagram.png",
	})
	if err != nil {
		t.Fatalf("add message with image: %v", err)
	}

	msgs, err := GetMessages(d, conv.ID, 2)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ImagePath != "/uploads/homework/diagram.png" {
		t.Fatalf("expected image path, got %q", msgs[0].ImagePath)
	}
	if msgs[0].ID != msg.ID {
		t.Fatalf("expected message ID %d, got %d", msg.ID, msgs[0].ID)
	}
}

func TestUpdateConversationSubject(t *testing.T) {
	d := setupTestDB(t)

	conv, err := CreateConversation(d, HomeworkConversation{KidID: 2, Subject: ""})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if conv.Subject != "" {
		t.Fatalf("expected empty subject, got %q", conv.Subject)
	}

	// Update the subject via the store function (which encrypts).
	if err := UpdateConversationSubject(d, conv.ID, 2, "math"); err != nil {
		t.Fatalf("update subject: %v", err)
	}

	// Read back and verify decryption round-trips.
	got, err := GetConversation(d, conv.ID, 2)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.Subject != "math" {
		t.Errorf("expected subject 'math', got %q", got.Subject)
	}

	// Verify the raw DB value is encrypted (not plaintext).
	var rawSubject string
	err = d.QueryRow(`SELECT subject FROM homework_conversations WHERE id = ?`, conv.ID).Scan(&rawSubject)
	if err != nil {
		t.Fatalf("query raw subject: %v", err)
	}
	if rawSubject == "math" {
		t.Error("expected subject to be encrypted in DB, but found plaintext")
	}
}
