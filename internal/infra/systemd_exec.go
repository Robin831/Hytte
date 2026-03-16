package infra

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// execSystemdChecker queries systemd via the systemctl command.
type execSystemdChecker struct{}

// UnitStatus runs "systemctl show" to get the active and sub-state of a unit.
func (c *execSystemdChecker) UnitStatus(unit string) (activeState, subState string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "show", "--property=ActiveState,SubState", "--no-pager", "--", unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("systemctl failed: %w; output: %s", err, strings.TrimSpace(string(out)))
	}

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "ActiveState":
			activeState = val
		case "SubState":
			subState = val
		}
	}

	if activeState == "" {
		return "", "", fmt.Errorf("could not determine state for unit %s", unit)
	}

	return activeState, subState, nil
}
