package infra

import (
	"database/sql"
	"fmt"
	"time"
)

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

// DBStatsModule reports SQLite database statistics.
type DBStatsModule struct {
	db *sql.DB
}

// NewDBStatsModule creates a database stats module.
func NewDBStatsModule(db *sql.DB) *DBStatsModule {
	return &DBStatsModule{db: db}
}

func (m *DBStatsModule) Name() string        { return "db_stats" }
func (m *DBStatsModule) DisplayName() string { return "Database Stats" }
func (m *DBStatsModule) Description() string {
	return "SQLite database size and table row counts"
}

// Check gathers database statistics. userID is unused since DB stats are global.
func (m *DBStatsModule) Check(_ int64) ModuleResult {
	overview, err := m.gatherStats()
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusDown,
			Message:   fmt.Sprintf("Failed to gather stats: %s", err.Error()),
			CheckedAt: time.Now().UTC(),
		}
	}

	msg := fmt.Sprintf("%.2f MB across %d tables", overview.SizeMB, len(overview.Tables))

	return ModuleResult{
		Name:      m.Name(),
		Status:    StatusOK,
		Message:   msg,
		Details:   map[string]any{"overview": overview},
		CheckedAt: time.Now().UTC(),
	}
}

func (m *DBStatsModule) gatherStats() (*DBOverview, error) {
	overview := &DBOverview{}

	// Get page count and page size.
	if err := m.db.QueryRow("PRAGMA page_count").Scan(&overview.PageCount); err != nil {
		return nil, fmt.Errorf("page_count: %w", err)
	}
	if err := m.db.QueryRow("PRAGMA page_size").Scan(&overview.PageSize); err != nil {
		return nil, fmt.Errorf("page_size: %w", err)
	}

	overview.SizeBytes = overview.PageCount * overview.PageSize
	overview.SizeMB = float64(overview.SizeBytes) / (1024 * 1024)

	// Get table names (exclude SQLite internal tables).
	rows, err := m.db.Query(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`,
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

	// Get row counts for each table.
	overview.Tables = make([]DBTableStats, 0, len(tableNames))
	for _, name := range tableNames {
		var count int64
		// Table names come from sqlite_master, which is a trusted source.
		if err := m.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", name)).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", name, err)
		}
		overview.Tables = append(overview.Tables, DBTableStats{
			Name:     name,
			RowCount: count,
		})
	}

	return overview, nil
}
