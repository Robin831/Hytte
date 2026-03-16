package infra

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

const dbStatsCacheTTL = 60 * time.Second

// DBTableStats holds row count information for a database table.
type DBTableStats struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
}

// DBOverview holds overall database statistics.
type DBOverview struct {
	PageCount int64          `json:"page_count"`
	PageSize  int64          `json:"page_size"`
	SizeBytes int64          `json:"size_bytes"`
	SizeMB    float64        `json:"size_mb"`
	Tables    []DBTableStats `json:"tables"`
}

// dbStatsCache holds a cached result for a single user.
type dbStatsCache struct {
	result    *DBOverview
	cachedAt  time.Time
}

// DBStatsModule reports SQLite database statistics.
type DBStatsModule struct {
	db    *sql.DB
	mu    sync.Mutex
	cache map[int64]dbStatsCache
}

// NewDBStatsModule creates a database stats module.
func NewDBStatsModule(db *sql.DB) *DBStatsModule {
	return &DBStatsModule{
		db:    db,
		cache: make(map[int64]dbStatsCache),
	}
}

func (m *DBStatsModule) Name() string        { return "db_stats" }
func (m *DBStatsModule) DisplayName() string { return "Database Stats" }
func (m *DBStatsModule) Description() string {
	return "SQLite database size and table row counts"
}

// Check gathers database statistics scoped to the authenticated user.
// Only tables containing a user_id column are included, with row counts
// filtered to the user's own data to prevent leaking other users' information.
// Results are cached for dbStatsCacheTTL to avoid expensive full-table scans
// on every status poll.
func (m *DBStatsModule) Check(userID int64) ModuleResult {
	m.mu.Lock()
	if c, ok := m.cache[userID]; ok && time.Since(c.cachedAt) < dbStatsCacheTTL {
		cached := c.result
		m.mu.Unlock()
		msg := fmt.Sprintf("%.2f MB, %d tables with your data", cached.SizeMB, len(cached.Tables))
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusOK,
			Message:   msg,
			Details:   map[string]any{"overview": cached},
			CheckedAt: time.Now().UTC(),
		}
	}
	m.mu.Unlock()

	overview, err := m.gatherStats(userID)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusDown,
			Message:   fmt.Sprintf("Failed to gather stats: %s", err.Error()),
			CheckedAt: time.Now().UTC(),
		}
	}

	m.mu.Lock()
	m.cache[userID] = dbStatsCache{result: overview, cachedAt: time.Now()}
	m.mu.Unlock()

	msg := fmt.Sprintf("%.2f MB, %d tables with your data", overview.SizeMB, len(overview.Tables))

	return ModuleResult{
		Name:      m.Name(),
		Status:    StatusOK,
		Message:   msg,
		Details:   map[string]any{"overview": overview},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *DBStatsModule) gatherStats(userID int64) (*DBOverview, error) {
	overview := &DBOverview{}

	// Get page count and page size (overall DB size is not sensitive).
	if err := m.db.QueryRow("PRAGMA page_count").Scan(&overview.PageCount); err != nil {
		return nil, fmt.Errorf("page_count: %w", err)
	}
	if err := m.db.QueryRow("PRAGMA page_size").Scan(&overview.PageSize); err != nil {
		return nil, fmt.Errorf("page_size: %w", err)
	}

	overview.SizeBytes = overview.PageCount * overview.PageSize
	overview.SizeMB = float64(overview.SizeBytes) / (1024 * 1024)

	// Find tables that have a user_id column (user-scoped data).
	rows, err := m.db.Query(
		`SELECT m.name FROM sqlite_master m
		 WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
		 AND EXISTS (SELECT 1 FROM pragma_table_info(m.name) WHERE name = 'user_id')
		 ORDER BY m.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Count only the authenticated user's rows in each table.
	overview.Tables = make([]DBTableStats, 0, len(tableNames))
	for _, name := range tableNames {
		var count int64
		// Table names come from sqlite_master, which is a trusted source.
		query := fmt.Sprintf("SELECT COUNT(*) FROM %q WHERE user_id = ?", name)
		if err := m.db.QueryRow(query, userID).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", name, err)
		}
		overview.Tables = append(overview.Tables, DBTableStats{
			Name:     name,
			RowCount: count,
		})
	}

	return overview, nil
}
