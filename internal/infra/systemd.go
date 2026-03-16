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

// SystemdService represents a configured systemd service to monitor.
type SystemdService struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Unit      string `json:"unit"`
	CreatedAt string `json:"created_at"`
}

// SystemdServiceResult holds the check result for a single systemd service.
type SystemdServiceResult struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Unit        string `json:"unit"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// systemdChecker abstracts systemd status lookups for testability.
type systemdChecker interface {
	// UnitStatus returns the active state and sub-state for a unit.
	UnitStatus(unit string) (activeState, subState string, err error)
}

// SystemdModule monitors systemd service units on the local host.
type SystemdModule struct {
	db      *sql.DB
	checker systemdChecker
}

// NewSystemdModule creates a systemd service monitoring module.
func NewSystemdModule(db *sql.DB) *SystemdModule {
	return &SystemdModule{
		db:      db,
		checker: &execSystemdChecker{},
	}
}

func (m *SystemdModule) Name() string        { return "systemd" }
func (m *SystemdModule) DisplayName() string  { return "System Services" }
func (m *SystemdModule) Description() string {
	return "Monitor systemd service units on the local host"
}

// Check queries the status of all configured systemd services for the user.
func (m *SystemdModule) Check(userID int64) ModuleResult {
	services, err := ListSystemdServices(m.db, userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "Failed to load systemd services",
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(services) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No systemd services configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"services": []SystemdServiceResult{}},
		}
	}

	results := make([]SystemdServiceResult, len(services))
	var wg sync.WaitGroup
	for i, svc := range services {
		wg.Add(1)
		go func(idx int, svc SystemdService) {
			defer wg.Done()
			results[idx] = m.checkService(svc)
		}(i, svc)
	}
	wg.Wait()

	failedCount := 0
	for i, result := range results {
		status := StatusOK
		if result.Status == string(StatusDown) {
			failedCount++
			status = StatusDown
		} else if result.Status == string(StatusDegraded) {
			status = StatusDegraded
		}
		if err := RecordCheck(m.db, userID, m.Name(), services[i].Unit, status, result.Error); err != nil {
			log.Printf("infra: failed to record systemd check for %q: %v", services[i].Unit, err)
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d services monitored", len(services))
	if failedCount == len(services) {
		overall = StatusDown
		msg = "All services inactive"
	} else if failedCount > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d services inactive", failedCount, len(services))
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"services": results},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *SystemdModule) checkService(svc SystemdService) SystemdServiceResult {
	result := SystemdServiceResult{
		ID:   svc.ID,
		Name: svc.Name,
		Unit: svc.Unit,
	}

	activeState, subState, err := m.checker.UnitStatus(svc.Unit)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}

	result.ActiveState = activeState
	result.SubState = subState

	switch activeState {
	case "active":
		result.Status = string(StatusOK)
	case "reloading", "activating", "deactivating":
		result.Status = string(StatusDegraded)
	default:
		// inactive, failed, etc.
		result.Status = string(StatusDown)
		if activeState == "failed" {
			result.Error = "service failed"
		}
	}

	return result
}

// --- Database operations ---

// ListSystemdServices returns all systemd services configured for userID.
func ListSystemdServices(db *sql.DB, userID int64) ([]SystemdService, error) {
	rows, err := db.Query(
		`SELECT id, name, unit, created_at FROM infra_systemd_services WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	services := make([]SystemdService, 0)
	for rows.Next() {
		var s SystemdService
		if err := rows.Scan(&s.ID, &s.Name, &s.Unit, &s.CreatedAt); err != nil {
			return nil, err
		}
		services = append(services, s)
	}
	return services, rows.Err()
}

// AddSystemdService inserts a new systemd service for userID.
func AddSystemdService(db *sql.DB, userID int64, name, unit string) (SystemdService, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_systemd_services (user_id, name, unit, created_at) VALUES (?, ?, ?, ?)`,
		userID, name, unit, now,
	)
	if err != nil {
		return SystemdService{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SystemdService{}, err
	}
	return SystemdService{ID: id, Name: name, Unit: unit, CreatedAt: now}, nil
}

// DeleteSystemdService removes a systemd service by ID, scoped to userID.
func DeleteSystemdService(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_systemd_services WHERE id = ? AND user_id = ?`, id, userID)
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

// ListSystemdServicesHandler returns all configured systemd services.
func ListSystemdServicesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		services, err := ListSystemdServices(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list systemd services")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": services})
	}
}

// AddSystemdServiceHandler adds a new systemd service to monitor.
func AddSystemdServiceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Name string `json:"name"`
			Unit string `json:"unit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Name = strings.TrimSpace(body.Name)
		body.Unit = strings.TrimSpace(body.Unit)

		if body.Name == "" || body.Unit == "" {
			writeError(w, http.StatusBadRequest, "name and unit are required")
			return
		}

		if err := validateUnitName(body.Unit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		svc, err := AddSystemdService(db, user.ID, body.Name, body.Unit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add systemd service")
			return
		}
		writeJSON(w, http.StatusCreated, svc)
	}
}

// DeleteSystemdServiceHandler removes a systemd service.
func DeleteSystemdServiceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteSystemdService(db, user.ID, id); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "systemd service not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete systemd service")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// validateUnitName checks that a unit name is safe and well-formed.
// Accepts names like "nginx.service", "docker.service", "postgresql@14.service".
func validateUnitName(unit string) error {
	if len(unit) > 256 {
		return fmt.Errorf("unit name too long (max 256 characters)")
	}

	// Must end with a known systemd unit suffix.
	validSuffixes := []string{".service", ".socket", ".timer", ".mount", ".target", ".path", ".slice"}
	hasSuffix := false
	for _, suffix := range validSuffixes {
		if strings.HasSuffix(unit, suffix) {
			hasSuffix = true
			break
		}
	}
	if !hasSuffix {
		return fmt.Errorf("unit must end with a valid suffix (.service, .socket, .timer, .mount, .target, .path, .slice)")
	}

	// Only allow safe characters: alphanumeric, dash, underscore, dot, @, backslash.
	for _, ch := range unit {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_' || ch == '.' || ch == '@' || ch == '\\') {
			return fmt.Errorf("unit name contains invalid character: %c", ch)
		}
	}

	return nil
}
