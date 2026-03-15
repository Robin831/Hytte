package infra

import (
	"database/sql"
	"time"
)

// ModuleStatus represents the health state of an infrastructure module.
type ModuleStatus string

const (
	StatusOK       ModuleStatus = "ok"
	StatusDegraded ModuleStatus = "degraded"
	StatusDown     ModuleStatus = "down"
	StatusUnknown  ModuleStatus = "unknown"
)

// ModuleResult holds the result of a single module check.
type ModuleResult struct {
	Name      string       `json:"name"`
	Status    ModuleStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	Details   any          `json:"details,omitempty"`
	CheckedAt time.Time    `json:"checked_at"`
}

// Module defines the interface that all infrastructure monitoring modules
// must implement. Each module is independently configurable and fetchable.
type Module interface {
	// Name returns the unique identifier for this module (e.g. "health_checks").
	Name() string

	// DisplayName returns a human-readable name for the UI.
	DisplayName() string

	// Description returns a short description of what this module monitors.
	Description() string

	// Check runs the module's health check and returns the result.
	Check() ModuleResult
}

// Registry holds all registered infrastructure modules.
type Registry struct {
	modules map[string]Module
	order   []string
}

// NewRegistry creates an empty module registry.
func NewRegistry() *Registry {
	return &Registry{
		modules: make(map[string]Module),
	}
}

// NewDefaultRegistry creates a registry pre-populated with all built-in
// infrastructure modules. Add new built-in modules here as they are created.
func NewDefaultRegistry(db *sql.DB) *Registry {
	r := NewRegistry()
	r.Register(NewHealthCheckModule(db))
	r.Register(NewSSLCertModule(db))
	r.Register(NewUptimeModule(db))
	return r
}

// Register adds a module to the registry.
func (r *Registry) Register(m Module) {
	name := m.Name()
	if _, exists := r.modules[name]; !exists {
		r.order = append(r.order, name)
	}
	r.modules[name] = m
}

// Get returns a module by name, or nil if not found.
func (r *Registry) Get(name string) Module {
	return r.modules[name]
}

// All returns all registered modules in registration order.
func (r *Registry) All() []Module {
	result := make([]Module, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.modules[name])
	}
	return result
}

// Names returns all registered module names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}
