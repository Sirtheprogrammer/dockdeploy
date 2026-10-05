package ai

import (
	"context"
	"testing"
	"time"
)

func TestDetectAgents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := DetectAgents(ctx)
	t.Logf("Detected agents: %d, Recommended: %s, HasLocal: %v", len(res.Agents), res.RecommendedID, res.HasLocalAgents)

	// Detection probes the host machine for installed CLIs and running local
	// LLM daemons, so whether any agent is actually *available* depends on
	// what happens to be on the machine running the test (nothing, on a bare
	// CI runner). That can't be asserted on. What every environment must
	// agree on is that detection itself runs cleanly and reports on the
	// fixed set of agents it always probes, each with well-formed fields.
	const alwaysProbed = 5 // ollama, antigravity, claude, hermes, copilot
	if len(res.Agents) < alwaysProbed {
		t.Fatalf("expected at least %d probed agents, got %d", alwaysProbed, len(res.Agents))
	}

	for _, a := range res.Agents {
		t.Logf("Agent: %s (id=%s, type=%s, available=%v, status=%s, models=%v)",
			a.Name, a.ID, a.Type, a.Available, a.Status, a.Models)

		if a.ID == "" || a.Name == "" || a.Type == "" || a.Status == "" {
			t.Errorf("agent %+v has an empty required field", a)
		}
		if a.Available && a.Status != "online" && a.Status != "ready" {
			t.Errorf("agent %s marked available but status is %q", a.ID, a.Status)
		}
		if !a.Available && a.Status != "offline" && a.Status != "not_found" {
			t.Errorf("agent %s marked unavailable but status is %q", a.ID, a.Status)
		}
	}

	if res.RecommendedID != "" && !res.HasLocalAgents {
		t.Errorf("RecommendedID %q set without HasLocalAgents", res.RecommendedID)
	}
}
