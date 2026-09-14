package ai

import (
	"encoding/json"
)

// SafeguardAction represents a proposed state-changing operation suggested by the AI.
// It is returned to the user interface as an interactive card so the user can verify
// and approve or deny execution, ensuring no unintended mutations take place.
type SafeguardAction struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`         // e.g. "container:restart", "container:stop", "nginx:apply", "deployment:deploy"
	Title       string          `json:"title"`        // e.g. "Restart container 'redis-prod'"
	Description string          `json:"description"`  // e.g. "Container failed health check; restarting may clear deadlocked socket."
	DangerLevel string          `json:"danger_level"` // "low", "medium", "high"
	Permission  string          `json:"permission"`   // e.g. "container:operate", "server:write", "domain:write"
	ServerID    string          `json:"server_id,omitempty"`
	ResourceID  string          `json:"resource_id,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// Danger levels
const (
	DangerLow    = "low"
	DangerMedium = "medium"
	DangerHigh   = "high"
)
