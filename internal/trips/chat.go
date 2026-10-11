package trips

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/races"
	"github.com/Robin831/Hytte/internal/training"
)

// Trip chat: ask Claude about a trip, or have it change the trip. Each trip
// has one shared conversation. A question is answered in the background (a
// web-enabled Claude call can take longer than the proxy's request limit);
// the page polls until the answer is in. Claude sees the whole trip, its
// checklists and the family roster, and answers with a reply plus, for
// travellers who may edit, an updated trip and checklist operations. The
// conversation is deleted a month after the trip ends.

// ChatModel answers trip chats.
const ChatModel = "claude-sonnet-5-5"

const (
	chatHistory      = 20
	chatTimeout      = 4 * time.Minute
	maxChatMessage   = 20000
	chatRetention    = 30 * 24 * time.Hour
	chatCleanupEvery = 24 * time.Hour
)

var (
	ErrChatBusy = errors.New("Claude is still answering the last question")
)

// ChatMessage is one message in a trip's conversation.
type ChatMessage struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"` // user or assistant
	UserName  string `json:"user_name,omitempty"`
	Content   string `json:"content"`
	Status    string `json:"status"`  // pending, done or failed (assistant)
	Changed   bool   `json:"changed"` // the answer changed the trip
	CreatedAt string `json:"created_at"`
}

// ListChat returns a trip's conversation, oldest first.
func ListChat(ctx context.Context, db *sql.DB, tripID, userID int64) ([]ChatMessage, error) {
	canView, _, err := access(ctx, db, tripID, userID)
	if err != nil {
		return nil, err
	}
	if !canView {
		return nil, ErrNotFound
	}
	return listChat(ctx, db, tripID, 0)
}

func listChat(ctx context.Context, db *sql.DB, tripID int64, last int) ([]ChatMessage, error) {
	q := `SELECT m.id, m.role, COALESCE(u.name, ''), m.content, m.status, m.changed, m.created_at
		FROM trip_chat_messages m LEFT JOIN users u ON u.id = m.user_id WHERE m.trip_id = ? ORDER BY m.id`
	args := []any{tripID}
	if last > 0 {
		q = `SELECT * FROM (` + strings.Replace(q, "ORDER BY m.id", "ORDER BY m.id DESC LIMIT ?", 1) + `) ORDER BY id`
		args = append(args, last)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list trip chat: %w", err)
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var enc, name string
		if err := rows.Scan(&m.ID, &m.Role, &name, &enc, &m.Status, &m.Changed, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Content = encryption.DecryptLenient(enc)
		if m.Role == "user" {
			m.UserName = firstName(name)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Chat runs one question. Run and Config are injectable for tests.
type Chat struct {
	DB     *sql.DB
	Run    func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error)
	Config func(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, error)
	Now    func() time.Time
}

// NewChat wires the web-enabled Claude CLI and the shared Claude budget.
func NewChat(db *sql.DB) *Chat {
	return &Chat{DB: db, Run: training.RunPromptWithWebTools, Now: time.Now,
		Config: func(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, error) {
			return races.BackgroundClaude(ctx, db, ChatModel)
		}}
}

func insertChat(ctx context.Context, db *sql.DB, tripID int64, userID *int64, role, content, status string) (int64, error) {
	enc := ""
	if content != "" {
		var err error
		if enc, err = encryption.EncryptField(content); err != nil {
			return 0, err
		}
	}
	res, err := db.ExecContext(ctx, `INSERT INTO trip_chat_messages (trip_id, user_id, role, content, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, tripID, userID, role, enc, status, now())
	if err != nil {
		return 0, fmt.Errorf("save chat message: %w", err)
	}
	return res.LastInsertId()
}

// Ask stores the question and a pending answer. The caller then runs Answer
// (in the background) with the returned answer ID.
func (c *Chat) Ask(ctx context.Context, tripID, userID int64, message string) (int64, error) {
	message = strings.TrimSpace(message)
	if message == "" || len([]rune(message)) > maxChatMessage {
		return 0, invalid("the message must be 1–%d characters", maxChatMessage)
	}
	canView, _, err := access(ctx, c.DB, tripID, userID)
	if err != nil {
		return 0, err
	}
	if !canView {
		return 0, ErrNotFound
	}
	var pending int
	if err := c.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM trip_chat_messages WHERE trip_id = ? AND status = 'pending'`,
		tripID).Scan(&pending); err != nil {
		return 0, err
	}
	if pending > 0 {
		return 0, ErrChatBusy
	}
	uid := userID
	if _, err := insertChat(ctx, c.DB, tripID, &uid, "user", message, "done"); err != nil {
		return 0, err
	}
	return insertChat(ctx, c.DB, tripID, nil, "assistant", "", "pending")
}

// ChecklistOp is one checklist change Claude asks for.
type ChecklistOp struct {
	Op        string `json:"op"` // add_list, add_item, set_done, delete_item
	ListID    int64  `json:"list_id,omitempty"`
	ListTitle I18n   `json:"list_title,omitempty"` // add_list, or add_item into a list by title
	ItemID    int64  `json:"item_id,omitempty"`
	Title     I18n   `json:"title,omitempty"`
	Detail    I18n   `json:"detail,omitempty"`
	Urgent    bool   `json:"urgent,omitempty"`
	Done      bool   `json:"done,omitempty"`
}

// chatChange is the fenced block at the end of a reply that changes the trip.
type chatChange struct {
	Update    *TripInput    `json:"update"`
	Checklist []ChecklistOp `json:"checklist"`
}

type chatChecklist struct {
	ID    int64      `json:"id"`
	Title I18n       `json:"title"`
	Items []chatItem `json:"items"`
}

type chatItem struct {
	ID     int64  `json:"id"`
	Title  I18n   `json:"title"`
	Detail I18n   `json:"detail,omitempty"`
	Urgent bool   `json:"urgent,omitempty"`
	Done   bool   `json:"done"`
	DoneBy string `json:"done_by,omitempty"`
}

const chatPrompt = `You are the trip assistant inside Hytte, a family's private web app. You help with one trip: answer questions about it and, when asked, change it.

Now: %s (Europe/Oslo). You are talking to %s. Reply in %s. They %s.

The trip (what "update" replaces; booking references and phone numbers are the family's own):
%s

Checklists (change them only through "checklist" operations):
%s

The family (people who can travel; use person_id when adding a traveller):
%s

Conversation so far:
%s

New message from %s:
%s

How to answer:
- Answer from the trip data. For live facts (weather, opening hours, train times, prices, event times) you may search the web; say where an answer comes from when it matters, and say so when you are unsure. Keep replies short and practical; plain text, no markdown headings.
- To change the trip, put "update" in the block: the COMPLETE trip in the same shape as above (kind, start_date, end_date, home_tz, dest_tz, share_family, race_event_id, result_id, doc). Copy everything you don't change exactly. Change only what was asked, and say in the reply what you changed. Otherwise leave "update" null.
- Every text in the doc is an object {"nb": ..., "en": ..., "th": ...}: write all three languages for anything you add or change. Keep names, codes, flight numbers, addresses, phone numbers and prices as written.
- Dates are YYYY-MM-DD; local times are YYYY-MM-DDTHH:MM with their IANA time zone in the matching tz field. Route stops are airport codes or short place names. A tentative trip has doc.flex; don't change dates of a tentative trip unless asked.
- Checklist operations: {"op":"add_item","list_id":N,"title":{...},"detail":{...},"urgent":false} (or "list_title" instead of list_id to add to a new list), {"op":"add_list","list_title":{...}}, {"op":"set_done","item_id":N,"done":true}, {"op":"delete_item","item_id":N}.
- %s

Format: write your reply to them as plain text. Only when you change something, end the reply with ONE fenced json block holding the change, like
` + "```json\n{\"update\": <the complete trip or null>, \"checklist\": [<operations>]}\n```" + `
With no change, write no block at all.`

var langNames = map[string]string{"nb": "Norwegian bokmål", "en": "English", "th": "Thai"}

// splitReply separates the reply text from an optional change block (the
// convention Stride's coach thread uses too).
func splitReply(s string) (string, *chatChange, error) {
	text, block, ok := training.SplitFencedObject(s)
	if !ok {
		return text, nil, nil
	}
	var c chatChange
	if err := json.Unmarshal([]byte(block), &c); err != nil {
		return text, nil, fmt.Errorf("unreadable change block: %w", err)
	}
	return text, &c, nil
}

// Answer produces the pending answer for a trip's latest question.
func (c *Chat) Answer(ctx context.Context, tripID, userID, answerID int64, lang string) {
	content, changed, status := c.answer(ctx, tripID, userID, lang)
	enc, err := encryption.EncryptField(content)
	if err != nil {
		log.Printf("trips: encrypt chat answer: %v", err)
		enc = ""
		status = "failed"
	}
	if _, err := c.DB.ExecContext(context.Background(), `UPDATE trip_chat_messages SET content = ?, status = ?, changed = ? WHERE id = ?`,
		enc, status, changed, answerID); err != nil {
		log.Printf("trips: save chat answer: %v", err)
	}
}

func (c *Chat) answer(ctx context.Context, tripID, userID int64, lang string) (string, bool, string) {
	if langNames[lang] == "" {
		lang = "en"
	}
	failed := func(nb, en, th string) (string, bool, string) {
		return map[string]string{"nb": nb, "en": en, "th": th}[lang], false, "failed"
	}
	trip, err := GetTrip(ctx, c.DB, tripID, userID)
	if err != nil {
		return failed("Fant ikke reisen.", "Couldn't load the trip.", "โหลดทริปไม่ได้")
	}
	cfg, err := c.Config(ctx, c.DB)
	if err != nil {
		if errors.Is(err, races.ErrResearchBudget) || errors.Is(err, races.ErrResearchMonthly) {
			return failed("Dagens Claude-budsjett er brukt opp. Prøv igjen i morgen, eller øk budsjettet under Løp → Research.",
				"Today's Claude budget is used up. Try again tomorrow, or raise it under Races → Research.",
				"งบ Claude ของวันนี้หมดแล้ว ลองใหม่พรุ่งนี้ หรือเพิ่มงบในหน้า งานวิ่ง → Research")
		}
		return failed("Claude er ikke tilgjengelig akkurat nå.", "Claude isn't available right now.", "Claude ไม่พร้อมใช้งานในขณะนี้")
	}

	snapshot := trip.UpdatedAt
	input := TripInput{Kind: trip.Kind, StartDate: trip.StartDate, EndDate: trip.EndDate, HomeTZ: trip.HomeTZ, DestTZ: trip.DestTZ,
		ShareFamily: trip.ShareFamily, RaceEventID: trip.RaceEventID, ResultID: trip.ResultID, Doc: trip.Doc}
	tripJSON, _ := json.MarshalIndent(input, "", " ")
	lists := make([]chatChecklist, 0, len(trip.Checklists))
	for _, g := range trip.Checklists {
		cl := chatChecklist{ID: g.ID, Title: g.Title}
		for _, it := range g.Items {
			cl.Items = append(cl.Items, chatItem{ID: it.ID, Title: it.Title, Detail: it.Detail, Urgent: it.Urgent, Done: it.Done, DoneBy: it.DoneByName})
		}
		lists = append(lists, cl)
	}
	listsJSON, _ := json.MarshalIndent(lists, "", " ")
	people, _ := ListPeople(ctx, c.DB)
	peopleJSON, _ := json.Marshal(people)

	history, err := listChat(ctx, c.DB, tripID, chatHistory+2)
	if err != nil {
		return failed("Kunne ikke lese samtalen.", "Couldn't read the conversation.", "อ่านบทสนทนาไม่ได้")
	}
	var question, asker string
	var past strings.Builder
	for _, m := range history {
		if m.Role == "assistant" && m.Status == "pending" {
			continue
		}
		if m.Role == "user" {
			question, asker = m.Content, m.UserName
		}
		who := "Assistant"
		if m.Role == "user" {
			who = m.UserName
		}
		fmt.Fprintf(&past, "%s: %s\n", who, m.Content)
	}
	// The latest user message is the question, not history.
	pastText := strings.TrimSuffix(past.String(), fmt.Sprintf("%s: %s\n", asker, question))
	if strings.TrimSpace(pastText) == "" {
		pastText = "(none)"
	}

	var viewer string
	_ = c.DB.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&viewer)
	viewer = firstName(viewer)
	rights := "can view this trip but not change it"
	rule := `They can't edit: never write a change block; if they ask for a change, explain that only travellers and the owner can change the trip.`
	if trip.CanEdit {
		rights = "can change this trip"
		rule = "If a request is unclear or would remove bookings, ask before changing anything."
	}
	prompt := fmt.Sprintf(chatPrompt, c.Now().In(oslo).Format("Monday 2 January 2006 15:04"), viewer, langNames[lang], rights,
		tripJSON, listsJSON, peopleJSON, pastText, viewer, question, rule)

	callCtx, cancel := context.WithTimeout(ctx, chatTimeout)
	defer cancel()
	text, cost, err := c.Run(callCtx, cfg, prompt)
	if err != nil {
		races.LogBackgroundRun(ctx, c.DB, "trip_chat", userID, cost, 0, "trip chat", err.Error())
		return failed("Claude svarte ikke. Prøv igjen.", "Claude didn't answer. Try again.", "Claude ไม่ตอบ ลองใหม่อีกครั้ง")
	}
	content, change, perr := splitReply(text)
	if content == "" && change == nil {
		races.LogBackgroundRun(ctx, c.DB, "trip_chat", userID, cost, 0, "trip chat", "empty reply")
		return failed("Claude svarte ikke. Prøv igjen.", "Claude didn't answer. Try again.", "Claude ไม่ตอบ ลองใหม่อีกครั้ง")
	}
	changes := 0
	saveErr := perr
	if saveErr == nil && change != nil && trip.CanEdit && (change.Update != nil || len(change.Checklist) > 0) {
		changes, saveErr = c.apply(ctx, tripID, userID, snapshot, *change)
	}
	if saveErr != nil {
		content += "\n\n" + map[string]string{
			"nb": "⚠️ Endringen ble ikke lagret: ",
			"en": "⚠️ The change wasn't saved: ",
			"th": "⚠️ ไม่ได้บันทึกการเปลี่ยนแปลง: ",
		}[lang] + saveErr.Error()
	}
	races.LogBackgroundRun(ctx, c.DB, "trip_chat", userID, cost, changes, "trip chat", "")
	return content, changes > 0, "done"
}

// apply saves Claude's changes; the trip must not have changed meanwhile.
func (c *Chat) apply(ctx context.Context, tripID, userID int64, snapshot string, r chatChange) (int, error) {
	var current string
	if err := c.DB.QueryRowContext(ctx, `SELECT updated_at FROM trips WHERE id = ?`, tripID).Scan(&current); err != nil {
		return 0, err
	}
	if current != snapshot {
		return 0, errors.New("the trip was changed while Claude was answering; ask again")
	}
	changes := 0
	if r.Update != nil {
		if err := UpdateTrip(ctx, c.DB, tripID, userID, *r.Update); err != nil {
			return 0, err
		}
		changes++
	}
	newLists := map[string]int64{}
	for _, op := range r.Checklist {
		var err error
		switch op.Op {
		case "add_list":
			var id int64
			id, err = AddChecklist(ctx, c.DB, tripID, userID, ChecklistInput{Title: op.ListTitle})
			newLists[firstText(op.ListTitle)] = id
		case "add_item":
			list := op.ListID
			if list == 0 {
				key := firstText(op.ListTitle)
				if id, ok := newLists[key]; ok {
					list = id
				} else if list, err = AddChecklist(ctx, c.DB, tripID, userID, ChecklistInput{Title: op.ListTitle}); err == nil {
					newLists[key] = list
				}
			}
			if err == nil {
				if gt, gerr := groupTrip(ctx, c.DB, list); gerr != nil || gt != tripID {
					err = invalid("checklist %d isn't on this trip", list)
				} else {
					_, err = AddItem(ctx, c.DB, list, userID, ItemInput{Title: op.Title, Detail: op.Detail, Urgent: op.Urgent})
				}
			}
		case "set_done", "delete_item":
			if it, ierr := itemTrip(ctx, c.DB, op.ItemID); ierr != nil || it != tripID {
				err = invalid("checklist item %d isn't on this trip", op.ItemID)
			} else if op.Op == "set_done" {
				err = SetDone(ctx, c.DB, op.ItemID, userID, op.Done)
			} else {
				err = DeleteItem(ctx, c.DB, op.ItemID, userID)
			}
		default:
			err = invalid("unknown checklist operation %q", op.Op)
		}
		if err != nil {
			return changes, err
		}
		changes++
	}
	if changes > 0 {
		TranslateSoon(c.DB, tripID, userID)
	}
	return changes, nil
}

func firstText(t I18n) string {
	for _, l := range Languages {
		if t[l] != "" {
			return t[l]
		}
	}
	return ""
}

// ClearChat deletes a trip's conversation (editors only).
func ClearChat(ctx context.Context, db *sql.DB, tripID, userID int64) error {
	if err := requireEdit(ctx, db, tripID, userID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM trip_chat_messages WHERE trip_id = ? AND status != 'pending'`, tripID)
	return err
}

// CleanupChats deletes conversations of trips that ended more than a month
// ago, and fails answers left pending by a restart.
func CleanupChats(ctx context.Context, db *sql.DB, now time.Time, startup bool) (int64, error) {
	if startup {
		if _, err := db.ExecContext(ctx, `UPDATE trip_chat_messages SET status = 'failed' WHERE status = 'pending'`); err != nil {
			return 0, err
		}
	}
	cutoff := now.Add(-chatRetention).In(oslo).Format("2006-01-02")
	res, err := db.ExecContext(ctx, `DELETE FROM trip_chat_messages WHERE trip_id IN (SELECT id FROM trips WHERE end_date < ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RunChatCleanupLoop runs CleanupChats at start and then daily.
func RunChatCleanupLoop(ctx context.Context, db *sql.DB) {
	startup := true
	for {
		if n, err := CleanupChats(ctx, db, time.Now(), startup); err != nil {
			log.Printf("trips: chat cleanup: %v", err)
		} else if n > 0 {
			log.Printf("trips: deleted %d chat messages of finished trips", n)
		}
		startup = false
		select {
		case <-ctx.Done():
			return
		case <-time.After(chatCleanupEvery):
		}
	}
}
