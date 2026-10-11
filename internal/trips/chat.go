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
// checklists, prices, the family roster, what it has been told to remember
// about the family, and a description of how the trip page works; it answers
// with a reply plus, for travellers who may edit, a change block. Pictures
// attached to a question are read by a separate vision pass first (see
// chat_images.go). The conversation is deleted a month after the trip ends.

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
	ID        int64   `json:"id"`
	Role      string  `json:"role"` // user or assistant
	UserName  string  `json:"user_name,omitempty"`
	Content   string  `json:"content"`
	Status    string  `json:"status"`  // pending, done or failed (assistant)
	Changed   bool    `json:"changed"` // the answer changed the trip
	Images    []int64 `json:"images"`  // attached pictures (IDs)
	CreatedAt string  `json:"created_at"`
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
	out := []ChatMessage{}
	index := map[int64]int{}
	for rows.Next() {
		var m ChatMessage
		var enc, name string
		if err := rows.Scan(&m.ID, &m.Role, &name, &enc, &m.Status, &m.Changed, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		m.Content = encryption.DecryptLenient(enc)
		m.Images = []int64{}
		if m.Role == "user" {
			m.UserName = firstName(name)
		}
		index[m.ID] = len(out)
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	irows, err := db.QueryContext(ctx, `SELECT id, message_id FROM trip_chat_images WHERE trip_id = ? ORDER BY id`, tripID)
	if err != nil {
		return nil, fmt.Errorf("list chat pictures: %w", err)
	}
	defer irows.Close()
	for irows.Next() {
		var id, msg int64
		if err := irows.Scan(&id, &msg); err != nil {
			return nil, err
		}
		if i, ok := index[msg]; ok {
			out[i].Images = append(out[i].Images, id)
		}
	}
	return out, irows.Err()
}

// Chat runs one question. Run, Describe and Config are injectable for tests.
type Chat struct {
	DB       *sql.DB
	Run      func(ctx context.Context, cfg *training.ClaudeConfig, prompt string) (string, float64, error)
	Describe func(ctx context.Context, cfg *training.ClaudeConfig, prompt, imagePath string) (string, error)
	Config   func(ctx context.Context, db *sql.DB) (*training.ClaudeConfig, error)
	Now      func() time.Time
}

// NewChat wires the web-enabled Claude CLI and the shared Claude budget.
func NewChat(db *sql.DB) *Chat {
	return &Chat{DB: db, Run: training.RunPromptWithWebTools, Describe: training.RunPromptWithImage, Now: time.Now,
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

// Ask stores the question (with any pictures) and a pending answer. The
// caller then runs Answer (in the background) with the returned answer ID.
func (c *Chat) Ask(ctx context.Context, tripID, userID int64, message string, images []ImageInput) (int64, error) {
	message = strings.TrimSpace(message)
	if len([]rune(message)) > maxChatMessage || (message == "" && len(images) == 0) {
		return 0, invalid("the message must be 1–%d characters", maxChatMessage)
	}
	decoded, err := decodeImages(images)
	if err != nil {
		return 0, err
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
	if message == "" {
		message = "📷"
	}
	uid := userID
	msgID, err := insertChat(ctx, c.DB, tripID, &uid, "user", message, "done")
	if err != nil {
		return 0, err
	}
	if err := saveImages(ctx, c.DB, tripID, msgID, decoded); err != nil {
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

// chatChange is the fenced block at the end of a reply that changes things.
type chatChange struct {
	Update       *TripInput     `json:"update"`
	Checklist    []ChecklistOp  `json:"checklist"`
	SearchPrices *SearchOptions `json:"search_prices"`
	Remember     []string       `json:"remember"`
	Forget       []int64        `json:"forget"`
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

// chatKnowledge is what the assistant is told about the app itself: what
// the family sees, what each part of a trip is for, and what it can't do.
const chatKnowledge = `How Hytte trips work:
- The trip page shows, top to bottom: title, dates, route chips, travellers, a "Now" box (a countdown before the trip; during it the current phase, today's flights, tonight's stay and today's plan; afterwards the result), clocks for home and the destination, then sections that appear only when they have content: Prices, Costs, To do (checklists), Day by day, Flights, Stays, Transport, Contacts & references, Documents, Notes, After the trip.
- Nobody edits a trip by hand: every change goes through you. The only thing they do themselves is tick checklist items. So when they tell you something happened or changed, updating the trip is your job.
- doc.phases split a trip into named parts with dates (outbound, a city, return); flights, stays, checklists and notes can carry a phase key, and the "Now" box shows the phase that covers today. doc.days is the day-by-day plan with timed steps ("key": true highlights a step). doc.contacts are phone numbers and booking references with copy buttons ("urgent": true marks emergency numbers). doc.documents are things to bring or have ready (status todo/done). doc.followups are things to do after the trip (claims, refunds). doc.notes are free text.
- doc.stays: one entry per hotel visit with check_in and check_out local times and tz. A booking that is split or moved becomes one entry per visit, same booking_ref.
- doc.destination {"city", "airports": [IATA codes], "hotel_area"} is what price searches use. doc.costs is the money: {"category": flights|stay|transport|tickets|food|other, "label": {...}, "amount", "currency": "NOK"/"EUR"/"THB"/..., "status": estimate|booked|paid}; the page totals them in NOK. When something is booked, record the cost (status booked) and fill in the flight or stay.
- Price searches use Google Flights and Google Hotels and are limited each month; results appear in the Prices section within a minute, each with a link to Google where the family books. Flights are searched as round trips from the home airport for everyone on the trip.
- You cannot: create or delete trips ("New trip" on the Trips page, "Delete trip" at the bottom of a trip), give someone access to a feature (Robin does that under Admin), book or pay for anything, or read their calendar or email. Say so plainly and tell them where it is done.`

const chatRules = `How to answer:
- Act, don't interrogate. When a request is clear enough to make a sensible change, make it in this reply and state any assumption in one line so they can correct you. Ask a question only when a wrong guess would lose a booking, or you truly cannot tell what they mean — then ask one question, once. Never ask for something the conversation, the trip or a picture already tells you.
- Take what they tell you as fact (a hotel agreed by phone, plans changed, who is driving): record it, don't ask for proof. In a trip that is under way, days before today have happened: don't re-plan them, and reason from today's date.
- You have web search and can open web pages. Use them before saying you can't find something (a hotel's price on booking sites, opening hours, transport times, event times). Say where an answer comes from when it matters, and say so when a page wouldn't load or you are unsure.
- Keep replies short and practical: plain text, no markdown headings, no closing boilerplate such as "I haven't changed anything". Say what you changed, in a line or two.
- To change the trip, put "update" in the block: the COMPLETE trip in the same shape as above (kind, start_date, end_date, home_tz, dest_tz, share_family, race_event_id, result_id, doc). Copy everything you don't change exactly. Change only what was asked.
- Every text in the doc is an object {"nb": ..., "en": ..., "th": ...}: write all three languages for anything you add or change. Keep names, codes, flight numbers, addresses, phone numbers and prices as written.
- Dates are YYYY-MM-DD; local times are YYYY-MM-DDTHH:MM with their IANA time zone in the matching tz field. Route stops are airport codes or short place names. A tentative trip has doc.flex; to pick dates set start_date/end_date and doc.flex.chosen true.
- Checklist operations go in "checklist": {"op":"add_item","list_id":N,"title":{...},"detail":{...},"urgent":false} (or "list_title" instead of list_id for a new list), {"op":"add_list","list_title":{...}}, {"op":"set_done","item_id":N,"done":true}, {"op":"delete_item","item_id":N}.
- To check prices add "search_prices": {"dates": [{"start","end"}], "kinds": ["flights","hotels"], "hotel_query": "..."}. Empty dates = the trip's own dates (or a tentative trip's candidate dates). For one named hotel, or hotels in an area, use "kinds": ["hotels"] with "hotel_query" (the hotel's name, or e.g. "hotels near Suvarnabhumi Airport") — never search flights nobody asked for. Each search of each kind costs one of the month's searches; reuse prices already found.
- To remember a lasting fact or preference about the family that will matter on other trips (who is who, how they like to travel), add "remember": ["one short sentence", ...]; to drop one, "forget": [id]. Trip details belong in the trip, not in memory. Never store booking references, document numbers or anything secret.`

const chatFormat = `Format: write your reply to them as plain text. Only when you change, search or remember something, end the reply with ONE fenced json block, like
` + "```json\n{\"update\": <the complete trip or null>, \"checklist\": [], \"search_prices\": null, \"remember\": [], \"forget\": []}\n```" + `
Otherwise write no block at all.`

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

// tripStatus says where the trip is relative to today, in a sentence.
func tripStatus(t *Trip, now time.Time) string {
	loc, err := time.LoadLocation(t.DestTZ)
	if err != nil {
		loc = oslo
	}
	today := now.In(loc).Format("2006-01-02")
	days := func(a, b string) int {
		ta, _ := time.Parse("2006-01-02", a)
		tb, _ := time.Parse("2006-01-02", b)
		return int(tb.Sub(ta).Hours() / 24)
	}
	switch {
	case isTentativeTrip(t):
		return fmt.Sprintf("The dates are tentative: somewhere between %s and %s, %d nights.", t.Doc.Flex.From, t.Doc.Flex.To, t.Doc.Flex.Nights)
	case today < t.StartDate:
		return fmt.Sprintf("The trip has not started: it begins in %d days (%s).", days(today, t.StartDate), t.StartDate)
	case today > t.EndDate:
		return fmt.Sprintf("The trip is over: it ended %d days ago (%s).", days(t.EndDate, today), t.EndDate)
	}
	s := fmt.Sprintf("The trip is under way: today (%s at the destination) is day %d of %d.", today, days(t.StartDate, today)+1, days(t.StartDate, t.EndDate)+1)
	for _, p := range t.Doc.Phases {
		if p.Start <= today && today <= p.End {
			s += fmt.Sprintf(" Current phase: %s.", firstText(p.Title))
			break
		}
	}
	return s
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

	history, err := listChat(ctx, c.DB, tripID, chatHistory+2)
	if err != nil {
		return failed("Kunne ikke lese samtalen.", "Couldn't read the conversation.", "อ่านบทสนทนาไม่ได้")
	}
	// Read any pictures on the question before the main call.
	descriptions := c.describeImages(ctx, cfg, tripID, userID, history)

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

	var question string
	var past []string
	for _, m := range history {
		if m.Role == "assistant" && m.Status == "pending" {
			continue
		}
		who := "Assistant"
		if m.Role == "user" {
			who = m.UserName
		}
		line := who + ": " + m.Content
		for _, id := range m.Images {
			if d := descriptions[id]; d != "" {
				line += "\n  [picture: " + d + "]"
			}
		}
		if m.Role == "user" {
			question = line
		}
		past = append(past, line)
	}
	// The latest user message is the question, not history.
	if n := len(past); n > 0 && past[n-1] == question {
		past = past[:n-1]
	}
	pastText := strings.Join(past, "\n")
	if strings.TrimSpace(pastText) == "" {
		pastText = "(none)"
	}

	var viewer string
	_ = c.DB.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&viewer)
	viewer = firstName(viewer)
	rights := "can change this trip"
	if !trip.CanEdit {
		rights = "can view this trip but not change it: never write a change block for them; if they ask for a change, explain that only travellers and the owner can change the trip"
	}
	pricesText, home := c.pricesContext(ctx, tripID, userID)
	homeCity := "Bergen"
	if set, err := races.LoadResearchSettings(ctx, c.DB); err == nil && set.HomeCity != "" {
		homeCity = set.HomeCity
	}
	nowT := c.Now()

	var b strings.Builder
	fmt.Fprintf(&b, "You are the trip assistant inside Hytte, a family's private web app. You help with one trip: answer questions about it and change it when asked.\n\n")
	fmt.Fprintf(&b, "Now: %s (Europe/Oslo). You are talking to %s, who %s. Reply in %s.\n", nowT.In(oslo).Format("Monday 2 January 2006 15:04"), viewer, rights, langNames[lang])
	fmt.Fprintf(&b, "%s\nHome is %s (airport %s).\n\n", tripStatus(trip, nowT), homeCity, home)
	b.WriteString(chatKnowledge + "\n\n")
	fmt.Fprintf(&b, "What you remember about this family (kept across all their trips):\n%s\n\n", notesContext(ctx, c.DB))
	fmt.Fprintf(&b, "The trip (what \"update\" replaces; booking references and phone numbers are the family's own):\n%s\n\n", tripJSON)
	fmt.Fprintf(&b, "Checklists (change them only through \"checklist\" operations):\n%s\n\n", listsJSON)
	fmt.Fprintf(&b, "The family (people who can travel; use person_id when adding a traveller):\n%s\n\n", peopleJSON)
	fmt.Fprintf(&b, "Their other trips:\n%s\n\n", c.otherTrips(ctx, tripID, userID))
	fmt.Fprintf(&b, "Prices found so far (Google Flights/Hotels, NOK, totals for all travellers):\n%s\n\n", pricesText)
	fmt.Fprintf(&b, "Conversation so far (pictures they sent were read for you; what they show is in [picture: ...]):\n%s\n\n", pastText)
	fmt.Fprintf(&b, "New message from %s\n\n", question)
	b.WriteString(chatRules + "\n\n" + chatFormat)

	callCtx, cancel := context.WithTimeout(ctx, chatTimeout)
	defer cancel()
	text, cost, err := c.Run(callCtx, cfg, b.String())
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
	if saveErr == nil && change != nil && trip.CanEdit {
		applyNotes(ctx, c.DB, userID, change.Remember, change.Forget)
	}
	if saveErr == nil && change != nil && change.SearchPrices != nil && trip.CanEdit {
		n, err := NewSearcher(c.DB).Start(ctx, tripID, userID, *change.SearchPrices)
		switch {
		case err != nil:
			content += "\n\n" + map[string]string{"nb": "⚠️ Prissøket startet ikke: ", "en": "⚠️ The price search didn't start: ",
				"th": "⚠️ การค้นหาราคาไม่ได้เริ่ม: "}[lang] + err.Error()
		case n > 0:
			changes++
			content += "\n\n" + map[string]string{"nb": "🔎 Prissøk startet – resultatene dukker opp under Priser om et minutt.",
				"en": "🔎 Price search started – results appear under Prices in a minute.",
				"th": "🔎 เริ่มค้นหาราคาแล้ว – ผลจะแสดงในส่วนราคาภายในหนึ่งนาที"}[lang]
		}
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

// otherTrips lists the viewer's other trips, one per line.
func (c *Chat) otherTrips(ctx context.Context, tripID, userID int64) string {
	list, err := ListTrips(ctx, c.DB, userID)
	if err != nil {
		return "(unknown)"
	}
	var lines []string
	for _, t := range list {
		if t.ID == tripID {
			continue
		}
		line := fmt.Sprintf("- %s (%s, %s to %s)", firstText(t.Title), t.Kind, t.StartDate, t.EndDate)
		if t.Tentative {
			line += " — tentative dates"
		}
		lines = append(lines, line)
		if len(lines) == 12 {
			break
		}
	}
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, "\n")
}

// pricesContext is the latest prices, compact, for the prompt.
func (c *Chat) pricesContext(ctx context.Context, tripID, userID int64) (string, string) {
	v, err := NewSearcher(c.DB).Prices(ctx, tripID, userID)
	if err != nil {
		return "(none)", "BGO"
	}
	type brief struct {
		Kind        string          `json:"kind"`
		Label       string          `json:"hotel_query,omitempty"`
		Start       string          `json:"start"`
		End         string          `json:"end"`
		Status      string          `json:"status"`
		Searched    string          `json:"searched_at"`
		Cheapest    *float64        `json:"cheapest,omitempty"`
		Level       string          `json:"google_price_level,omitempty"`
		TypicalLow  *float64        `json:"typical_low,omitempty"`
		TypicalHigh *float64        `json:"typical_high,omitempty"`
		Top         json.RawMessage `json:"top_options,omitempty"`
		Link        string          `json:"google_link,omitempty"`
	}
	var out []brief
	for _, q := range v.Quotes {
		var opts []json.RawMessage
		_ = json.Unmarshal(q.Options, &opts)
		if len(opts) > 5 {
			opts = opts[:5]
		}
		top, _ := json.Marshal(opts)
		out = append(out, brief{q.Kind, q.Label, q.Start, q.End, q.Status, q.SearchedAt, q.Cheapest, q.PriceLevel, q.TypicalLow, q.TypicalHigh, top, q.Link})
	}
	if len(out) == 0 {
		return fmt.Sprintf("(none yet; %d of %d searches used this month)", v.Quota.Used, v.Quota.Limit), v.Home
	}
	b, _ := json.Marshal(map[string]any{"quotes": out, "passengers": v.Passengers,
		"searches_used_this_month": v.Quota.Used, "monthly_limit": v.Quota.Limit})
	return string(b), v.Home
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
	for _, l := range []string{"en", "nb", "th"} {
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
