package infra

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// HealthService represents a service endpoint to monitor.
type HealthService struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// ServiceCheckResult holds the result of checking a single service.
type ServiceCheckResult struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	Status         string `json:"status"`
	StatusCode     int    `json:"status_code,omitempty"`
	ResponseTimeMs int64  `json:"response_time_ms,omitempty"`
	Error          string `json:"error,omitempty"`
}

// HealthCheckModule monitors HTTP service endpoints.
type HealthCheckModule struct {
	db     *sql.DB
	client *http.Client
}

// NewHealthCheckModule creates a health check module with a sensible timeout.
// The HTTP client uses a custom dialer that validates resolved IPs at
// connection time to prevent DNS rebinding SSRF attacks.
func NewHealthCheckModule(db *sql.DB) *HealthCheckModule {
	return newHealthCheckModule(db, 10*time.Second)
}

// newHealthCheckModule creates a health check module with a configurable timeout.
// Used internally and in tests to avoid long waits on unreachable addresses.
func newHealthCheckModule(db *sql.DB, timeout time.Duration) *HealthCheckModule {
	return &HealthCheckModule{
		db: db,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: safeDialContext,
				// Disable proxy to prevent SSRF bypasses: if HTTP(S)_PROXY is set,
				// requests would be tunneled via the proxy and safeDialContext would
				// not validate the final destination IPs.
				Proxy: nil,
			},
			// Do not follow redirects — a redirect to an internal URL
			// could bypass the initial URL validation.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (m *HealthCheckModule) Name() string        { return "health_checks" }
func (m *HealthCheckModule) DisplayName() string { return "Service Health Checks" }
func (m *HealthCheckModule) Description() string {
	return "Monitor HTTP service endpoints for availability"
}

// Check pings all configured services for userID and returns aggregated status.
// Load errors are reported as StatusDown (a real failure, not merely
// "unconfigured"), keeping StatusUnknown reserved for the case where no
// services have been set up yet.
func (m *HealthCheckModule) Check(userID int64) ModuleResult {
	services, err := ListHealthServices(m.db, userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusDown,
			Message:   "Failed to load services",
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(services) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No services configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"services": []ServiceCheckResult{}},
		}
	}

	results := make([]ServiceCheckResult, len(services))
	downCount := 0

	var wg sync.WaitGroup
	for i, svc := range services {
		wg.Add(1)
		go func(idx int, s HealthService) {
			defer wg.Done()
			results[idx] = m.checkService(s)
		}(i, svc)
	}
	wg.Wait()

	for i, result := range results {
		if result.Status == string(StatusDown) {
			downCount++
		}
		if err := RecordCheck(m.db, userID, m.Name(), services[i].Name, ModuleStatus(result.Status), result.Error); err != nil {
			log.Printf("infra: failed to record health check history for %q: %v", services[i].Name, err)
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d/%d services healthy", len(services)-downCount, len(services))

	if downCount == len(services) {
		overall = StatusDown
	} else if downCount > 0 {
		overall = StatusDegraded
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"services": results},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *HealthCheckModule) checkService(svc HealthService) ServiceCheckResult {
	result := ServiceCheckResult{
		ID:   svc.ID,
		Name: svc.Name,
		URL:  svc.URL,
	}

	// Validate URL before making any outbound request to prevent SSRF.
	if err := ValidateServiceURL(svc.URL); err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("blocked: %v", err)
		return result
	}

	start := time.Now()
	resp, err := m.client.Get(svc.URL)
	elapsed := time.Since(start)
	result.ResponseTimeMs = elapsed.Milliseconds()

	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Status = string(StatusOK)
	} else {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return result
}

// --- Database operations ---

// defaultHealthServices are seeded for every user that has no services yet.
// They provide a useful starting point and ensure the overall status reflects
// real data rather than Unknown from the very first page load.
var defaultHealthServices = []struct {
	Name string
	URL  string
}{
	{"Hytte", "https://robinedvardsmith.com/api/health"},
}

// EnsureDefaultHealthServices inserts the default health check services for
// userID if that user has no services configured yet. It is safe under
// concurrent requests: the unique index on (user_id, name) means INSERT OR
// IGNORE silently discards any duplicate that races through the count-check
// window, so at most one row per default service name is ever created.
func EnsureDefaultHealthServices(db *sql.DB, userID int64) error {
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM infra_health_services WHERE user_id = ?`, userID,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, d := range defaultHealthServices {
		if _, err := db.Exec(
			`INSERT OR IGNORE INTO infra_health_services (user_id, name, url, created_at) VALUES (?, ?, ?, ?)`,
			userID, d.Name, d.URL, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// ListHealthServices returns all health check services configured for userID.
func ListHealthServices(db *sql.DB, userID int64) ([]HealthService, error) {
	rows, err := db.Query(
		`SELECT id, name, url, created_at FROM infra_health_services WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	services := make([]HealthService, 0)
	for rows.Next() {
		var s HealthService
		if err := rows.Scan(&s.ID, &s.Name, &s.URL, &s.CreatedAt); err != nil {
			return nil, err
		}
		services = append(services, s)
	}
	return services, rows.Err()
}

// AddHealthService inserts a new service to monitor for userID.
func AddHealthService(db *sql.DB, userID int64, name, url string) (HealthService, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_health_services (user_id, name, url, created_at) VALUES (?, ?, ?, ?)`,
		userID, name, url, now,
	)
	if err != nil {
		return HealthService{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return HealthService{}, err
	}
	return HealthService{ID: id, Name: name, URL: url, CreatedAt: now}, nil
}

// DeleteHealthService removes a service by ID, scoped to userID.
func DeleteHealthService(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_health_services WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- HTTP handlers ---

// ListHealthServicesHandler returns all configured services for the authenticated user.
// On first call (no services configured) it seeds defaults via an idempotent
// INSERT OR IGNORE — safe to retry and safe under concurrent requests.
func ListHealthServicesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if err := EnsureDefaultHealthServices(db, user.ID); err != nil {
			log.Printf("infra: failed to seed default health services for user %d: %v", user.ID, err)
		}
		services, err := ListHealthServices(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list services")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": services})
	}
}

// AddHealthServiceHandler adds a new service to monitor for the authenticated user.
func AddHealthServiceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Name = strings.TrimSpace(body.Name)
		body.URL = strings.TrimSpace(body.URL)

		if body.Name == "" || body.URL == "" {
			writeError(w, http.StatusBadRequest, "name and url are required")
			return
		}

		if err := ValidateServiceURL(body.URL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		svc, err := AddHealthService(db, user.ID, body.Name, body.URL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add service")
			return
		}
		writeJSON(w, http.StatusCreated, svc)
	}
}

// DeleteHealthServiceHandler removes a service belonging to the authenticated user.
func DeleteHealthServiceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteHealthService(db, user.ID, id); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "service not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete service")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
