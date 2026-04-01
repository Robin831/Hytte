package forge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ForgeConfig mirrors the top-level structure of ~/.forge/config.yaml.
type ForgeConfig struct {
	Anvils        map[string]AnvilConfig `json:"anvils" yaml:"anvils"`
	Settings      ForgeSettings          `json:"settings" yaml:"settings"`
	Notifications NotificationConfig     `json:"notifications" yaml:"notifications"`
}

// AnvilConfig represents per-anvil (repository) settings.
type AnvilConfig struct {
	Path                   string   `json:"path" yaml:"path"`
	MaxSmiths              int      `json:"max_smiths" yaml:"max_smiths"`
	AutoDispatch           string   `json:"auto_dispatch" yaml:"auto_dispatch"`
	AutoDispatchMinPriority int     `json:"auto_dispatch_min_priority" yaml:"auto_dispatch_min_priority"`
	AutoDispatchTag        string   `json:"auto_dispatch_tag" yaml:"auto_dispatch_tag"`
	AutoMerge              bool     `json:"auto_merge" yaml:"auto_merge"`
	WicketEnabled          bool     `json:"wicket_enabled,omitempty" yaml:"wicket_enabled,omitempty"`
	WicketAutoDispatch     bool     `json:"wicket_auto_dispatch,omitempty" yaml:"wicket_auto_dispatch,omitempty"`
	WicketTrustedUsers     []string `json:"wicket_trusted_users,omitempty" yaml:"wicket_trusted_users,omitempty"`
}

// ForgeSettings holds global forge daemon settings.
type ForgeSettings struct {
	MaxTotalSmiths           int      `json:"max_total_smiths" yaml:"max_total_smiths"`
	PollInterval             string   `json:"poll_interval" yaml:"poll_interval"`
	SmithTimeout             string   `json:"smith_timeout" yaml:"smith_timeout"`
	StaleInterval            string   `json:"stale_interval" yaml:"stale_interval"`
	BellowsInterval          string   `json:"bellows_interval" yaml:"bellows_interval"`
	RateLimitBackoff         string   `json:"rate_limit_backoff" yaml:"rate_limit_backoff"`
	MaxCIFixAttempts         int      `json:"max_ci_fix_attempts" yaml:"max_ci_fix_attempts"`
	MaxPipelineIterations    int      `json:"max_pipeline_iterations" yaml:"max_pipeline_iterations"`
	MaxRebaseAttempts        int      `json:"max_rebase_attempts" yaml:"max_rebase_attempts"`
	MaxReviewAttempts        int      `json:"max_review_attempts" yaml:"max_review_attempts"`
	MaxReviewFixAttempts     int      `json:"max_review_fix_attempts" yaml:"max_review_fix_attempts"`
	Providers                []string `json:"providers" yaml:"providers"`
	SmithProviders           []string `json:"smith_providers" yaml:"smith_providers"`
	ClaudeFlags              []string `json:"claude_flags" yaml:"claude_flags"`
	AutoLearnRules           bool     `json:"auto_learn_rules" yaml:"auto_learn_rules"`
	SchematicEnabled         bool     `json:"schematic_enabled" yaml:"schematic_enabled"`
	CrucibleEnabled          bool     `json:"crucible_enabled" yaml:"crucible_enabled"`
	WicketEnabled            bool     `json:"wicket_enabled" yaml:"wicket_enabled"`
	CopilotDailyRequestLimit int     `json:"copilot_daily_request_limit" yaml:"copilot_daily_request_limit"`
	SmelterInterval          string   `json:"smelter_interval" yaml:"smelter_interval"`
}

// NotificationConfig holds notification-related settings.
type NotificationConfig struct {
	Enabled  bool           `json:"enabled" yaml:"enabled"`
	Teams    TeamsConfig    `json:"teams" yaml:"teams"`
	Webhooks []WebhookEntry `json:"webhooks" yaml:"webhooks"`
}

// TeamsConfig holds Microsoft Teams notification settings.
type TeamsConfig struct {
	WebhookURL string `json:"webhook_url" yaml:"webhook_url"`
}

// WebhookEntry represents a notification webhook endpoint.
type WebhookEntry struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url" yaml:"url"`
}

const maxConfigSize = 256 * 1024 // 256 KiB

// configPath returns the path to ~/.forge/config.yaml.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("forge: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".forge", "config.yaml"), nil
}

// GetConfigHandler reads ~/.forge/config.yaml and returns it as JSON.
func GetConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfgPath, err := configPath()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot resolve config path")
			return
		}

		data, err := os.ReadFile(cfgPath)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "forge config file not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to read config file")
			return
		}

		var cfg ForgeConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse config file")
			return
		}

		writeJSON(w, http.StatusOK, cfg)
	}
}

// PutConfigHandler accepts a JSON body, converts it to YAML, and writes it to
// ~/.forge/config.yaml. The forge daemon hot-reloads on file change.
func PutConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfgPath, err := configPath()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot resolve config path")
			return
		}

		lr := &io.LimitedReader{R: r.Body, N: maxConfigSize + 1}
		body, err := io.ReadAll(lr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		if int64(len(body)) > maxConfigSize {
			writeError(w, http.StatusRequestEntityTooLarge, "config too large")
			return
		}

		var cfg ForgeConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		yamlData, err := yaml.Marshal(&cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to serialize config")
			return
		}

		if err := os.WriteFile(cfgPath, yamlData, 0644); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write config file")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}
