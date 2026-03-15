package infra

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// SSLHost represents a hostname to monitor for SSL certificate expiry.
type SSLHost struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Hostname  string `json:"hostname"`
	Port      int    `json:"port"`
	CreatedAt string `json:"created_at"`
}

// CertCheckResult holds the result of checking a single host's certificate.
type CertCheckResult struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Hostname      string `json:"hostname"`
	Port          int    `json:"port"`
	Status        string `json:"status"`
	Issuer        string `json:"issuer,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	DaysRemaining int    `json:"days_remaining,omitempty"`
	Error         string `json:"error,omitempty"`
}

// SSLCertModule monitors SSL certificate expiry dates.
type SSLCertModule struct {
	db      *sql.DB
	timeout time.Duration
}

// NewSSLCertModule creates an SSL certificate monitoring module.
func NewSSLCertModule(db *sql.DB) *SSLCertModule {
	return &SSLCertModule{
		db:      db,
		timeout: 10 * time.Second,
	}
}

func (m *SSLCertModule) Name() string        { return "ssl_certs" }
func (m *SSLCertModule) DisplayName() string { return "SSL Certificate Expiry" }
func (m *SSLCertModule) Description() string {
	return "Monitor SSL/TLS certificate expiration dates"
}

// Check inspects certificates for all configured hosts.
func (m *SSLCertModule) Check() ModuleResult {
	hosts, err := ListSSLHosts(m.db)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "Failed to load hosts",
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(hosts) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusOK,
			Message:   "No hosts configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"certificates": []CertCheckResult{}},
		}
	}

	results := make([]CertCheckResult, 0, len(hosts))
	minDays := math.MaxInt32
	errorCount := 0

	for _, host := range hosts {
		result := m.checkHost(host)
		results = append(results, result)
		if result.Status == string(StatusDown) {
			errorCount++
		} else if result.DaysRemaining < minDays {
			minDays = result.DaysRemaining
		}
		_ = RecordCheck(m.db, m.Name(), host.Name, ModuleStatus(result.Status), result.Error)
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d certificates monitored", len(hosts))

	if errorCount == len(hosts) {
		overall = StatusDown
		msg = "All certificate checks failed"
	} else if errorCount > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d certificate checks failed", errorCount, len(hosts))
	} else if minDays <= 7 {
		overall = StatusDown
		msg = fmt.Sprintf("Certificate expires in %d days", minDays)
	} else if minDays <= 30 {
		overall = StatusDegraded
		msg = fmt.Sprintf("Certificate expires in %d days", minDays)
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"certificates": results},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *SSLCertModule) checkHost(host SSLHost) CertCheckResult {
	result := CertCheckResult{
		ID:       host.ID,
		Name:     host.Name,
		Hostname: host.Hostname,
		Port:     host.Port,
	}

	// Validate hostname for early rejection with a clear error message.
	if err := ValidateHostname(host.Hostname); err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("blocked: %v", err)
		return result
	}

	// Use safeDialContext which validates resolved IPs at connection time
	// to prevent DNS rebinding attacks.
	addr := fmt.Sprintf("%s:%d", host.Hostname, host.Port)
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	rawConn, err := safeDialContext(ctx, "tcp", addr)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}

	conn := tls.Client(rawConn, &tls.Config{
		ServerName: host.Hostname,
	})
	defer conn.Close()

	if err := conn.HandshakeContext(ctx); err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		result.Status = string(StatusDown)
		result.Error = "no certificates returned"
		return result
	}

	leaf := certs[0]
	result.Issuer = leaf.Issuer.CommonName
	result.ExpiresAt = leaf.NotAfter.UTC().Format(time.RFC3339)
	daysRemaining := int(time.Until(leaf.NotAfter).Hours() / 24)
	result.DaysRemaining = daysRemaining

	if daysRemaining <= 0 {
		result.Status = string(StatusDown)
		result.Error = "certificate expired"
	} else if daysRemaining <= 7 {
		result.Status = string(StatusDown)
	} else if daysRemaining <= 30 {
		result.Status = string(StatusDegraded)
	} else {
		result.Status = string(StatusOK)
	}

	return result
}

// --- Database operations ---

// ListSSLHosts returns all configured SSL hosts.
func ListSSLHosts(db *sql.DB) ([]SSLHost, error) {
	rows, err := db.Query(`SELECT id, name, hostname, port, created_at FROM infra_ssl_hosts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []SSLHost
	for rows.Next() {
		var h SSLHost
		if err := rows.Scan(&h.ID, &h.Name, &h.Hostname, &h.Port, &h.CreatedAt); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// AddSSLHost inserts a new host to monitor.
func AddSSLHost(db *sql.DB, name, hostname string, port int) (SSLHost, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_ssl_hosts (name, hostname, port, created_at) VALUES (?, ?, ?, ?)`,
		name, hostname, port, now,
	)
	if err != nil {
		return SSLHost{}, err
	}
	id, _ := result.LastInsertId()
	return SSLHost{ID: id, Name: name, Hostname: hostname, Port: port, CreatedAt: now}, nil
}

// DeleteSSLHost removes a host by ID.
func DeleteSSLHost(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_ssl_hosts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- HTTP handlers ---

// ListSSLHostsHandler returns all configured SSL hosts.
func ListSSLHostsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hosts, err := ListSSLHosts(db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list hosts")
			return
		}
		if hosts == nil {
			hosts = []SSLHost{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
	}
}

// AddSSLHostHandler adds a new host to monitor.
func AddSSLHostHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Hostname string `json:"hostname"`
			Port     int    `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Name = strings.TrimSpace(body.Name)
		body.Hostname = strings.TrimSpace(body.Hostname)

		if body.Name == "" || body.Hostname == "" {
			writeError(w, http.StatusBadRequest, "name and hostname are required")
			return
		}
		if body.Port <= 0 {
			body.Port = 443
		}
		if body.Port > 65535 {
			writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
			return
		}

		if err := ValidateHostname(body.Hostname); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		host, err := AddSSLHost(db, body.Name, body.Hostname, body.Port)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add host")
			return
		}
		writeJSON(w, http.StatusCreated, host)
	}
}

// DeleteSSLHostHandler removes a host.
func DeleteSSLHostHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteSSLHost(db, id); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "host not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete host")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
