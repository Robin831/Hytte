package infra

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type versionsCache struct {
	mu        sync.Mutex
	data      map[string]string
	fetchedAt time.Time
}

var (
	versionsCacheInstance versionsCache
	versionsGroup         singleflight.Group
)

type versionEntry struct {
	name string
	cmd  string
	args []string
	dir  string
}

// commandRunner abstracts exec.CommandContext so tests can inject stubs.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// forgeRepoDir returns the Forge repository directory from the FORGE_REPO_DIR
// environment variable. Returns an empty string if the variable is not set,
// which causes the forge_head entry to be omitted from results.
func forgeRepoDir() string {
	return os.Getenv("FORGE_REPO_DIR")
}

func getVersions(runner commandRunner) map[string]string {
	entries := []versionEntry{
		{name: "claude", cmd: "claude", args: []string{"--version"}},
		{name: "forge", cmd: "forge", args: []string{"version"}},
		{name: "bd", cmd: "bd", args: []string{"version"}},
		{name: "go", cmd: "go", args: []string{"version"}},
		{name: "node", cmd: "node", args: []string{"--version"}},
		{name: "npm", cmd: "npm", args: []string{"--version"}},
		{name: "gh", cmd: "gh", args: []string{"--version"}},
		{name: "git", cmd: "git", args: []string{"--version"}},
		{name: "dolt", cmd: "dolt", args: []string{"version"}},
	}

	if forgeDir := forgeRepoDir(); forgeDir != "" {
		entries = append(entries, versionEntry{
			name: "forge_head",
			cmd:  "git",
			args: []string{"rev-parse", "--short", "HEAD"},
			dir:  forgeDir,
		})
	}

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := runner(ctx, e.cmd, e.args...)
		cancel()
		if err != nil {
			log.Printf("versions: command %q failed: %v; output: %s", e.cmd, err, strings.TrimSpace(string(out)))
			result[e.name] = "unavailable"
		} else {
			result[e.name] = strings.TrimSpace(string(out))
		}
	}
	return result
}

// versionsHandlerWithRunner is the testable core of VersionsHandler.
func versionsHandlerWithRunner(runner commandRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionsCacheInstance.mu.Lock()
		if versionsCacheInstance.data != nil && time.Since(versionsCacheInstance.fetchedAt) < 5*time.Minute {
			data := versionsCacheInstance.data
			versionsCacheInstance.mu.Unlock()
			writeJSON(w, http.StatusOK, data)
			return
		}
		versionsCacheInstance.mu.Unlock()

		v, _, _ := versionsGroup.Do("fetch", func() (any, error) {
			versions := getVersions(runner)
			versionsCacheInstance.mu.Lock()
			versionsCacheInstance.data = versions
			versionsCacheInstance.fetchedAt = time.Now()
			versionsCacheInstance.mu.Unlock()
			return versions, nil
		})

		writeJSON(w, http.StatusOK, v.(map[string]string))
	}
}

// VersionsHandler returns a JSON object mapping tool names to version strings.
// Results are cached in-memory for 5 minutes. Concurrent requests during a
// cache miss share a single fetch via singleflight to avoid spawning multiple
// shell invocations simultaneously.
func VersionsHandler() http.HandlerFunc {
	return versionsHandlerWithRunner(defaultCommandRunner)
}
