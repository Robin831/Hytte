package forge

import (
	"fmt"
	"testing"
)

func TestGHCIStatus(t *testing.T) {
	tests := []struct {
		name        string
		checks      []ghCheckStatus
		wantPassing bool
		wantPending bool
	}{
		{
			name:        "empty rollup",
			checks:      []ghCheckStatus{},
			wantPassing: false,
			wantPending: false,
		},
		{
			name:        "nil rollup",
			checks:      nil,
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "all StatusContext SUCCESS",
			checks: []ghCheckStatus{
				{State: "SUCCESS"},
				{State: "SUCCESS"},
			},
			wantPassing: true,
			wantPending: false,
		},
		{
			name: "StatusContext PENDING",
			checks: []ghCheckStatus{
				{State: "SUCCESS"},
				{State: "PENDING"},
			},
			wantPassing: false,
			wantPending: true,
		},
		{
			name: "StatusContext EXPECTED (pending)",
			checks: []ghCheckStatus{
				{State: "EXPECTED"},
			},
			wantPassing: false,
			wantPending: true,
		},
		{
			name: "StatusContext FAILURE",
			checks: []ghCheckStatus{
				{State: "SUCCESS"},
				{State: "FAILURE"},
			},
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "StatusContext ERROR",
			checks: []ghCheckStatus{
				{State: "ERROR"},
			},
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "CheckRun COMPLETED SUCCESS",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
			},
			wantPassing: true,
			wantPending: false,
		},
		{
			name: "CheckRun COMPLETED NEUTRAL (treated as pass)",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "NEUTRAL"},
			},
			wantPassing: true,
			wantPending: false,
		},
		{
			name: "CheckRun COMPLETED SKIPPED (treated as pass)",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "SKIPPED"},
			},
			wantPassing: true,
			wantPending: false,
		},
		{
			name: "CheckRun COMPLETED FAILURE",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "FAILURE"},
			},
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "CheckRun COMPLETED CANCELLED",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "CANCELLED"},
			},
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "CheckRun COMPLETED TIMED_OUT",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "TIMED_OUT"},
			},
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "CheckRun IN_PROGRESS (pending)",
			checks: []ghCheckStatus{
				{Status: "IN_PROGRESS", Conclusion: ""},
			},
			wantPassing: false,
			wantPending: true,
		},
		{
			name: "CheckRun QUEUED (pending)",
			checks: []ghCheckStatus{
				{Status: "QUEUED", Conclusion: ""},
			},
			wantPassing: false,
			wantPending: true,
		},
		{
			name: "mixed: one passing CheckRun, one pending StatusContext",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{State: "PENDING"},
			},
			wantPassing: false,
			wantPending: true,
		},
		{
			name: "mixed: one pending CheckRun, one failed StatusContext",
			checks: []ghCheckStatus{
				{Status: "IN_PROGRESS"},
				{State: "FAILURE"},
			},
			wantPassing: false,
			wantPending: false,
		},
		{
			name: "all passing: mix of CheckRun SUCCESS and StatusContext SUCCESS",
			checks: []ghCheckStatus{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{State: "SUCCESS"},
				{Status: "COMPLETED", Conclusion: "SKIPPED"},
			},
			wantPassing: true,
			wantPending: false,
		},
		{
			name: "lowercase state is handled (case-insensitive)",
			checks: []ghCheckStatus{
				{State: "success"},
			},
			wantPassing: true,
			wantPending: false,
		},
		{
			name: "lowercase status/conclusion is handled (case-insensitive)",
			checks: []ghCheckStatus{
				{Status: "completed", Conclusion: "failure"},
			},
			wantPassing: false,
			wantPending: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPassing, gotPending := ghCIStatus(tt.checks)
			if gotPassing != tt.wantPassing || gotPending != tt.wantPending {
				t.Errorf("ghCIStatus(%v) = (passing=%v, pending=%v), want (passing=%v, pending=%v)",
					tt.checks, gotPassing, gotPending, tt.wantPassing, tt.wantPending)
			}
		})
	}
}

func TestFilterExternal_CompositeKey(t *testing.T) {
	// Verifies that PRs with the same number but different repos are treated
	// as distinct — only the forge-tracked (repo, number) pair is excluded.
	anvilToRepo := map[string]string{
		"hytte": "Robin831/Hytte",
		"forge": "Robin831/Forge",
	}

	allGitHub := []ExternalPR{
		{Number: 10, Anvil: "Robin831/Hytte"},
		{Number: 20, Anvil: "Robin831/Hytte"},
		{Number: 10, Anvil: "Robin831/Forge"}, // same number, different repo
		{Number: 30, Anvil: "Robin831/Forge"},
	}

	tests := []struct {
		name     string
		forgePRs []PR
		wantKeys []string // "repo:number"
	}{
		{
			name:     "no forge PRs — all external pass through",
			forgePRs: nil,
			wantKeys: []string{"Robin831/Hytte:10", "Robin831/Hytte:20", "Robin831/Forge:10", "Robin831/Forge:30"},
		},
		{
			name:     "exclude Hytte PR #10 only, not Forge PR #10",
			forgePRs: []PR{{Number: 10, Anvil: "hytte"}},
			wantKeys: []string{"Robin831/Hytte:20", "Robin831/Forge:10", "Robin831/Forge:30"},
		},
		{
			name:     "exclude both PR #10s from different repos",
			forgePRs: []PR{{Number: 10, Anvil: "hytte"}, {Number: 10, Anvil: "forge"}},
			wantKeys: []string{"Robin831/Hytte:20", "Robin831/Forge:30"},
		},
		{
			name:     "forge PR with unknown anvil is not excluded",
			forgePRs: []PR{{Number: 10, Anvil: "unknown"}},
			wantKeys: []string{"Robin831/Hytte:10", "Robin831/Hytte:20", "Robin831/Forge:10", "Robin831/Forge:30"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterExternal(allGitHub, tt.forgePRs, anvilToRepo)
			if len(result) != len(tt.wantKeys) {
				t.Fatalf("got %d PRs, want %d", len(result), len(tt.wantKeys))
			}
			for i, ep := range result {
				gotKey := fmt.Sprintf("%s:%d", ep.Anvil, ep.Number)
				if gotKey != tt.wantKeys[i] {
					t.Errorf("result[%d] = %q, want %q", i, gotKey, tt.wantKeys[i])
				}
			}
		})
	}
}
