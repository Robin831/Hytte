package offers

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/push"
)

type sentPush struct {
	userID int64
	note   push.Notification
}

// fakeSender records every push and fails for users listed in failFor.
type fakeSender struct {
	sent    []sentPush
	failFor map[int64]bool
}

func (f *fakeSender) send(_ *sql.DB, userID int64, payload []byte) error {
	if f.failFor[userID] {
		return errors.New("push endpoint unreachable")
	}
	var n push.Notification
	if err := json.Unmarshal(payload, &n); err != nil {
		return err
	}
	f.sent = append(f.sent, sentPush{userID: userID, note: n})
	return nil
}

func (f *fakeSender) forUser(userID int64) []sentPush {
	var out []sentPush
	for _, s := range f.sent {
		if s.userID == userID {
			out = append(out, s)
		}
	}
	return out
}

func testNotifier(db *sql.DB, sender *fakeSender, quiet map[int64]bool) *Notifier {
	return &Notifier{
		DB:              db,
		Send:            sender.send,
		InQuietHours:    func(_ *sql.DB, userID int64, _ time.Time) bool { return quiet[userID] },
		VAPIDConfigured: func(*sql.DB) (bool, error) { return true, nil },
	}
}

// seedNotifyUser makes userID fully eligible unless the caller strips one of
// the prerequisites afterwards.
func seedNotifyUser(t *testing.T, db *sql.DB, userID int64, keywords ...string) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO users (id, email, name, picture, google_id, created_at)
		VALUES (?, ?, 'U', '', ?, '')`, userID, fmt.Sprintf("u%d@example.com", userID), fmt.Sprintf("g%d", userID)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := auth.SetUserFeature(db, userID, "offers", true); err != nil {
		t.Fatalf("set feature: %v", err)
	}
	if err := auth.SetPreference(db, userID, NotifyPreferenceKey, "true"); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	for _, k := range keywords {
		if _, err := AddWatchlist(db, userID, k); err != nil {
			t.Fatalf("add watchlist %q: %v", k, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, created_at)
		VALUES (?, ?, 'p', 'a', '')`, userID, fmt.Sprintf("https://push.example/%d", userID)); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
}

func headingOffer(id, heading string) Offer {
	o := testOffer(id, 10, 20)
	o.Heading = heading
	return o
}

func storeOffers(t *testing.T, db *sql.DB, offers ...Offer) []string {
	t.Helper()
	inserted, err := UpsertOffers(context.Background(), db, offers)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return inserted
}

func notifiedIDs(t *testing.T, db *sql.DB, userID int64, ids []string) map[string]bool {
	t.Helper()
	got, err := NotifiedOfferIDs(context.Background(), db, userID, ids)
	if err != nil {
		t.Fatalf("notified lookup: %v", err)
	}
	return got
}

var notifyNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func TestNotifyOnePushPerUserWithCountAndKeywords(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk", "kaffe", "laks", "brød")
	ids := storeOffers(t, db,
		headingOffer("a", "TINE Helmelk"),
		headingOffer("b", "Lettmelk 1L"),
		headingOffer("c", "Evergood Kaffe"),
		headingOffer("d", "Røkt laks"),
		headingOffer("e", "Bananer"),
	)

	sender := &fakeSender{}
	if err := testNotifier(db, sender, nil).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d pushes, want 1: %+v", len(sender.sent), sender.sent)
	}
	n := sender.sent[0].note
	if n.Body != "4 new matches: melk, kaffe, laks" {
		t.Errorf("body = %q", n.Body)
	}
	if n.URL != "/offers" {
		t.Errorf("url = %q, want /offers", n.URL)
	}
	if n.Title == "" {
		t.Error("title is empty")
	}
	marked := notifiedIDs(t, db, 1, ids)
	for _, id := range []string{"a", "b", "c", "d"} {
		if !marked[id] {
			t.Errorf("offer %s not marked notified", id)
		}
	}
	if marked["e"] {
		t.Error("non-matching offer e was marked notified")
	}
}

func TestNotifySingularBody(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{}
	if err := testNotifier(db, sender, nil).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].note.Body != "1 new match: melk" {
		t.Fatalf("sent = %+v", sender.sent)
	}
}

func TestNotifyDeduplicatesAcrossRuns(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"), headingOffer("b", "Lettmelk"))

	sender := &fakeSender{}
	n := testNotifier(db, sender, nil)
	if err := n.Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("first notify: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("first run sent %d, want 1", len(sender.sent))
	}

	// Same ids again (e.g. an admin refresh racing the scheduler): nothing.
	if err := n.Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("second notify: %v", err)
	}
	// No new offers: nothing.
	if err := n.Notify(context.Background(), nil, notifyNow); err != nil {
		t.Fatalf("empty notify: %v", err)
	}
	// Re-syncing the same catalog inserts nothing, so nothing is passed on.
	if again := storeOffers(t, db, headingOffer("a", "Helmelk")); len(again) != 0 {
		t.Fatalf("re-upsert reported inserted %v", again)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("total sent %d after re-runs, want 1", len(sender.sent))
	}

	// A mix of an old and a new match only counts the new one.
	more := storeOffers(t, db, headingOffer("c", "Skummet melk"))
	if err := n.Notify(context.Background(), append([]string{"a"}, more...), notifyNow); err != nil {
		t.Fatalf("third notify: %v", err)
	}
	if len(sender.sent) != 2 || sender.sent[1].note.Body != "1 new match: melk" {
		t.Fatalf("sent = %+v", sender.sent)
	}
}

func TestNotifySkipsIneligibleUsers(t *testing.T) {
	db := setupTestDB(t)
	// 1: eligible control. 2: preference off. 3: preference missing.
	// 4: empty watchlist. 5: no subscription. 6: feature disabled.
	// 7: admin without explicit feature row (admins bypass feature checks).
	for id := int64(1); id <= 7; id++ {
		if id == 4 {
			seedNotifyUser(t, db, id)
		} else {
			seedNotifyUser(t, db, id, "melk")
		}
	}
	if err := auth.SetPreference(db, 2, NotifyPreferenceKey, "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM user_preferences WHERE user_id = 3 AND key = ?", NotifyPreferenceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM push_subscriptions WHERE user_id = 5"); err != nil {
		t.Fatal(err)
	}
	if err := auth.SetUserFeature(db, 6, "offers", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM user_features WHERE user_id = 7"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE users SET is_admin = 1 WHERE id = 7"); err != nil {
		t.Fatal(err)
	}
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{}
	if err := testNotifier(db, sender, nil).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify: %v", err)
	}
	var users []int64
	for _, s := range sender.sent {
		users = append(users, s.userID)
	}
	if !reflect.DeepEqual(users, []int64{1, 7}) {
		t.Fatalf("pushed users = %v, want [1 7]", users)
	}
}

func TestNotifyQuietHoursSuppressAndLeaveUnmarked(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	seedNotifyUser(t, db, 2, "melk")
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{}
	if err := testNotifier(db, sender, map[int64]bool{1: true}).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(sender.forUser(1)) != 0 {
		t.Errorf("user in quiet hours got a push")
	}
	if len(sender.forUser(2)) != 1 {
		t.Errorf("user outside quiet hours got %d pushes, want 1", len(sender.forUser(2)))
	}
	if notifiedIDs(t, db, 1, ids)["a"] {
		t.Error("offer marked notified for a user in quiet hours")
	}
}

func TestNotifySenderErrorLeavesUnmarkedAndContinues(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	seedNotifyUser(t, db, 2, "melk")
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{failFor: map[int64]bool{1: true}}
	if err := testNotifier(db, sender, nil).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify returned %v; per-user errors must only be logged", err)
	}
	if notifiedIDs(t, db, 1, ids)["a"] {
		t.Error("offer marked notified despite a failed send")
	}
	if len(sender.forUser(2)) != 1 || !notifiedIDs(t, db, 2, ids)["a"] {
		t.Error("second user was not notified after the first user's send failed")
	}

	// The failed user is retried on the next pass with the same ids.
	sender.failFor = nil
	if err := testNotifier(db, sender, nil).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("retry notify: %v", err)
	}
	if len(sender.forUser(1)) != 1 || len(sender.forUser(2)) != 1 {
		t.Errorf("after retry: user1=%d user2=%d pushes, want 1 each", len(sender.forUser(1)), len(sender.forUser(2)))
	}
}

func TestNotifyMissingVAPIDIsNoop(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{}
	n := testNotifier(db, sender, nil)
	n.VAPIDConfigured = vapidKeysExist // fresh DB has no vapid_keys row
	if err := n.Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent %d pushes without VAPID keys", len(sender.sent))
	}
	if notifiedIDs(t, db, 1, ids)["a"] {
		t.Error("offer marked notified without a send")
	}
}

func TestNotifyMatchingParityWithRank(t *testing.T) {
	db := setupTestDB(t)
	keywords := []string{"melk", "kaffe", "vaskemiddel", "grandiosa pizza", "Øl"}
	seedNotifyUser(t, db, 1, keywords...)

	offers := []Offer{
		headingOffer("p1", "TINE HELMELK"),
		headingOffer("p2", "Sjokolademelk"),
		headingOffer("p3", "Melkesjokolade"),
		headingOffer("p4", "Kaffefilter nr 4"),
		headingOffer("p5", "Evergood kaffe"),
		headingOffer("p6", "OMO Tøyvaskemiddel"),
		headingOffer("p7", "Grandiosa Pizza Original"),
		headingOffer("p8", "Grandiosa"),
		headingOffer("p9", "Lettøl 6pk"),
		headingOffer("p10", "Ølglass"),
		{ID: "p11", Heading: "Frokost", Description: "Filterkaffe, 250g"},
		headingOffer("p12", "Bananer"),
	}
	for i := range offers {
		if offers[i].RunFrom == "" {
			base := testOffer(offers[i].ID, 10, 0)
			base.Heading, base.Description = offers[i].Heading, offers[i].Description
			offers[i] = base
		}
	}
	ids := storeOffers(t, db, offers...)

	var want []string
	for _, r := range Rank(offers, keywords) {
		if len(r.MatchedKeywords) > 0 {
			want = append(want, r.ID)
		}
	}
	sort.Strings(want)

	sender := &fakeSender{}
	if err := testNotifier(db, sender, nil).Notify(context.Background(), ids, notifyNow); err != nil {
		t.Fatalf("notify: %v", err)
	}
	marked := notifiedIDs(t, db, 1, ids)
	var got []string
	for id := range marked {
		got = append(got, id)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("notifier matched %v, Rank matched %v", got, want)
	}

	// Spot-check the compound semantics both sides share.
	for _, id := range []string{"p1", "p2", "p5", "p6", "p7", "p9", "p11"} {
		if !marked[id] {
			t.Errorf("%s should match", id)
		}
	}
	for _, id := range []string{"p3", "p4", "p8", "p10", "p12"} {
		if marked[id] {
			t.Errorf("%s should not match", id)
		}
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d pushes, want 1", len(sender.sent))
	}
	if body := sender.sent[0].note.Body; !strings.HasPrefix(body, "7 new matches: melk, kaffe, vaskemiddel, grandiosa pizza, Øl") {
		t.Errorf("body = %q", body)
	}
}

func TestNotifyRecentDeliversAfterQuietHoursEnd(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{}
	quiet := map[int64]bool{1: true}
	n := testNotifier(db, sender, quiet)

	// The sync lands inside the user's quiet window: nothing is sent.
	if err := n.NotifyRecent(context.Background(), time.Now()); err != nil {
		t.Fatalf("quiet pass: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent %d pushes during quiet hours", len(sender.sent))
	}

	// A later periodic pass after the window ends delivers the held-back match.
	delete(quiet, 1)
	if err := n.NotifyRecent(context.Background(), time.Now()); err != nil {
		t.Fatalf("post-quiet pass: %v", err)
	}
	if len(sender.forUser(1)) != 1 || sender.sent[0].note.Body != "1 new match: melk" {
		t.Fatalf("sent = %+v", sender.sent)
	}
	if !notifiedIDs(t, db, 1, []string{"a"})["a"] {
		t.Error("offer not marked notified after delivery")
	}

	// Further passes do not repeat it.
	if err := n.NotifyRecent(context.Background(), time.Now()); err != nil {
		t.Fatalf("repeat pass: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d pushes after repeat pass, want 1", len(sender.sent))
	}
}

func TestNotifyRecentIgnoresOffersOutsideWindow(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	storeOffers(t, db, headingOffer("old", "Helmelk"), headingOffer("legacy", "Lettmelk"), headingOffer("new", "Skummet melk"))
	stale := time.Now().Add(-RecentWindow - time.Hour).UTC().Format(time.RFC3339)
	if _, err := db.Exec("UPDATE shop_offers SET first_seen_at = ? WHERE id = 'old'", stale); err != nil {
		t.Fatal(err)
	}
	// Rows that predate the first_seen_at column keep the '' default.
	if _, err := db.Exec("UPDATE shop_offers SET first_seen_at = '' WHERE id = 'legacy'"); err != nil {
		t.Fatal(err)
	}

	ids, err := RecentOfferIDs(context.Background(), db, time.Now().Add(-RecentWindow))
	if err != nil {
		t.Fatalf("recent ids: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"new"}) {
		t.Fatalf("recent ids = %v, want [new]", ids)
	}

	sender := &fakeSender{}
	if err := testNotifier(db, sender, nil).NotifyRecent(context.Background(), time.Now()); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].note.Body != "1 new match: melk" {
		t.Fatalf("sent = %+v", sender.sent)
	}
}

func TestNotifyVAPIDLookupErrorIsReturned(t *testing.T) {
	db := setupTestDB(t)
	seedNotifyUser(t, db, 1, "melk")
	ids := storeOffers(t, db, headingOffer("a", "Helmelk"))

	sender := &fakeSender{}
	n := testNotifier(db, sender, nil)
	n.VAPIDConfigured = func(*sql.DB) (bool, error) { return false, errors.New("disk I/O error") }
	if err := n.Notify(context.Background(), ids, notifyNow); err == nil {
		t.Fatal("notify returned nil for a failed VAPID lookup")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent %d pushes after a failed VAPID lookup", len(sender.sent))
	}
}

func TestRunNotifyPassLogsErrorsAndHonoursCancel(t *testing.T) {
	db := setupTestDB(t)
	orig := notifyRecent
	t.Cleanup(func() { notifyRecent = orig })

	var gotDeadline bool
	var gotCtxErr error
	notifyRecent = func(ctx context.Context, _ *sql.DB, _ time.Time) error {
		_, gotDeadline = ctx.Deadline()
		gotCtxErr = ctx.Err()
		return errors.New("boom")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RunNotifyPass(ctx, db) // must not panic on the error
	if !gotDeadline {
		t.Error("notify pass ran without a timeout")
	}
	if gotCtxErr == nil {
		t.Error("notify pass ignored the caller's cancellation")
	}
}

// newPushSubscription saves a subscription for userID pointing at endpoint
// with real P-256 / auth keys so push.sendPush can encrypt for it.
func newPushSubscription(t *testing.T, db *sql.DB, userID int64, endpoint string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	if _, err := push.SaveSubscription(db, userID,
		endpoint,
		base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(secret)); err != nil {
		t.Fatalf("save subscription: %v", err)
	}
}

func TestDefaultSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/created":
			w.WriteHeader(http.StatusCreated)
		case "/gone":
			w.WriteHeader(http.StatusGone)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	// A listener that is closed straight away yields a transport error.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL + "/secret-token"
	dead.Close()

	tests := []struct {
		name       string
		endpoints  []string
		wantErr    bool
		wantErrMsg string
	}{
		{"partial delivery", []string{srv.URL + "/created", srv.URL + "/gone", srv.URL + "/fail"}, false, ""},
		{"all non-2xx", []string{srv.URL + "/gone", srv.URL + "/fail"}, true, "push endpoint returned"},
		{"no subscriptions", nil, true, "no push subscriptions"},
		{"transport error", []string{deadURL}, true, "send request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			if _, err := push.GetOrCreateVAPIDKeys(db); err != nil {
				t.Fatalf("vapid keys: %v", err)
			}
			for _, ep := range tc.endpoints {
				newPushSubscription(t, db, 1, ep)
			}
			var logs strings.Builder
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			err := defaultSend(db, 1, []byte(`{"title":"t"}`))
			if tc.wantErr {
				if err == nil {
					t.Fatal("defaultSend returned nil, want an error (offers would be marked notified)")
				}
				if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErrMsg)
				}
			} else if err != nil {
				t.Fatalf("defaultSend returned %v, want nil when one subscription accepted", err)
			}
			if strings.Contains(logs.String(), "secret-token") || (err != nil && strings.Contains(err.Error(), "secret-token")) {
				t.Errorf("push endpoint leaked into logs/error: %s / %v", logs.String(), err)
			}
		})
	}
}

func TestRedactPushErr(t *testing.T) {
	inner := errors.New("connection refused")
	wrapped := fmt.Errorf("send request: %w", &url.Error{Op: "Post", URL: "https://fcm.example/send/abc123", Err: inner})
	got := redactPushErr(wrapped)
	if strings.Contains(got.Error(), "abc123") {
		t.Errorf("redacted error still contains the endpoint: %v", got)
	}
	if !errors.Is(got, inner) {
		t.Errorf("redacted error lost the cause: %v", got)
	}
	plain := errors.New("encrypt: bad key")
	if redactPushErr(plain) != plain {
		t.Error("non-URL errors should pass through unchanged")
	}
}
