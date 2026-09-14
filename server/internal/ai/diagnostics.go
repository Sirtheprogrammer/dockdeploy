package ai

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

type DiagnosticReport struct {
	ServerID       string                  `json:"server_id"`
	ServerName     string                  `json:"server_name"`
	CollectedAt    time.Time               `json:"collected_at"`
	HealthIssues   []string                `json:"health_issues"`
	Metrics        *servers.ServerMetrics  `json:"metrics,omitempty"`
	Containers     []dockerx.Container     `json:"containers,omitempty"`
	ContainerLogs  map[string]string       `json:"container_logs,omitempty"` // containerID -> logs
	ProposedAction *SafeguardAction        `json:"proposed_action,omitempty"`
	SummaryMarkdown string                 `json:"summary_markdown"`
}

type bufferWriter struct {
	buf bytes.Buffer
}

func (b *bufferWriter) Write(p []byte) (n int, err error) {
	return b.buf.Write(p)
}

// CollectServerDiagnostic gathers a full diagnostic snapshot from a server.
func CollectServerDiagnostic(
	ctx context.Context,
	srvMgr *servers.Manager,
	server *store.Server,
	targetContainerID string,
) (*DiagnosticReport, error) {
	report := &DiagnosticReport{
		ServerID:      server.ID,
		ServerName:    server.Name,
		CollectedAt:   time.Now().UTC(),
		ContainerLogs: make(map[string]string),
	}

	var issues []string

	// 1. Collect Metrics
	metrics, err := srvMgr.Metrics(ctx, server)
	if err == nil && metrics != nil {
		report.Metrics = metrics
		issues = append(issues, metrics.HealthIssues...)
	} else if err != nil {
		issues = append(issues, fmt.Sprintf("Failed to collect system metrics: %v", err))
	}

	// 2. Connect Docker Session
	session, err := srvMgr.Session(ctx, server)
	if err != nil {
		issues = append(issues, fmt.Sprintf("Docker daemon session unavailable: %v", err))
	} else {
		defer session.Close()

		// List Containers
		dockerCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		containers, err := session.Docker.ListContainers(dockerCtx)
		cancel()

		if err == nil {
			report.Containers = containers
			for _, c := range containers {
				if c.State == "restarting" {
					issues = append(issues, fmt.Sprintf("Container '%s' is in crash-loop (restarting)", c.Name))
				} else if c.State == "dead" {
					issues = append(issues, fmt.Sprintf("Container '%s' is dead", c.Name))
				}
			}
		}

		// Fetch logs for targetContainerID or failing containers
		containersToLog := []string{}
		if targetContainerID != "" {
			containersToLog = append(containersToLog, targetContainerID)
		} else {
			// Auto-tail restarting or dead containers
			for _, c := range containers {
				if c.State == "restarting" || c.State == "dead" {
					containersToLog = append(containersToLog, c.ID)
					if len(containersToLog) >= 2 {
						break
					}
				}
			}
		}

		for _, cid := range containersToLog {
			logCtx, logCancel := context.WithTimeout(ctx, 10*time.Second)
			var stdoutBuf, stderrBuf bufferWriter
			opts := dockerx.LogOptions{Tail: "80", Follow: false}
			_ = session.Docker.Logs(logCtx, cid, opts, &stdoutBuf, &stderrBuf)
			logCancel()

			var combined strings.Builder
			if stderrBuf.buf.Len() > 0 {
				combined.WriteString("=== STDERR ===\n")
				combined.WriteString(stderrBuf.buf.String())
				combined.WriteString("\n")
			}
			if stdoutBuf.buf.Len() > 0 {
				combined.WriteString("=== STDOUT ===\n")
				combined.WriteString(stdoutBuf.buf.String())
			}
			report.ContainerLogs[cid] = strings.TrimSpace(combined.String())
		}
	}

	report.HealthIssues = issues

	// 3. Build Markdown Summary
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Diagnostics for Server: **%s** (`%s`)\n\n", server.Name, server.Host))

	if len(issues) > 0 {
		sb.WriteString("⚠️ **Detected Issues:**\n")
		for _, issue := range issues {
			sb.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("✅ **Health Status:** All probed metrics within normal limits.\n\n")
	}

	if report.Metrics != nil {
		m := report.Metrics
		sb.WriteString("#### Resource Utilization\n")
		sb.WriteString(fmt.Sprintf("- **CPU Usage:** %.1f%% (%d cores, Load: %.2f, %.2f, %.2f)\n",
			m.CPU.UsagePercent, m.CPU.Cores, m.CPU.Load1, m.CPU.Load5, m.CPU.Load15))
		sb.WriteString(fmt.Sprintf("- **Memory:** %.1f%% used (%.2f GB / %.2f GB)\n",
			m.Memory.UsedPercent, float64(m.Memory.UsedBytes)/(1024*1024*1024), float64(m.Memory.TotalBytes)/(1024*1024*1024)))
		if m.Memory.SwapTotalBytes > 0 {
			sb.WriteString(fmt.Sprintf("- **Swap:** %.1f%% used (%.2f GB / %.2f GB)\n",
				m.Memory.SwapUsedPercent, float64(m.Memory.SwapUsedBytes)/(1024*1024*1024), float64(m.Memory.SwapTotalBytes)/(1024*1024*1024)))
		}
		sb.WriteString(fmt.Sprintf("- **Disk (/):** %.1f%% used (%.2f GB / %.2f GB)\n",
			m.Disk.UsedPercent, float64(m.Disk.UsedBytes)/(1024*1024*1024), float64(m.Disk.TotalBytes)/(1024*1024*1024)))
		sb.WriteString(fmt.Sprintf("- **Docker:** %s (Containers: %d running, %d stopped, %d total)\n\n",
			m.Docker.Status, m.Docker.ContainersRunning, m.Docker.ContainersStopped, m.Docker.ContainersTotal))
	}

	if len(report.Containers) > 0 {
		sb.WriteString("#### Containers Overview\n")
		sb.WriteString("| Name | State | Status | Image | Ports |\n")
		sb.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, c := range report.Containers {
			ports := []string{}
			for _, p := range c.Ports {
				if p.HostPort > 0 {
					ports = append(ports, fmt.Sprintf("%d:%d", p.HostPort, p.Container))
				} else {
					ports = append(ports, fmt.Sprintf("%d", p.Container))
				}
			}
			portsStr := strings.Join(ports, ", ")
			if portsStr == "" {
				portsStr = "-"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | `%s` | %s |\n", c.Name, c.State, c.Status, c.Image, portsStr))
		}
		sb.WriteString("\n")
	}

	if len(report.ContainerLogs) > 0 {
		sb.WriteString("#### Container Log Excerpts\n")
		for cid, logs := range report.ContainerLogs {
			name := cid
			for _, c := range report.Containers {
				if c.ID == cid {
					name = c.Name
					break
				}
			}
			sb.WriteString(fmt.Sprintf("**Logs for `%s`:**\n```\n%s\n```\n\n", name, logs))
		}
	}

	report.SummaryMarkdown = sb.String()
	return report, nil
}
