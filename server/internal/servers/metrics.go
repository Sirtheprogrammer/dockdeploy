package servers

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthWarning  HealthStatus = "warning"
	HealthCritical HealthStatus = "critical"
)

type CPUMetrics struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
}

type MemoryMetrics struct {
	TotalBytes      uint64  `json:"total_bytes"`
	UsedBytes       uint64  `json:"used_bytes"`
	AvailableBytes  uint64  `json:"available_bytes"`
	UsedPercent     float64 `json:"used_percent"`
	SwapTotalBytes  uint64  `json:"swap_total_bytes"`
	SwapUsedBytes   uint64  `json:"swap_used_bytes"`
	SwapUsedPercent float64 `json:"swap_used_percent"`
}

type DiskMetrics struct {
	Filesystem  string  `json:"filesystem"`
	Mount       string  `json:"mount"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type DockerMetrics struct {
	Status            string `json:"status"` // "running" or "unavailable"
	ServerVersion     string `json:"server_version"`
	ContainersTotal   int    `json:"containers_total"`
	ContainersRunning int    `json:"containers_running"`
	ContainersPaused  int    `json:"containers_paused"`
	ContainersStopped int    `json:"containers_stopped"`
	ImagesCount       int    `json:"images_count"`
}

type SystemInfo struct {
	Hostname      string    `json:"hostname"`
	OS            string    `json:"os"`
	Kernel        string    `json:"kernel"`
	UptimeSeconds uint64    `json:"uptime_seconds"`
	ServerTime    time.Time `json:"server_time"`
}

type ServerMetrics struct {
	ServerID     string        `json:"server_id"`
	CollectedAt  time.Time     `json:"collected_at"`
	Health       HealthStatus  `json:"health"`
	HealthIssues []string      `json:"health_issues"`
	System       SystemInfo    `json:"system"`
	CPU          CPUMetrics    `json:"cpu"`
	Memory       MemoryMetrics `json:"memory"`
	Disk         DiskMetrics   `json:"disk"`
	Docker       DockerMetrics `json:"docker"`
}

const metricsCollectorScript = `echo "===SECTION:LOADAVG==="; cat /proc/loadavg 2>/dev/null || true; echo "===SECTION:MEMINFO==="; cat /proc/meminfo 2>/dev/null || true; echo "===SECTION:UPTIME==="; cat /proc/uptime 2>/dev/null || true; echo "===SECTION:DF==="; df -kP / 2>/dev/null || true; echo "===SECTION:STAT==="; grep '^cpu ' /proc/stat 2>/dev/null || true; echo "===SECTION:NPROC==="; nproc 2>/dev/null || grep -c '^processor' /proc/cpuinfo 2>/dev/null || true; echo "===SECTION:HOSTNAME==="; hostname 2>/dev/null || true; echo "===SECTION:UNAME==="; uname -sr 2>/dev/null || true; echo "===SECTION:OS==="; (. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME") || uname -s 2>/dev/null || true`

// Metrics collects real-time health measurements, hardware utilization, and docker metrics from the server.
func (m *Manager) Metrics(ctx context.Context, server *store.Server) (*ServerMetrics, error) {
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	cmdRes, err := conn.Run(ctx, metricsCollectorScript)
	if err != nil {
		return nil, fmt.Errorf("collect metrics: %w", err)
	}

	metrics := parseRawMetrics(cmdRes.Stdout)
	metrics.ServerID = server.ID
	metrics.CollectedAt = time.Now().UTC()
	metrics.System.ServerTime = metrics.CollectedAt

	// Check Docker daemon metrics via Docker client
	m.populateDockerMetrics(ctx, conn, server, metrics)

	// Evaluate overall health and identify potential warning or critical conditions
	evaluateHealth(metrics)

	return metrics, nil
}

func (m *Manager) populateDockerMetrics(ctx context.Context, conn *sshx.Conn, server *store.Server, metrics *ServerMetrics) {
	metrics.Docker.Status = "unavailable"
	client, err := dockerx.New(ctx, conn, server.DockerSocket)
	if err != nil {
		metrics.HealthIssues = append(metrics.HealthIssues, "Docker daemon is unreachable.")
		return
	}
	defer client.Close()

	info, err := client.Info(ctx)
	if err != nil {
		metrics.HealthIssues = append(metrics.HealthIssues, "Failed to query Docker daemon info.")
		return
	}

	metrics.Docker.Status = "running"
	metrics.Docker.ServerVersion = info.ServerVersion
	metrics.Docker.ContainersTotal = info.Containers
	metrics.Docker.ContainersRunning = info.ContainersRunning
	metrics.Docker.ContainersStopped = info.ContainersStopped
	metrics.Docker.ImagesCount = info.Images

	if metrics.CPU.Cores == 0 && info.CPUs > 0 {
		metrics.CPU.Cores = info.CPUs
	}
	if metrics.Memory.TotalBytes == 0 && info.MemoryBytes > 0 {
		metrics.Memory.TotalBytes = uint64(info.MemoryBytes)
	}
}

func parseRawMetrics(output string) *ServerMetrics {
	metrics := &ServerMetrics{
		Health:       HealthHealthy,
		HealthIssues: []string{},
		CPU:          CPUMetrics{Cores: 1},
		Disk:         DiskMetrics{Mount: "/"},
	}

	sections := make(map[string]string)
	var currentSection string
	var sectionLines []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "===SECTION:") && strings.HasSuffix(line, "===") {
			if currentSection != "" {
				sections[currentSection] = strings.Join(sectionLines, "\n")
			}
			currentSection = strings.TrimSuffix(strings.TrimPrefix(line, "===SECTION:"), "===")
			sectionLines = nil
			continue
		}
		sectionLines = append(sectionLines, line)
	}
	if currentSection != "" {
		sections[currentSection] = strings.Join(sectionLines, "\n")
	}

	// 1. Load averages
	if loadContent, ok := sections["LOADAVG"]; ok {
		fields := strings.Fields(loadContent)
		if len(fields) >= 3 {
			metrics.CPU.Load1, _ = strconv.ParseFloat(fields[0], 64)
			metrics.CPU.Load5, _ = strconv.ParseFloat(fields[1], 64)
			metrics.CPU.Load15, _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// 2. Cores count
	if nprocContent, ok := sections["NPROC"]; ok {
		trimmed := strings.TrimSpace(nprocContent)
		if cores, err := strconv.Atoi(trimmed); err == nil && cores > 0 {
			metrics.CPU.Cores = cores
		}
	}

	// 3. CPU Stat
	if statContent, ok := sections["STAT"]; ok {
		fields := strings.Fields(statContent)
		// fields[0] is "cpu", subsequent fields are numbers
		if len(fields) >= 5 {
			var vals []uint64
			for _, f := range fields[1:] {
				v, _ := strconv.ParseUint(f, 10, 64)
				vals = append(vals, v)
			}
			if len(vals) >= 4 {
				user := vals[0]
				nice := vals[1]
				sys := vals[2]
				idle := vals[3]
				var iowait uint64
				if len(vals) >= 5 {
					iowait = vals[4]
				}

				total := user + nice + sys + idle + iowait
				for _, extra := range vals[5:] {
					total += extra
				}

				idleTotal := idle + iowait
				if total > 0 && total >= idleTotal {
					active := total - idleTotal
					percent := (float64(active) / float64(total)) * 100.0
					metrics.CPU.UsagePercent = math.Round(percent*10) / 10
				}
			}
		}
	}

	// 4. Memory info
	if memContent, ok := sections["MEMINFO"]; ok {
		memScanner := bufio.NewScanner(strings.NewReader(memContent))
		var memTotal, memFree, memAvailable, buffers, cached uint64
		var swapTotal, swapFree uint64
		hasAvailable := false

		for memScanner.Scan() {
			parts := strings.SplitN(memScanner.Text(), ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			valStr := strings.TrimSpace(parts[1])
			valFields := strings.Fields(valStr)
			if len(valFields) == 0 {
				continue
			}
			kb, err := strconv.ParseUint(valFields[0], 10, 64)
			if err != nil {
				continue
			}
			bytes := kb * 1024

			switch key {
			case "MemTotal":
				memTotal = bytes
			case "MemFree":
				memFree = bytes
			case "MemAvailable":
				memAvailable = bytes
				hasAvailable = true
			case "Buffers":
				buffers = bytes
			case "Cached":
				cached = bytes
			case "SwapTotal":
				swapTotal = bytes
			case "SwapFree":
				swapFree = bytes
			}
		}

		if !hasAvailable {
			memAvailable = memFree + buffers + cached
			if memAvailable > memTotal {
				memAvailable = memTotal
			}
		}

		metrics.Memory.TotalBytes = memTotal
		metrics.Memory.AvailableBytes = memAvailable
		if memTotal >= memAvailable {
			metrics.Memory.UsedBytes = memTotal - memAvailable
		}
		if memTotal > 0 {
			pct := (float64(metrics.Memory.UsedBytes) / float64(memTotal)) * 100.0
			metrics.Memory.UsedPercent = math.Round(pct*10) / 10
		}

		metrics.Memory.SwapTotalBytes = swapTotal
		if swapTotal >= swapFree {
			metrics.Memory.SwapUsedBytes = swapTotal - swapFree
		}
		if swapTotal > 0 {
			swapPct := (float64(metrics.Memory.SwapUsedBytes) / float64(swapTotal)) * 100.0
			metrics.Memory.SwapUsedPercent = math.Round(swapPct*10) / 10
		}
	}

	// 5. Uptime
	if uptimeContent, ok := sections["UPTIME"]; ok {
		fields := strings.Fields(uptimeContent)
		if len(fields) > 0 {
			if upSecs, err := strconv.ParseFloat(fields[0], 64); err == nil && upSecs >= 0 {
				metrics.System.UptimeSeconds = uint64(upSecs)
			}
		}
	}

	// 6. Disk space (df -kP /)
	if dfContent, ok := sections["DF"]; ok {
		lines := strings.Split(strings.TrimSpace(dfContent), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 6 {
				metrics.Disk.Filesystem = fields[0]
				totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
				usedKB, _ := strconv.ParseUint(fields[2], 10, 64)
				freeKB, _ := strconv.ParseUint(fields[3], 10, 64)
				metrics.Disk.TotalBytes = totalKB * 1024
				metrics.Disk.UsedBytes = usedKB * 1024
				metrics.Disk.FreeBytes = freeKB * 1024
				metrics.Disk.Mount = fields[5]

				if metrics.Disk.TotalBytes > 0 {
					pct := (float64(metrics.Disk.UsedBytes) / float64(metrics.Disk.TotalBytes)) * 100.0
					metrics.Disk.UsedPercent = math.Round(pct*10) / 10
				}
			}
		}
	}

	// 7. System Information
	if hostContent, ok := sections["HOSTNAME"]; ok {
		metrics.System.Hostname = strings.TrimSpace(hostContent)
	}
	if unameContent, ok := sections["UNAME"]; ok {
		metrics.System.Kernel = strings.TrimSpace(unameContent)
	}
	if osContent, ok := sections["OS"]; ok {
		metrics.System.OS = strings.TrimSpace(osContent)
	}

	return metrics
}

func evaluateHealth(metrics *ServerMetrics) {
	var criticalIssues []string
	var warningIssues []string

	// Disk health
	if metrics.Disk.UsedPercent >= 90.0 {
		criticalIssues = append(criticalIssues, fmt.Sprintf("Critically low disk space: %.1f%% used on %s", metrics.Disk.UsedPercent, metrics.Disk.Mount))
	} else if metrics.Disk.UsedPercent >= 80.0 {
		warningIssues = append(warningIssues, fmt.Sprintf("High disk space usage: %.1f%% used on %s", metrics.Disk.UsedPercent, metrics.Disk.Mount))
	}

	// Memory health
	if metrics.Memory.UsedPercent >= 90.0 {
		criticalIssues = append(criticalIssues, fmt.Sprintf("Critically high memory usage: %.1f%% used", metrics.Memory.UsedPercent))
	} else if metrics.Memory.UsedPercent >= 80.0 {
		warningIssues = append(warningIssues, fmt.Sprintf("High memory usage: %.1f%% used", metrics.Memory.UsedPercent))
	}

	// CPU Load health
	if metrics.CPU.Cores > 0 && metrics.CPU.Load15 > float64(metrics.CPU.Cores)*2.0 {
		warningIssues = append(warningIssues, fmt.Sprintf("High 15-minute load average: %.2f (server has %d CPU cores)", metrics.CPU.Load15, metrics.CPU.Cores))
	}

	// Combine issues
	metrics.HealthIssues = append(metrics.HealthIssues, criticalIssues...)
	metrics.HealthIssues = append(metrics.HealthIssues, warningIssues...)
	if metrics.HealthIssues == nil {
		metrics.HealthIssues = []string{}
	}

	if len(criticalIssues) > 0 || (metrics.Docker.Status == "unavailable") {
		metrics.Health = HealthCritical
	} else if len(warningIssues) > 0 {
		metrics.Health = HealthWarning
	} else {
		metrics.Health = HealthHealthy
	}
}
