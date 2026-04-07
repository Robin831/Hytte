package forge

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Robin831/Hytte/internal/encryption"
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

// isRegularFile checks that the path is a regular file (not a symlink or other
// special file type). This prevents symlink-based attacks on config read/write.
func isRegularFile(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to follow symlink: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	return nil
}

// isRegularDir checks that the directory path itself is not a symlink.
// This prevents attacks where ~/.forge is replaced with a symlink to another
// directory, causing reads/writes to occur outside the expected location.
func isRegularDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to follow symlink directory: %s", dir)
	}
	if !fi.Mode().IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	return nil
}

// decryptSensitiveFields decrypts encrypted fields (webhook URLs) after
// reading from disk.
//
// If decryption fails (e.g. wrong key or corrupt ciphertext), the field is
// cleared instead of leaving the ciphertext in place. This avoids exposing
// opaque ciphertext to callers and prevents it from being re-encrypted on a
// subsequent write, which would otherwise corrupt the stored value further.
func decryptSensitiveFields(cfg *ForgeConfig) {
	if cfg.Notifications.Teams.WebhookURL != "" {
		if dec, err := encryption.DecryptField(cfg.Notifications.Teams.WebhookURL); err == nil {
			cfg.Notifications.Teams.WebhookURL = dec
		} else {
			log.Printf("forge: failed to decrypt Teams webhook URL: %v", err)
			cfg.Notifications.Teams.WebhookURL = ""
		}
	}
	for i := range cfg.Notifications.Webhooks {
		if cfg.Notifications.Webhooks[i].URL != "" {
			if dec, err := encryption.DecryptField(cfg.Notifications.Webhooks[i].URL); err == nil {
				cfg.Notifications.Webhooks[i].URL = dec
			} else {
				log.Printf("forge: failed to decrypt webhook URL at index %d: %v", i, err)
				cfg.Notifications.Webhooks[i].URL = ""
			}
		}
	}
}

// encryptSensitiveFields encrypts sensitive fields (webhook URLs) before
// writing to disk.
func encryptSensitiveFields(cfg *ForgeConfig) error {
	if cfg.Notifications.Teams.WebhookURL != "" {
		enc, err := encryption.EncryptField(cfg.Notifications.Teams.WebhookURL)
		if err != nil {
			return fmt.Errorf("encrypt teams webhook URL: %w", err)
		}
		cfg.Notifications.Teams.WebhookURL = enc
	}
	for i := range cfg.Notifications.Webhooks {
		if cfg.Notifications.Webhooks[i].URL != "" {
			enc, err := encryption.EncryptField(cfg.Notifications.Webhooks[i].URL)
			if err != nil {
				return fmt.Errorf("encrypt webhook URL: %w", err)
			}
			cfg.Notifications.Webhooks[i].URL = enc
		}
	}
	return nil
}

// loadForgeConfig reads and parses ~/.forge/config.yaml. It returns an error
// if the file is missing or cannot be parsed; callers should fall back to
// defaults in that case.
func loadForgeConfig() (*ForgeConfig, error) {
	cfgPath, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var cfg ForgeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetConfigHandler reads ~/.forge/config.yaml and returns it as JSON.
func GetConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfgPath, err := configPath()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot resolve config path")
			return
		}

		forgeDir := filepath.Dir(cfgPath)
		if err := isRegularDir(forgeDir); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusForbidden, "config directory is not a regular directory")
			return
		}

		if err := isRegularFile(cfgPath); err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "forge config file not found")
				return
			}
			writeError(w, http.StatusForbidden, "config path is not a regular file")
			return
		}

		fi, err := os.Stat(cfgPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to stat config file")
			return
		}
		if fi.Size() > maxConfigSize {
			writeError(w, http.StatusRequestEntityTooLarge, "config file too large")
			return
		}

		data, err := os.ReadFile(cfgPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read config file")
			return
		}

		var cfg ForgeConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse config file")
			return
		}

		decryptSensitiveFields(&cfg)

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

		forgeDir := filepath.Dir(cfgPath)
		if err := isRegularDir(forgeDir); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusForbidden, "config directory is not a regular directory")
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

		// Check that the existing config path is not a symlink before overwriting.
		if err := isRegularFile(cfgPath); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusForbidden, "config path is not a regular file")
			return
		}

		var cfg ForgeConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		if err := encryptSensitiveFields(&cfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt sensitive fields")
			return
		}

		yamlData, err := yaml.Marshal(&cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to serialize config")
			return
		}

		// Ensure the .forge directory exists. MkdirAll does not tighten permissions
		// on existing directories, so enforce 0700 explicitly afterwards.
		if err := os.MkdirAll(forgeDir, 0700); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create config directory")
			return
		}
		if err := os.Chmod(forgeDir, 0700); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set config directory permissions")
			return
		}

		// Write atomically: write to a temp file then rename into place so a
		// crash mid-write cannot leave a truncated or invalid config.yaml.
		tmpFile, err := os.CreateTemp(forgeDir, "config-*.yaml.tmp")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write config file")
			return
		}
		tmpPath := tmpFile.Name()
		committed := false
		defer func() {
			if !committed {
				os.Remove(tmpPath)
			}
		}()
		if _, err := tmpFile.Write(yamlData); err != nil {
			tmpFile.Close()
			writeError(w, http.StatusInternalServerError, "failed to write config file")
			return
		}
		if err := tmpFile.Chmod(0600); err != nil {
			tmpFile.Close()
			writeError(w, http.StatusInternalServerError, "failed to write config file")
			return
		}
		if err := tmpFile.Close(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write config file")
			return
		}
		if err := os.Rename(tmpPath, cfgPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write config file")
			return
		}
		committed = true

		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}
