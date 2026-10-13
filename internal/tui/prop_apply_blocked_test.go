package tui

import (
	"testing"
	"time"

	"github.com/radix29/gosmo"
)

// blockedLabel names the blocker of the run's own wait, skipping a longer wait
// of another of this process's sessions that began before the run did.
func TestBlockedLabel(t *testing.T) {
	panel := gosmo.BlockedRequest{SessionID: 70, WaitTimeMS: 300_000, BlockingSessionID: 80, BlockerLogin: "bob"}
	apply := gosmo.BlockedRequest{SessionID: 61, WaitTimeMS: 12_400, BlockingSessionID: 57,
		BlockerLogin: "sa", BlockerHost: "WS1", BlockerProgram: "SSMS"}
	for _, tc := range []struct {
		name string
		reqs []gosmo.BlockedRequest
		want string
	}{
		{"none", nil, "Applying..."},
		{"only an older wait", []gosmo.BlockedRequest{panel}, "Applying..."},
		{"the run's wait under an older one", []gosmo.BlockedRequest{panel, apply}, "Blocked 12s by session 57 (sa@WS1, SSMS)"},
		{"blocker with no login", []gosmo.BlockedRequest{{WaitTimeMS: 3000, BlockingSessionID: 9, BlockerProgram: "agent"}},
			"Blocked 3s by session 9 (agent)"},
		{"system blocker", []gosmo.BlockedRequest{{WaitTimeMS: 3000, BlockingSessionID: 9}}, "Blocked 3s by session 9"},
	} {
		if got := blockedLabel("Applying...", tc.reqs, 13*time.Second); got != tc.want {
			t.Errorf("%s: blockedLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}
