package stars

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAwardStarsHandler(t *testing.T) {
	db := setupTestDB(t)

	// Insert a user to award stars to.
	res, err := db.Exec(`INSERT INTO users (email, name, google_id) VALUES ('child@example.com', 'Child', 'google-child')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	childID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	handler := AdminAwardStarsHandler(db)

	t.Run("award positive stars", func(t *testing.T) {
		body, _ := json.Marshal(adminAwardRequest{
			UserID:      childID,
			Amount:      10,
			Reason:      "migration",
			Description: "importing old balance",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Check transaction was inserted.
		var txAmount int
		var txReason, txDesc string
		err := db.QueryRow(`SELECT amount, reason, description FROM star_transactions WHERE user_id = ?`, childID).Scan(&txAmount, &txReason, &txDesc)
		if err != nil {
			t.Fatalf("query transaction: %v", err)
		}
		if txAmount != 10 {
			t.Errorf("transaction amount = %d, want 10", txAmount)
		}
		if txReason != "migration" {
			t.Errorf("transaction reason = %q, want 'migration'", txReason)
		}

		// Check balance was updated.
		earned, _, balance := getBalance(t, db, childID)
		if earned != 10 {
			t.Errorf("total_earned = %d, want 10", earned)
		}
		if balance != 10 {
			t.Errorf("current_balance = %d, want 10", balance)
		}
	})

	t.Run("deduct stars via negative amount", func(t *testing.T) {
		body, _ := json.Marshal(adminAwardRequest{
			UserID:      childID,
			Amount:      -3,
			Reason:      "correction",
			Description: "fixing error",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		_, spent, balance := getBalance(t, db, childID)
		if spent != 3 {
			t.Errorf("total_spent = %d, want 3", spent)
		}
		if balance != 7 {
			t.Errorf("current_balance = %d, want 7", balance)
		}
	})

	t.Run("missing user_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"amount": 5, "reason": "test"})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("zero amount rejected", func(t *testing.T) {
		body, _ := json.Marshal(adminAwardRequest{UserID: childID, Amount: 0, Reason: "test"})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing reason", func(t *testing.T) {
		body, _ := json.Marshal(adminAwardRequest{UserID: childID, Amount: 5})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("whitespace-only reason rejected", func(t *testing.T) {
		body, _ := json.Marshal(adminAwardRequest{UserID: childID, Amount: 5, Reason: "   "})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("non-existent user_id returns 404", func(t *testing.T) {
		body, _ := json.Marshal(adminAwardRequest{UserID: 99999, Amount: 5, Reason: "test"})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/stars/award", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}
