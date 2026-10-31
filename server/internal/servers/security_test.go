package servers

import (
	"testing"
)

func TestParseSSHLogs(t *testing.T) {
	rawLogs := `
Oct 31 09:12:01 host sshd[12345]: Failed password for invalid user admin from 192.168.1.100 port 44321 ssh2
Oct 31 09:12:03 host sshd[12346]: Failed password for root from 192.168.1.100 port 44322 ssh2
Oct 31 09:12:05 host sshd[12347]: Failed password for root from 203.0.113.50 port 55123 ssh2
Oct 31 09:12:07 host sshd[12348]: Invalid user test from 198.51.100.22 port 33112 ssh2
`
	var ssh SSHAttackAnalysis
	parseSSHLogs(rawLogs, &ssh)

	if ssh.FailedAttemptsCount != 3 {
		t.Errorf("expected 3 failed attempts, got %d", ssh.FailedAttemptsCount)
	}
	if ssh.RootLoginAttempts != 2 {
		t.Errorf("expected 2 root login attempts, got %d", ssh.RootLoginAttempts)
	}
	if ssh.InvalidUserAttempts != 2 {
		t.Errorf("expected 2 invalid user attempts, got %d", ssh.InvalidUserAttempts)
	}
	if len(ssh.TopAttackerIPs) == 0 {
		t.Fatalf("expected attacker IPs to be parsed")
	}
	if ssh.TopAttackerIPs[0].IP != "192.168.1.100" || ssh.TopAttackerIPs[0].Count != 2 {
		t.Errorf("expected top attacker 192.168.1.100 with count 2, got %+v", ssh.TopAttackerIPs[0])
	}
}

func TestParseExposedPorts(t *testing.T) {
	rawPorts := `State  Recv-Q Send-Q Local Address:Port  Peer Address:PortProcess
LISTEN 0      128          0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=800,fd=3))
LISTEN 0      1024         0.0.0.0:2375       0.0.0.0:*    users:(("dockerd",pid=1200,fd=4))
LISTEN 0      511        127.0.0.1:6379       0.0.0.0:*    users:(("redis-server",pid=1500,fd=6))
LISTEN 0      511          0.0.0.0:3306       0.0.0.0:*    users:(("mariadbd",pid=1800,fd=5))
`
	ports := parseExposedPorts(rawPorts)
	if len(ports) < 4 {
		t.Fatalf("expected at least 4 ports parsed, got %d", len(ports))
	}

	foundDocker := false
	foundMySQL := false
	foundRedis := false

	for _, p := range ports {
		if p.Port == 2375 {
			foundDocker = true
			if !p.IsRisky {
				t.Errorf("expected port 2375 on 0.0.0.0 to be flagged as risky")
			}
		}
		if p.Port == 3306 {
			foundMySQL = true
			if !p.IsRisky {
				t.Errorf("expected port 3306 on 0.0.0.0 to be flagged as risky")
			}
		}
		if p.Port == 6379 {
			foundRedis = true
			if p.IsPublic {
				t.Errorf("expected 127.0.0.1:6379 NOT to be public")
			}
			if p.IsRisky {
				t.Errorf("expected 127.0.0.1:6379 NOT to be risky since bound to localhost")
			}
		}
	}

	if !foundDocker || !foundMySQL || !foundRedis {
		t.Errorf("missing parsed ports: docker=%t, mysql=%t, redis=%t", foundDocker, foundMySQL, foundRedis)
	}
}

func TestParseWebAttacks(t *testing.T) {
	rawLogs := `
192.168.1.50 - - [31/Oct/2026:09:00:01 +0000] "GET /index.php?id=1%20UNION%20SELECT%20null,username,password%20FROM%20users HTTP/1.1" 200 452
192.168.1.51 - - [31/Oct/2026:09:00:02 +0000] "GET /../../etc/passwd HTTP/1.1" 404 153
192.168.1.52 - - [31/Oct/2026:09:00:03 +0000] "POST /wp-login.php HTTP/1.1" 404 153
192.168.1.53 - - [31/Oct/2026:09:00:04 +0000] "GET /.env HTTP/1.1" 404 153
`
	var web WebAttackSummary
	parseWebAttacks(rawLogs, &web)

	if web.SQLiAttempts != 1 {
		t.Errorf("expected 1 SQLi attempt, got %d", web.SQLiAttempts)
	}
	if web.PathTraversalAttempts != 1 {
		t.Errorf("expected 1 Path Traversal attempt, got %d", web.PathTraversalAttempts)
	}
	if web.ScansDetected != 2 {
		t.Errorf("expected 2 scanner probe detections, got %d", web.ScansDetected)
	}
}

func TestCalculateScoreAndFindings(t *testing.T) {
	audit := &ServerSecurityAudit{
		Findings:        []SecurityFinding{},
		ExposedPorts:    []ExposedPort{},
		ContainerRisks:  []ContainerSecurityRisk{},
		SSHAnalysis:     SSHAttackAnalysis{},
		WebAttacks:      WebAttackSummary{},
		SystemIntegrity: SystemIntegritySummary{FirewallStatus: "disabled"},
	}

	audit.calculateScoreAndFindings()
	if audit.Score >= 100 {
		t.Errorf("expected score deduction for disabled firewall, got %d", audit.Score)
	}
	if len(audit.Findings) == 0 {
		t.Fatalf("expected findings for disabled firewall")
	}
}
