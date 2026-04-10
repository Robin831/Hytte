package homework

// HelpLevel describes how much assistance a message provides.
type HelpLevel string

const (
	HelpLevelHint        HelpLevel = "hint"
	HelpLevelExplain     HelpLevel = "explain"
	HelpLevelWalkthrough HelpLevel = "walkthrough"
	HelpLevelAnswer      HelpLevel = "answer"
)

// ValidHelpLevels is the set of allowed help level values.
var ValidHelpLevels = map[HelpLevel]bool{
	HelpLevelHint:        true,
	HelpLevelExplain:     true,
	HelpLevelWalkthrough: true,
	HelpLevelAnswer:      true,
}

// HomeworkProfile stores a child's academic context for personalised help.
type HomeworkProfile struct {
	ID                int64    `json:"id"`
	KidID             int64    `json:"kid_id"`
	Age               int      `json:"age"`
	GradeLevel        string   `json:"grade_level"`
	Subjects          []string `json:"subjects"`
	PreferredLanguage string   `json:"preferred_language"`
	SchoolName        string   `json:"school_name"`
	CurrentTopics     []string `json:"current_topics"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

// HomeworkConversation groups messages about a single homework topic.
type HomeworkConversation struct {
	ID                 int64  `json:"id"`
	KidID              int64  `json:"kid_id"`
	Subject            string `json:"subject"`
	LastMessagePreview string `json:"last_message_preview,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

// HomeworkMessage is a single turn in a homework conversation.
type HomeworkMessage struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	HelpLevel      HelpLevel `json:"help_level,omitempty"`
	ImagePath      string    `json:"image_path,omitempty"`
	CreatedAt      string    `json:"created_at"`
}

// ConversationSummary extends HomeworkConversation with review info for parents.
type ConversationSummary struct {
	HomeworkConversation
	MessageCount int            `json:"message_count"`
	HelpLevels   map[string]int `json:"help_levels"`
}
