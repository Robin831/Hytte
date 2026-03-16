package infra

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// DNSMonitor represents a configured DNS hostname to monitor.
type DNSMonitor struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	RecordType string `json:"record_type"`
	CreatedAt  string `json:"created_at"`
}

// DNSCheckResult holds the result of resolving a single DNS entry.
type DNSCheckResult struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Hostname       string   `json:"hostname"`
	RecordType     string   `json:"record_type"`
	Status         string   `json:"status"`
	ResolvedValues []string `json:"resolved_values,omitempty"`
	ResponseTimeMs int64    `json:"response_time_ms"`
	Error          string   `json:"error,omitempty"`
}

// Allowed DNS record types for validation.
var allowedRecordTypes = map[string]bool{
	"A":     true,
	"AAAA":  true,
	"CNAME": true,
	"MX":    true,
	"TXT":   true,
	"NS":    true,
}

// DNSModule monitors DNS resolution for configured hostnames.
type DNSModule struct {
	db       *sql.DB
	resolver dnsResolver
}

// dnsResolver abstracts DNS lookups for testability.
type dnsResolver interface {
	LookupHost(hostname string) ([]string, error)
	LookupCNAME(hostname string) (string, error)
	LookupMX(hostname string) ([]*net.MX, error)
	LookupTXT(hostname string) ([]string, error)
	LookupNS(hostname string) ([]*net.NS, error)
}

// netResolver wraps the standard net resolver.
type netResolver struct{}

func (n *netResolver) LookupHost(hostname string) ([]string, error) {
	return net.LookupHost(hostname)
}

func (n *netResolver) LookupCNAME(hostname string) (string, error) {
	return net.LookupCNAME(hostname)
}

func (n *netResolver) LookupMX(hostname string) ([]*net.MX, error) {
	return net.LookupMX(hostname)
}

func (n *netResolver) LookupTXT(hostname string) ([]string, error) {
	return net.LookupTXT(hostname)
}

func (n *netResolver) LookupNS(hostname string) ([]*net.NS, error) {
	return net.LookupNS(hostname)
}

// NewDNSModule creates a DNS monitoring module.
func NewDNSModule(db *sql.DB) *DNSModule {
	return &DNSModule{
		db:       db,
		resolver: &netResolver{},
	}
}

func (m *DNSModule) Name() string        { return "dns" }
func (m *DNSModule) DisplayName() string { return "DNS Monitoring" }
func (m *DNSModule) Description() string {
	return "Monitor DNS resolution for configured hostnames"
}

// Check resolves all configured DNS entries for the user.
func (m *DNSModule) Check(userID int64) ModuleResult {
	monitors, err := ListDNSMonitors(m.db, userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "Failed to load DNS monitors",
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(monitors) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No DNS monitors configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"monitors": []DNSCheckResult{}},
		}
	}

	results := make([]DNSCheckResult, len(monitors))
	var wg sync.WaitGroup
	for i, mon := range monitors {
		wg.Add(1)
		go func(idx int, mon DNSMonitor) {
			defer wg.Done()
			results[idx] = m.checkDNS(mon)
		}(i, mon)
	}
	wg.Wait()

	failedCount := 0
	for i, result := range results {
		status := StatusOK
		if result.Error != "" {
			failedCount++
			status = StatusDown
		}
		target := monitors[i].Hostname + ":" + monitors[i].RecordType
		if err := RecordCheck(m.db, userID, m.Name(), target, status, result.Error); err != nil {
			log.Printf("infra: failed to record DNS check for %q: %v", target, err)
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d hostnames monitored", len(monitors))
	if failedCount == len(monitors) {
		overall = StatusDown
		msg = "All DNS lookups failed"
	} else if failedCount > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d DNS lookups failed", failedCount, len(monitors))
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"monitors": results},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *DNSModule) checkDNS(mon DNSMonitor) DNSCheckResult {
	result := DNSCheckResult{
		ID:         mon.ID,
		Name:       mon.Name,
		Hostname:   mon.Hostname,
		RecordType: mon.RecordType,
	}

	start := time.Now()
	var values []string
	var err error

	switch mon.RecordType {
	case "A", "AAAA":
		values, err = m.resolver.LookupHost(mon.Hostname)
		if mon.RecordType == "AAAA" {
			// Filter to only IPv6 addresses.
			filtered := make([]string, 0, len(values))
			for _, v := range values {
				if strings.Contains(v, ":") {
					filtered = append(filtered, v)
				}
			}
			values = filtered
		} else {
			// Filter to only IPv4 addresses.
			filtered := make([]string, 0, len(values))
			for _, v := range values {
				if !strings.Contains(v, ":") {
					filtered = append(filtered, v)
				}
			}
			values = filtered
		}
	case "CNAME":
		var cname string
		cname, err = m.resolver.LookupCNAME(mon.Hostname)
		if err == nil {
			values = []string{cname}
		}
	case "MX":
		var mxRecords []*net.MX
		mxRecords, err = m.resolver.LookupMX(mon.Hostname)
		if err == nil {
			values = make([]string, 0, len(mxRecords))
			for _, mx := range mxRecords {
				values = append(values, fmt.Sprintf("%s (priority %d)", mx.Host, mx.Pref))
			}
		}
	case "TXT":
		values, err = m.resolver.LookupTXT(mon.Hostname)
	case "NS":
		var nsRecords []*net.NS
		nsRecords, err = m.resolver.LookupNS(mon.Hostname)
		if err == nil {
			values = make([]string, 0, len(nsRecords))
			for _, ns := range nsRecords {
				values = append(values, ns.Host)
			}
		}
	default:
		err = fmt.Errorf("unsupported record type: %s", mon.RecordType)
	}

	result.ResponseTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}

	// Filter private IPs from resolved values to prevent leaking internal topology.
	if mon.RecordType == "A" || mon.RecordType == "AAAA" {
		values = FilterPrivateIPs(values)
	}

	if len(values) == 0 {
		result.Status = string(StatusDown)
		result.Error = "no records found"
		return result
	}

	result.Status = string(StatusOK)
	result.ResolvedValues = values
	return result
}

// --- Database operations ---

// ListDNSMonitors returns all DNS monitors configured for userID.
func ListDNSMonitors(db *sql.DB, userID int64) ([]DNSMonitor, error) {
	rows, err := db.Query(
		`SELECT id, name, hostname, record_type, created_at FROM infra_dns_monitors WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	monitors := make([]DNSMonitor, 0)
	for rows.Next() {
		var m DNSMonitor
		if err := rows.Scan(&m.ID, &m.Name, &m.Hostname, &m.RecordType, &m.CreatedAt); err != nil {
			return nil, err
		}
		monitors = append(monitors, m)
	}
	return monitors, rows.Err()
}

// AddDNSMonitor inserts a new DNS monitor for userID.
func AddDNSMonitor(db *sql.DB, userID int64, name, hostname, recordType string) (DNSMonitor, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_dns_monitors (user_id, name, hostname, record_type, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, hostname, recordType, now,
	)
	if err != nil {
		return DNSMonitor{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return DNSMonitor{}, err
	}
	return DNSMonitor{ID: id, Name: name, Hostname: hostname, RecordType: recordType, CreatedAt: now}, nil
}

// DeleteDNSMonitor removes a DNS monitor by ID, scoped to userID.
func DeleteDNSMonitor(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_dns_monitors WHERE id = ? AND user_id = ?`, id, userID)
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

// ListDNSMonitorsHandler returns all configured DNS monitors.
func ListDNSMonitorsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		monitors, err := ListDNSMonitors(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list DNS monitors")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"monitors": monitors})
	}
}

// AddDNSMonitorHandler adds a new DNS monitor.
func AddDNSMonitorHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Name       string `json:"name"`
			Hostname   string `json:"hostname"`
			RecordType string `json:"record_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Name = strings.TrimSpace(body.Name)
		body.Hostname = strings.TrimSpace(body.Hostname)
		body.RecordType = strings.TrimSpace(strings.ToUpper(body.RecordType))

		if body.Name == "" || body.Hostname == "" {
			writeError(w, http.StatusBadRequest, "name and hostname are required")
			return
		}

		if body.RecordType == "" {
			body.RecordType = "A"
		}

		if !allowedRecordTypes[body.RecordType] {
			writeError(w, http.StatusBadRequest, "record_type must be one of: A, AAAA, CNAME, MX, TXT, NS")
			return
		}

		if err := ValidateDNSHostname(body.Hostname); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		monitor, err := AddDNSMonitor(db, user.ID, body.Name, body.Hostname, body.RecordType)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add DNS monitor")
			return
		}
		writeJSON(w, http.StatusCreated, monitor)
	}
}

// DeleteDNSMonitorHandler removes a DNS monitor.
func DeleteDNSMonitorHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteDNSMonitor(db, user.ID, id); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "DNS monitor not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete DNS monitor")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
