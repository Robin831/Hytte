package infra

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	return &HealthCheckModule{
		db: db,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: safeDialContext,
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

// Check pings all configured services and returns aggregated status.
func (m *HealthCheckModule) Check() ModuleResult {
	services, err := ListHealthServices(m.db)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
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

	results := make([]ServiceCheckResult, 0, len(services))
	downCount := 0

	for _, svc := range services {
		result := m.checkService(svc)
		results = append(results, result)
		if result.Status == string(StatusDown) {
			downCount++
		}
		// Record in uptime history.
		_ = RecordCheck(m.db, m.Name(), svc.Name, ModuleStatus(result.Status), result.Error)
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

// ListHealthServices returns all configured health check services.
func ListHealthServices(db *sql.DB) ([]HealthService, error) {
	rows, err := db.Query(`SELECT id, name, url, created_at FROM infra_health_services ORDER BY name`)
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

// AddHealthService inserts a new service to monitor.
func AddHealthService(db *sql.DB, name, url string) (HealthService, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_health_services (name, url, created_at) VALUES (?, ?, ?)`,
		name, url, now,
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

// DeleteHealthService removes a service by ID.
func DeleteHealthService(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_health_services WHERE id = ?`, id)
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

// ListHealthServicesHandler returns all configured services.
func ListHealthServicesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		services, err := ListHealthServices(db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list services")
			return
		}
		if services == nil {
			services = []HealthService{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": services})
	}
}

// AddHealthServiceHandler adds a new service to monitor.
func AddHealthServiceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		svc, err := AddHealthService(db, body.Name, body.URL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add service")
			return
		}
		writeJSON(w, http.StatusCreated, svc)
	}
}

// DeleteHealthServiceHandler removes a service.
func DeleteHealthServiceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteHealthService(db, id); err != nil {
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
