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

	for _, a := range res.Agents {
		t.Logf("Agent: %s (id=%s, type=%s, available=%v, status=%s, models=%v)",
			a.Name, a.ID, a.Type, a.Available, a.Status, a.Models)
	}

	if !res.HasLocalAgents {
		t.Errorf("expected at least one local agent to be detected (e.g. ollama, agy, claude)")
	}
}
