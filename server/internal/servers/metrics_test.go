package servers

import (
	"testing"
)

func TestParseRawMetrics(t *testing.T) {
	sampleOutput := `===SECTION:LOADAVG===
0.75 1.20 0.95 2/350 12345
===SECTION:MEMINFO===
MemTotal:        16384000 kB
MemFree:          2048000 kB
MemAvailable:     8192000 kB
Buffers:           102400 kB
Cached:           6041600 kB
SwapTotal:        4096000 kB
SwapFree:         2048000 kB
===SECTION:UPTIME===
86400.50 172800.00
===SECTION:DF===
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/root        104857600  52428800  52428800      50% /
===SECTION:STAT===
cpu  20000 500 10000 70000 500 0 100 0 0 0
===SECTION:NPROC===
4
===SECTION:HOSTNAME===
production-node-1
===SECTION:UNAME===
Linux 6.6.0-x86_64
===SECTION:OS===
Ubuntu 24.04.1 LTS`

	metrics := parseRawMetrics(sampleOutput)

	if metrics.System.Hostname != "production-node-1" {
		t.Errorf("expected hostname production-node-1, got %s", metrics.System.Hostname)
	}
	if metrics.System.Kernel != "Linux 6.6.0-x86_64" {
		t.Errorf("expected kernel Linux 6.6.0-x86_64, got %s", metrics.System.Kernel)
	}
	if metrics.System.OS != "Ubuntu 24.04.1 LTS" {
		t.Errorf("expected OS Ubuntu 24.04.1 LTS, got %s", metrics.System.OS)
	}
	if metrics.System.UptimeSeconds != 86400 {
		t.Errorf("expected uptime 86400, got %d", metrics.System.UptimeSeconds)
	}

	// CPU
	if metrics.CPU.Cores != 4 {
		t.Errorf("expected 4 cores, got %d", metrics.CPU.Cores)
	}
	if metrics.CPU.Load1 != 0.75 || metrics.CPU.Load5 != 1.20 || metrics.CPU.Load15 != 0.95 {
		t.Errorf("unexpected load averages: %+v", metrics.CPU)
	}
	// CPU usage: total = 20000+500+10000+70000+500+100 = 101100. idleTotal = 70500. active = 30600. ~30.3%
	if metrics.CPU.UsagePercent < 30.0 || metrics.CPU.UsagePercent > 31.0 {
		t.Errorf("expected CPU usage around 30.3%%, got %f", metrics.CPU.UsagePercent)
	}

	// Memory
	expectedTotalMem := uint64(16384000 * 1024)
	if metrics.Memory.TotalBytes != expectedTotalMem {
		t.Errorf("expected total memory %d, got %d", expectedTotalMem, metrics.Memory.TotalBytes)
	}
	expectedAvailMem := uint64(8192000 * 1024)
	if metrics.Memory.AvailableBytes != expectedAvailMem {
		t.Errorf("expected available memory %d, got %d", expectedAvailMem, metrics.Memory.AvailableBytes)
	}
	if metrics.Memory.UsedPercent != 50.0 {
		t.Errorf("expected 50.0%% memory used, got %f", metrics.Memory.UsedPercent)
	}
	if metrics.Memory.SwapUsedPercent != 50.0 {
		t.Errorf("expected 50.0%% swap used, got %f", metrics.Memory.SwapUsedPercent)
	}

	// Disk
	expectedTotalDisk := uint64(104857600 * 1024)
	if metrics.Disk.TotalBytes != expectedTotalDisk {
		t.Errorf("expected total disk %d, got %d", expectedTotalDisk, metrics.Disk.TotalBytes)
	}
	if metrics.Disk.UsedPercent != 50.0 {
		t.Errorf("expected 50.0%% disk used, got %f", metrics.Disk.UsedPercent)
	}
	if metrics.Disk.Mount != "/" {
		t.Errorf("expected mount '/', got %s", metrics.Disk.Mount)
	}

	// Health check
	metrics.Docker.Status = "running"
	evaluateHealth(metrics)
	if metrics.Health != HealthHealthy {
		t.Errorf("expected HealthHealthy, got %s (issues: %+v)", metrics.Health, metrics.HealthIssues)
	}
}

func TestEvaluateHealthConditions(t *testing.T) {
	t.Run("critical on high disk", func(t *testing.T) {
		m := &ServerMetrics{
			CPU:    CPUMetrics{Cores: 2},
			Memory: MemoryMetrics{UsedPercent: 40},
			Disk:   DiskMetrics{UsedPercent: 94.5, Mount: "/"},
			Docker: DockerMetrics{Status: "running"},
		}
		evaluateHealth(m)
		if m.Health != HealthCritical {
			t.Errorf("expected critical health, got %s", m.Health)
		}
		if len(m.HealthIssues) == 0 {
			t.Errorf("expected health issues reported")
		}
	})

	t.Run("warning on high memory", func(t *testing.T) {
		m := &ServerMetrics{
			CPU:    CPUMetrics{Cores: 4},
			Memory: MemoryMetrics{UsedPercent: 84.0},
			Disk:   DiskMetrics{UsedPercent: 40.0, Mount: "/"},
			Docker: DockerMetrics{Status: "running"},
		}
		evaluateHealth(m)
		if m.Health != HealthWarning {
			t.Errorf("expected warning health, got %s", m.Health)
		}
	})

	t.Run("critical on docker down", func(t *testing.T) {
		m := &ServerMetrics{
			CPU:    CPUMetrics{Cores: 4},
			Memory: MemoryMetrics{UsedPercent: 50.0},
			Disk:   DiskMetrics{UsedPercent: 50.0, Mount: "/"},
			Docker: DockerMetrics{Status: "unavailable"},
		}
		evaluateHealth(m)
		if m.Health != HealthCritical {
			t.Errorf("expected critical health when docker unavailable, got %s", m.Health)
		}
	})
}
