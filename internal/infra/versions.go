package infra

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type versionsCache struct {
	mu        sync.Mutex
	data      map[string]string
	fetchedAt time.Time
}

var versionsCacheInstance versionsCache

type versionEntry struct {
	name string
	cmd  string
	args []string
	dir  string
}

func forgeRepoDir() string {
	if d := os.Getenv("FORGE_REPO_DIR"); d != "" {
		return d
	}
	return "/home/robin/source/Hytte"
}

func getVersions() map[string]string {
	forgeDir := forgeRepoDir()

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
		{name: "forge_head", cmd: "git", args: []string{"rev-parse", "--short", "HEAD"}, dir: forgeDir},
	}

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		cmd := exec.Command(e.cmd, e.args...)
		if e.dir != "" {
			cmd.Dir = e.dir
		}
		out, err := cmd.Output()
		if err != nil {
			result[e.name] = "error: " + err.Error()
		} else {
			result[e.name] = strings.TrimSpace(string(out))
		}
	}
	return result
}

// VersionsHandler returns a JSON object mapping tool names to version strings.
// Results are cached in-memory for 5 minutes.
func VersionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionsCacheInstance.mu.Lock()
		if versionsCacheInstance.data != nil && time.Since(versionsCacheInstance.fetchedAt) < 5*time.Minute {
			data := versionsCacheInstance.data
			versionsCacheInstance.mu.Unlock()
			writeJSON(w, http.StatusOK, data)
			return
		}
		versionsCacheInstance.mu.Unlock()

		versions := getVersions()

		versionsCacheInstance.mu.Lock()
		versionsCacheInstance.data = versions
		versionsCacheInstance.fetchedAt = time.Now()
		versionsCacheInstance.mu.Unlock()

		writeJSON(w, http.StatusOK, versions)
	}
}
