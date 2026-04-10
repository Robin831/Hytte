package homework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// CreateProfile inserts a new homework profile for a child.
func CreateProfile(db *sql.DB, p HomeworkProfile) (HomeworkProfile, error) {
	subjectsJSON, err := json.Marshal(p.Subjects)
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("marshal subjects: %w", err)
	}
	topicsJSON, err := json.Marshal(p.CurrentTopics)
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("marshal current_topics: %w", err)
	}

	encSchool, err := encryption.EncryptField(p.SchoolName)
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("encrypt school_name: %w", err)
	}
	encSubjects, err := encryption.EncryptField(string(subjectsJSON))
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("encrypt subjects: %w", err)
	}
	encTopics, err := encryption.EncryptField(string(topicsJSON))
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("encrypt current_topics: %w", err)
	}
	encGradeLevel, err := encryption.EncryptField(p.GradeLevel)
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("encrypt grade_level: %w", err)
	}
	encLang, err := encryption.EncryptField(p.PreferredLanguage)
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("encrypt preferred_language: %w", err)
	}

	now := time.Now().UTC().Format(timeFormat)
	result, err := db.Exec(`
		INSERT INTO kids_homework_profiles (kid_id, age, grade_level, subjects, preferred_language, school_name, current_topics, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.KidID, p.Age, encGradeLevel, encSubjects, encLang, encSchool, encTopics, now, now,
	)
	if err != nil {
		return HomeworkProfile{}, fmt.Errorf("insert homework profile: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return HomeworkProfile{}, err
	}

	p.ID = id
	p.CreatedAt = now
	p.UpdatedAt = now
	return p, nil
}

// UpdateProfile updates an existing homework profile.
func UpdateProfile(db *sql.DB, p HomeworkProfile) error {
	subjectsJSON, err := json.Marshal(p.Subjects)
	if err != nil {
		return fmt.Errorf("marshal subjects: %w", err)
	}
	topicsJSON, err := json.Marshal(p.CurrentTopics)
	if err != nil {
		return fmt.Errorf("marshal current_topics: %w", err)
	}

	encSchool, err := encryption.EncryptField(p.SchoolName)
	if err != nil {
		return fmt.Errorf("encrypt school_name: %w", err)
	}
	encSubjects, err := encryption.EncryptField(string(subjectsJSON))
	if err != nil {
		return fmt.Errorf("encrypt subjects: %w", err)
	}
	encTopics, err := encryption.EncryptField(string(topicsJSON))
	if err != nil {
		return fmt.Errorf("encrypt current_topics: %w", err)
	}
	encGradeLevel, err := encryption.EncryptField(p.GradeLevel)
	if err != nil {
		return fmt.Errorf("encrypt grade_level: %w", err)
	}
	encLang, err := encryption.EncryptField(p.PreferredLanguage)
	if err != nil {
		return fmt.Errorf("encrypt preferred_language: %w", err)
	}

	now := time.Now().UTC().Format(timeFormat)
	result, err := db.Exec(`
		UPDATE kids_homework_profiles
		SET age = ?, grade_level = ?, subjects = ?, preferred_language = ?, school_name = ?, current_topics = ?, updated_at = ?
		WHERE kid_id = ?`,
		p.Age, encGradeLevel, encSubjects, encLang, encSchool, encTopics, now, p.KidID,
	)
	if err != nil {
		return fmt.Errorf("update homework profile: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetProfileByKidID returns the homework profile for a given child.
func GetProfileByKidID(db *sql.DB, kidID int64) (*HomeworkProfile, error) {
	var p HomeworkProfile
	var encSubjects, encSchool, encTopics, encGradeLevel, encLang string
	err := db.QueryRow(`
		SELECT id, kid_id, age, grade_level, subjects, preferred_language, school_name, current_topics, created_at, updated_at
		FROM kids_homework_profiles
		WHERE kid_id = ?`,
		kidID,
	).Scan(&p.ID, &p.KidID, &p.Age, &encGradeLevel, &encSubjects, &encLang, &encSchool, &encTopics, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get homework profile: %w", err)
	}

	gradeLevel, err := decryptField(encGradeLevel)
	if err != nil {
		return nil, fmt.Errorf("decrypt grade_level: %w", err)
	}
	p.GradeLevel = gradeLevel

	lang, err := decryptField(encLang)
	if err != nil {
		return nil, fmt.Errorf("decrypt preferred_language: %w", err)
	}
	p.PreferredLanguage = lang

	schoolName, err := decryptField(encSchool)
	if err != nil {
		return nil, fmt.Errorf("decrypt school_name: %w", err)
	}
	p.SchoolName = schoolName

	subjectsStr, err := decryptField(encSubjects)
	if err != nil {
		return nil, fmt.Errorf("decrypt subjects: %w", err)
	}
	if err := json.Unmarshal([]byte(subjectsStr), &p.Subjects); err != nil {
		return nil, fmt.Errorf("unmarshal subjects: %w", err)
	}

	topicsStr, err := decryptField(encTopics)
	if err != nil {
		return nil, fmt.Errorf("decrypt current_topics: %w", err)
	}
	if err := json.Unmarshal([]byte(topicsStr), &p.CurrentTopics); err != nil {
		return nil, fmt.Errorf("unmarshal current_topics: %w", err)
	}

	if p.Subjects == nil {
		p.Subjects = []string{}
	}
	if p.CurrentTopics == nil {
		p.CurrentTopics = []string{}
	}

	return &p, nil
}

// CreateConversation starts a new homework conversation for a child.
func CreateConversation(db *sql.DB, conv HomeworkConversation) (HomeworkConversation, error) {
	encSubject, err := encryption.EncryptField(conv.Subject)
	if err != nil {
		return HomeworkConversation{}, fmt.Errorf("encrypt subject: %w", err)
	}

	now := time.Now().UTC().Format(timeFormat)
	result, err := db.Exec(`
		INSERT INTO homework_conversations (kid_id, subject, created_at, updated_at)
		VALUES (?, ?, ?, ?)`,
		conv.KidID, encSubject, now, now,
	)
	if err != nil {
		return HomeworkConversation{}, fmt.Errorf("insert homework conversation: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return HomeworkConversation{}, err
	}

	conv.ID = id
	conv.CreatedAt = now
	conv.UpdatedAt = now
	return conv, nil
}

// GetConversation returns a single homework conversation by ID, scoped to a specific kid.
func GetConversation(db *sql.DB, id, kidID int64) (*HomeworkConversation, error) {
	var c HomeworkConversation
	var encSubject string
	err := db.QueryRow(`
		SELECT id, kid_id, subject, created_at, updated_at
		FROM homework_conversations
		WHERE id = ? AND kid_id = ?`,
		id, kidID,
	).Scan(&c.ID, &c.KidID, &encSubject, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get homework conversation: %w", err)
	}
	subject, err := decryptField(encSubject)
	if err != nil {
		return nil, fmt.Errorf("decrypt conversation subject: %w", err)
	}
	c.Subject = subject
	return &c, nil
}

// GetSessionID returns the Claude CLI session ID for a homework conversation.
func GetSessionID(db *sql.DB, conversationID, kidID int64) (string, error) {
	var sessionID string
	err := db.QueryRow(
		`SELECT session_id FROM homework_conversations WHERE id = ? AND kid_id = ?`,
		conversationID, kidID,
	).Scan(&sessionID)
	if err != nil {
		return "", fmt.Errorf("get homework session_id: %w", err)
	}
	return sessionID, nil
}

// UpdateSessionID stores the Claude CLI session ID on a homework conversation.
func UpdateSessionID(db *sql.DB, conversationID, kidID int64, sessionID string) error {
	now := time.Now().UTC().Format(timeFormat)
	result, err := db.Exec(
		`UPDATE homework_conversations SET session_id = ?, updated_at = ? WHERE id = ? AND kid_id = ?`,
		sessionID, now, conversationID, kidID,
	)
	if err != nil {
		return fmt.Errorf("update homework session_id: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListConversationsByKid returns all homework conversations for a child, newest first.
func ListConversationsByKid(db *sql.DB, kidID int64) ([]HomeworkConversation, error) {
	rows, err := db.Query(`
		SELECT id, kid_id, subject, created_at, updated_at
		FROM homework_conversations
		WHERE kid_id = ?
		ORDER BY updated_at DESC, id DESC`,
		kidID,
	)
	if err != nil {
		return nil, fmt.Errorf("list homework conversations: %w", err)
	}
	defer rows.Close()

	var convos []HomeworkConversation
	for rows.Next() {
		var c HomeworkConversation
		var encSubject string
		if err := rows.Scan(&c.ID, &c.KidID, &encSubject, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		subject, err := decryptField(encSubject)
		if err != nil {
			return nil, fmt.Errorf("decrypt conversation subject: %w", err)
		}
		c.Subject = subject
		convos = append(convos, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if convos == nil {
		convos = []HomeworkConversation{}
	}
	return convos, nil
}

// AddMessage inserts a new message into a homework conversation and updates the conversation timestamp.
func AddMessage(db *sql.DB, msg HomeworkMessage) (HomeworkMessage, error) {
	if msg.HelpLevel != "" && !ValidHelpLevels[msg.HelpLevel] {
		return HomeworkMessage{}, fmt.Errorf("invalid help_level %q: must be one of hint, explain, walkthrough, answer", msg.HelpLevel)
	}

	encContent, err := encryption.EncryptField(msg.Content)
	if err != nil {
		return HomeworkMessage{}, fmt.Errorf("encrypt content: %w", err)
	}
	encImagePath, err := encryption.EncryptField(msg.ImagePath)
	if err != nil {
		return HomeworkMessage{}, fmt.Errorf("encrypt image_path: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return HomeworkMessage{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC().Format(timeFormat)
	result, err := tx.Exec(`
		INSERT INTO homework_messages (conversation_id, role, content, help_level, image_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ConversationID, msg.Role, encContent, string(msg.HelpLevel), encImagePath, now,
	)
	if err != nil {
		return HomeworkMessage{}, fmt.Errorf("insert homework message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return HomeworkMessage{}, err
	}

	// Update the conversation's updated_at timestamp.
	_, err = tx.Exec(`UPDATE homework_conversations SET updated_at = ? WHERE id = ?`, now, msg.ConversationID)
	if err != nil {
		return HomeworkMessage{}, fmt.Errorf("update conversation timestamp: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return HomeworkMessage{}, fmt.Errorf("commit transaction: %w", err)
	}

	msg.ID = id
	msg.CreatedAt = now
	return msg, nil
}

// GetMessages returns all messages for a conversation, scoped to a specific kid.
func GetMessages(db *sql.DB, conversationID, kidID int64) ([]HomeworkMessage, error) {
	rows, err := db.Query(`
		SELECT m.id, m.conversation_id, m.role, m.content, m.help_level, m.image_path, m.created_at
		FROM homework_messages m
		JOIN homework_conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = ? AND c.kid_id = ?
		ORDER BY m.created_at ASC, m.id ASC`,
		conversationID, kidID,
	)
	if err != nil {
		return nil, fmt.Errorf("list homework messages: %w", err)
	}
	defer rows.Close()

	var msgs []HomeworkMessage
	for rows.Next() {
		var m HomeworkMessage
		var encContent, encImagePath string
		var helpLevel string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &encContent, &helpLevel, &encImagePath, &m.CreatedAt); err != nil {
			return nil, err
		}
		content, err := decryptField(encContent)
		if err != nil {
			return nil, fmt.Errorf("decrypt message content: %w", err)
		}
		m.Content = content
		imgPath, err := decryptField(encImagePath)
		if err != nil {
			return nil, fmt.Errorf("decrypt message image_path: %w", err)
		}
		m.ImagePath = imgPath
		m.HelpLevel = HelpLevel(helpLevel)
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []HomeworkMessage{}
	}
	return msgs, nil
}

// GetConversationsForParentReview returns conversations for a child with message counts
// and help-level breakdowns so parents can review how much assistance was used.
func GetConversationsForParentReview(db *sql.DB, kidID int64) ([]ConversationSummary, error) {
	rows, err := db.Query(`
		SELECT c.id, c.kid_id, c.subject, c.created_at, c.updated_at,
		       COUNT(m.id) AS message_count
		FROM homework_conversations c
		LEFT JOIN homework_messages m ON m.conversation_id = c.id
		WHERE c.kid_id = ?
		GROUP BY c.id
		ORDER BY c.updated_at DESC, c.id DESC`,
		kidID,
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations for review: %w", err)
	}
	defer rows.Close()

	var summaries []ConversationSummary
	idIndex := map[int64]int{}
	for rows.Next() {
		var s ConversationSummary
		var encSubject string
		if err := rows.Scan(&s.ID, &s.KidID, &encSubject, &s.CreatedAt, &s.UpdatedAt, &s.MessageCount); err != nil {
			return nil, err
		}
		subject, err := decryptField(encSubject)
		if err != nil {
			return nil, fmt.Errorf("decrypt conversation subject: %w", err)
		}
		s.Subject = subject
		s.HelpLevels = map[string]int{}
		idIndex[s.ID] = len(summaries)
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch help-level counts for all conversations in a single query.
	if len(summaries) > 0 {
		hlRows, err := db.Query(`
			SELECT m.conversation_id, m.help_level, COUNT(*)
			FROM homework_messages m
			JOIN homework_conversations c ON c.id = m.conversation_id
			WHERE c.kid_id = ? AND m.help_level != ''
			GROUP BY m.conversation_id, m.help_level`, kidID)
		if err != nil {
			return nil, fmt.Errorf("count help levels: %w", err)
		}
		defer hlRows.Close()

		for hlRows.Next() {
			var convID int64
			var level string
			var count int
			if err := hlRows.Scan(&convID, &level, &count); err != nil {
				return nil, err
			}
			if idx, ok := idIndex[convID]; ok {
				summaries[idx].HelpLevels[level] = count
			}
		}
		if err := hlRows.Err(); err != nil {
			return nil, err
		}
	}

	if summaries == nil {
		summaries = []ConversationSummary{}
	}
	return summaries, nil
}

// decryptField decrypts a field value. If the value has the "enc:" prefix but
// decryption fails, the error is propagated to avoid silently losing data.
// For legacy plaintext values (no "enc:" prefix), the value is returned as-is.
func decryptField(val string) (string, error) {
	if val == "" {
		return val, nil
	}
	decrypted, err := encryption.DecryptField(val)
	if err != nil {
		if len(val) >= 4 && val[:4] == "enc:" {
			return "", fmt.Errorf("decrypt encrypted field: %w", err)
		}
		log.Printf("homework: decrypt field warning (legacy plaintext): %v", err)
		return val, nil
	}
	return decrypted, nil
}
