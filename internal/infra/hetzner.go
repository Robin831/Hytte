package infra

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// HetznerServer represents a Hetzner Cloud server from the API.
type HetznerServer struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	ServerType string  `json:"server_type"`
	Datacenter string  `json:"datacenter"`
	PublicIPv4 string  `json:"public_ipv4,omitempty"`
	CPUCount   int     `json:"cpu_count"`
	Memory     float64 `json:"memory_gb"`
	Disk       float64 `json:"disk_gb"`
}

// HetznerModule monitors Hetzner Cloud VPS servers.
type HetznerModule struct {
	db      *sql.DB
	client  *http.Client
	baseURL string // overridable for tests; defaults to https://api.hetzner.cloud
}

// NewHetznerModule creates a Hetzner VPS stats module.
func NewHetznerModule(db *sql.DB) *HetznerModule {
	return &HetznerModule{
		db: db,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (m *HetznerModule) Name() string        { return "hetzner_vps" }
func (m *HetznerModule) DisplayName() string { return "Hetzner VPS Stats" }
func (m *HetznerModule) Description() string {
	return "Monitor Hetzner Cloud server status and resources"
}

// Check fetches all servers from the Hetzner Cloud API for the user.
func (m *HetznerModule) Check(userID int64) ModuleResult {
	token, err := GetHetznerToken(m.db, userID)
	if err != nil || token == "" {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No API token configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"servers": []HetznerServer{}},
		}
	}

	servers, err := m.fetchServers(token)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusDown,
			Message:   fmt.Sprintf("API error: %s", err.Error()),
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(servers) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusOK,
			Message:   "No servers found",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"servers": servers},
		}
	}

	nonRunning := 0
	for _, s := range servers {
		status := StatusOK
		if s.Status != "running" {
			nonRunning++
			status = StatusDown
		}
		if err := RecordCheck(m.db, userID, m.Name(), s.Name, status, s.Status); err != nil {
			log.Printf("infra: failed to record Hetzner check for %q: %v", s.Name, err)
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d/%d servers running", len(servers)-nonRunning, len(servers))
	if nonRunning == len(servers) {
		overall = StatusDown
	} else if nonRunning > 0 {
		overall = StatusDegraded
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"servers": servers},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *HetznerModule) fetchServers(token string) ([]HetznerServer, error) {
	base := m.baseURL
	if base == "" {
		base = "https://api.hetzner.cloud"
	}
	req, err := http.NewRequest("GET", base+"/v1/servers?per_page=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	const maxResponseSize int64 = 1 << 20
	lr := &io.LimitedReader{R: resp.Body, N: maxResponseSize + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > maxResponseSize {
		return nil, fmt.Errorf("response body too large (>%d bytes)", maxResponseSize)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Servers []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Server struct {
				Description string  `json:"description"`
				CPUs        int     `json:"cores"`
				Memory      float64 `json:"memory"`
				Disk        float64 `json:"disk"`
			} `json:"server_type"`
			Datacenter struct {
				Name string `json:"name"`
			} `json:"datacenter"`
			PublicNet struct {
				IPv4 struct {
					IP string `json:"ip"`
				} `json:"ipv4"`
			} `json:"public_net"`
		} `json:"servers"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	servers := make([]HetznerServer, 0, len(result.Servers))
	for _, s := range result.Servers {
		servers = append(servers, HetznerServer{
			ID:         s.ID,
			Name:       s.Name,
			Status:     s.Status,
			ServerType: s.Server.Description,
			Datacenter: s.Datacenter.Name,
			PublicIPv4: s.PublicNet.IPv4.IP,
			CPUCount:   s.Server.CPUs,
			Memory:     s.Server.Memory,
			Disk:       s.Server.Disk,
		})
	}
	return servers, nil
}

// --- Database operations ---

// GetHetznerToken returns the stored Hetzner API token for userID,
// decrypting it from the database.
func GetHetznerToken(db *sql.DB, userID int64) (string, error) {
	var encrypted string
	err := db.QueryRow(
		`SELECT api_token FROM infra_hetzner_config WHERE user_id = ?`,
		userID,
	).Scan(&encrypted)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return DecryptToken(encrypted)
}

// SetHetznerToken encrypts and upserts the Hetzner API token for userID.
func SetHetznerToken(db *sql.DB, userID int64, token string) error {
	encrypted, err := EncryptToken(token)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`INSERT INTO infra_hetzner_config (user_id, api_token, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET api_token = excluded.api_token, updated_at = excluded.updated_at`,
		userID, encrypted, now,
	)
	return err
}

// DeleteHetznerToken removes the Hetzner API token for userID.
func DeleteHetznerToken(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM infra_hetzner_config WHERE user_id = ?`, userID)
	return err
}

// MaskToken returns a masked version of a token, showing only the last 4 chars.
func MaskToken(token string) string {
	if len(token) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(token)-4) + token[len(token)-4:]
}

// --- HTTP handlers ---

// HetznerTokenGetHandler returns whether a token is configured (masked).
func HetznerTokenGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		token, err := GetHetznerToken(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get token")
			return
		}
		configured := token != ""
		masked := ""
		if configured {
			masked = MaskToken(token)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": configured,
			"masked":     masked,
		})
	}
}

// HetznerTokenSetHandler stores or updates the Hetzner API token.
func HetznerTokenSetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Token = strings.TrimSpace(body.Token)
		if body.Token == "" {
			writeError(w, http.StatusBadRequest, "token is required")
			return
		}

		if err := SetHetznerToken(db, user.ID, body.Token); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// HetznerTokenDeleteHandler removes the stored Hetzner API token.
func HetznerTokenDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if err := DeleteHetznerToken(db, user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// --- Bandwidth / Transfer Usage Module ---

// BandwidthServer holds traffic data for a single Hetzner server.
type BandwidthServer struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	IncludedTrafficTB float64 `json:"included_traffic_tb"`
	IngoingTrafficTB  float64 `json:"ingoing_traffic_tb"`
	OutgoingTrafficTB float64 `json:"outgoing_traffic_tb"`
	UsagePercent     float64 `json:"usage_percent"`
}

// BandwidthModule monitors Hetzner server bandwidth/transfer usage.
type BandwidthModule struct {
	db      *sql.DB
	client  *http.Client
	baseURL string // overridable for tests; defaults to https://api.hetzner.cloud
}

// NewBandwidthModule creates a bandwidth/transfer usage module.
func NewBandwidthModule(db *sql.DB) *BandwidthModule {
	return &BandwidthModule{
		db: db,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (m *BandwidthModule) Name() string        { return "bandwidth" }
func (m *BandwidthModule) DisplayName() string { return "Bandwidth / Transfer Usage" }
func (m *BandwidthModule) Description() string {
	return "Monitor Hetzner Cloud server bandwidth and transfer quotas"
}

// Check fetches traffic data from the Hetzner Cloud API.
func (m *BandwidthModule) Check(userID int64) ModuleResult {
	token, err := GetHetznerToken(m.db, userID)
	if err != nil || token == "" {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No Hetzner API token configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"servers": []BandwidthServer{}},
		}
	}

	servers, err := m.fetchTraffic(token)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusDown,
			Message:   fmt.Sprintf("API error: %s", err.Error()),
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(servers) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusOK,
			Message:   "No servers found",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"servers": servers},
		}
	}

	var maxUsage float64
	var warnCount int
	for _, s := range servers {
		if s.UsagePercent > maxUsage {
			maxUsage = s.UsagePercent
		}
		if s.UsagePercent >= 80 {
			warnCount++
		}
		status := StatusOK
		if s.UsagePercent >= 95 {
			status = StatusDown
		} else if s.UsagePercent >= 80 {
			status = StatusDegraded
		}
		if err := RecordCheck(m.db, userID, m.Name(), s.Name, status, fmt.Sprintf("%.1f%% used", s.UsagePercent)); err != nil {
			log.Printf("infra: failed to record bandwidth check for %q: %v", s.Name, err)
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d servers monitored, max %.1f%% usage", len(servers), maxUsage)
	if maxUsage >= 95 {
		overall = StatusDown
		msg = fmt.Sprintf("Transfer quota critical: %.1f%% used", maxUsage)
	} else if maxUsage >= 80 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d server(s) above 80%% transfer usage", warnCount)
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"servers": servers},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *BandwidthModule) fetchTraffic(token string) ([]BandwidthServer, error) {
	base := m.baseURL
	if base == "" {
		base = "https://api.hetzner.cloud"
	}
	req, err := http.NewRequest("GET", base+"/v1/servers?per_page=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	const maxResponseSize int64 = 1 << 20
	lr := &io.LimitedReader{R: resp.Body, N: maxResponseSize + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > maxResponseSize {
		return nil, fmt.Errorf("response body too large (>%d bytes)", maxResponseSize)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Servers []struct {
			ID               int64  `json:"id"`
			Name             string `json:"name"`
			IncludedTraffic  int64  `json:"included_traffic"`
			IngoingTraffic   int64  `json:"ingoing_traffic"`
			OutgoingTraffic  int64  `json:"outgoing_traffic"`
		} `json:"servers"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	const bytesPerTB = 1_000_000_000_000.0
	servers := make([]BandwidthServer, 0, len(result.Servers))
	for _, s := range result.Servers {
		included := float64(s.IncludedTraffic) / bytesPerTB
		outgoing := float64(s.OutgoingTraffic) / bytesPerTB
		ingoing := float64(s.IngoingTraffic) / bytesPerTB
		var usage float64
		if s.IncludedTraffic > 0 {
			usage = float64(s.OutgoingTraffic) / float64(s.IncludedTraffic) * 100
		}
		servers = append(servers, BandwidthServer{
			ID:                s.ID,
			Name:              s.Name,
			IncludedTrafficTB: included,
			IngoingTrafficTB:  ingoing,
			OutgoingTrafficTB: outgoing,
			UsagePercent:      usage,
		})
	}
	return servers, nil
}

// --- Docker Containers Module ---

// DockerHost represents a configured Docker host endpoint.
type DockerHost struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// DockerContainer represents a container from the Docker API.
type DockerContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	HostID  int64  `json:"host_id"`
	Host    string `json:"host"`
}

// DockerHostResult holds the check result for a single Docker host.
type DockerHostResult struct {
	HostID     int64             `json:"host_id"`
	HostName   string            `json:"host_name"`
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
	Containers []DockerContainer `json:"containers"`
}

// DockerModule monitors Docker containers across configured hosts.
type DockerModule struct {
	db          *sql.DB
	client      *http.Client
	validateURL func(string) error // overridable for tests; nil uses ValidateServiceURL
}

// NewDockerModule creates a Docker containers monitoring module.
// The HTTP client uses a custom dialer that validates resolved IPs at
// connection time to prevent DNS rebinding SSRF attacks against user-configured
// Docker host URLs.
func NewDockerModule(db *sql.DB) *DockerModule {
	return &DockerModule{
		db: db,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: safeDialContext,
				// Disable proxy to prevent SSRF bypasses via HTTP(S)_PROXY.
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

func (m *DockerModule) Name() string        { return "docker" }
func (m *DockerModule) DisplayName() string { return "Docker Containers" }
func (m *DockerModule) Description() string {
	return "Monitor Docker container status across hosts"
}

// Check queries all configured Docker hosts for container status.
func (m *DockerModule) Check(userID int64) ModuleResult {
	hosts, err := ListDockerHosts(m.db, userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "Failed to load Docker hosts",
			CheckedAt: time.Now().UTC(),
		}
	}

	if len(hosts) == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No Docker hosts configured",
			CheckedAt: time.Now().UTC(),
			Details:   map[string]any{"hosts": []DockerHostResult{}},
		}
	}

	results := make([]DockerHostResult, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h DockerHost) {
			defer wg.Done()
			results[idx] = m.checkHost(h)
		}(i, host)
	}
	wg.Wait()

	totalContainers := 0
	stoppedContainers := 0
	hostErrors := 0
	for i, result := range results {
		status := StatusOK
		if result.Error != "" {
			hostErrors++
			status = StatusDown
		} else {
			for _, c := range result.Containers {
				totalContainers++
				if c.State != "running" {
					stoppedContainers++
				}
			}
		}
		if err := RecordCheck(m.db, userID, m.Name(), hosts[i].Name, status, result.Error); err != nil {
			log.Printf("infra: failed to record Docker check for %q: %v", hosts[i].Name, err)
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%d containers across %d hosts", totalContainers, len(hosts))

	if hostErrors == len(hosts) {
		overall = StatusDown
		msg = "All Docker hosts unreachable"
	} else if hostErrors > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d Docker hosts unreachable", hostErrors, len(hosts))
	} else if stoppedContainers > 0 {
		overall = StatusDegraded
		msg = fmt.Sprintf("%d/%d containers not running", stoppedContainers, totalContainers)
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		Details:   map[string]any{"hosts": results},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *DockerModule) checkHost(host DockerHost) DockerHostResult {
	result := DockerHostResult{
		HostID:   host.ID,
		HostName: host.Name,
	}

	// Validate the URL to prevent SSRF.
	validateFn := m.validateURL
	if validateFn == nil {
		validateFn = ValidateServiceURL
	}
	if err := validateFn(host.URL); err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("blocked: %v", err)
		return result
	}

	url := strings.TrimRight(host.URL, "/") + "/containers/json?all=true"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}

	resp, err := m.client.Do(req)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	const maxResponseSize int64 = 1 << 20
	lr := &io.LimitedReader{R: resp.Body, N: maxResponseSize + 1}
	respBody, err := io.ReadAll(lr)
	if err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("read: %v", err)
		return result
	}
	if int64(len(respBody)) > maxResponseSize {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("response body too large (>%d bytes)", maxResponseSize)
		return result
	}

	if resp.StatusCode != http.StatusOK {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
		return result
	}

	var containers []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		Image string   `json:"Image"`
		State string   `json:"State"`
		Stat  string   `json:"Status"`
	}

	if err := json.Unmarshal(respBody, &containers); err != nil {
		result.Status = string(StatusDown)
		result.Error = fmt.Sprintf("decode: %v", err)
		return result
	}

	result.Status = string(StatusOK)
	result.Containers = make([]DockerContainer, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		// Truncate container ID to 12 chars for display.
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		result.Containers = append(result.Containers, DockerContainer{
			ID:     id,
			Name:   name,
			Image:  c.Image,
			State:  c.State,
			Status: c.Stat,
			HostID: host.ID,
			Host:   host.Name,
		})
	}
	return result
}

// --- Docker host database operations ---

// ListDockerHosts returns all Docker hosts configured for userID.
func ListDockerHosts(db *sql.DB, userID int64) ([]DockerHost, error) {
	rows, err := db.Query(
		`SELECT id, name, url, created_at FROM infra_docker_hosts WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hosts := make([]DockerHost, 0)
	for rows.Next() {
		var h DockerHost
		if err := rows.Scan(&h.ID, &h.Name, &h.URL, &h.CreatedAt); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// AddDockerHost inserts a new Docker host to monitor for userID.
func AddDockerHost(db *sql.DB, userID int64, name, url string) (DockerHost, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		`INSERT INTO infra_docker_hosts (user_id, name, url, created_at) VALUES (?, ?, ?, ?)`,
		userID, name, url, now,
	)
	if err != nil {
		return DockerHost{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return DockerHost{}, err
	}
	return DockerHost{ID: id, Name: name, URL: url, CreatedAt: now}, nil
}

// DeleteDockerHost removes a Docker host by ID, scoped to userID.
func DeleteDockerHost(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(`DELETE FROM infra_docker_hosts WHERE id = ? AND user_id = ?`, id, userID)
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

// --- Docker HTTP handlers ---

// ListDockerHostsHandler returns all configured Docker hosts for the authenticated user.
func ListDockerHostsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		hosts, err := ListDockerHosts(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list Docker hosts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
	}
}

// AddDockerHostHandler adds a new Docker host to monitor for the authenticated user.
func AddDockerHostHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
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

		host, err := AddDockerHost(db, user.ID, body.Name, body.URL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add Docker host")
			return
		}
		writeJSON(w, http.StatusCreated, host)
	}
}

// DeleteDockerHostHandler removes a Docker host belonging to the authenticated user.
func DeleteDockerHostHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		if err := DeleteDockerHost(db, user.ID, id); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "Docker host not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete Docker host")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
