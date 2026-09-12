// Package gitx fetches source for a deployment.
//
// It is deliberately an interface with one credential-based driver rather than
// a direct dependency on any provider. A GitHub App can be added later by
// implementing Provider, without touching the build pipeline.
package gitx

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Kind is how a repository is authenticated.
type Kind string

const (
	// KindToken is an HTTPS personal access token or app password. Works
	// identically on GitHub, GitLab, Bitbucket and self-hosted forges.
	KindToken Kind = "token"
	// KindSSHKey is a deploy key.
	KindSSHKey Kind = "ssh_key"
)

func (k Kind) Valid() bool { return k == KindToken || k == KindSSHKey }

// Credential is decrypted material for one clone.
type Credential struct {
	Kind Kind
	// Username is required by some forges for token auth. GitHub accepts
	// anything; GitLab wants "oauth2"; Bitbucket wants the account name.
	Username string
	Secret   string
}

// Source describes what to fetch.
type Source struct {
	RepoURL string
	Ref     string
	// Credential is nil for a public repository.
	Credential *Credential
}

// Ref returns the ref to check out, defaulting to the remote HEAD.
func (s Source) Reference() string {
	if strings.TrimSpace(s.Ref) == "" {
		return "HEAD"
	}
	return strings.TrimSpace(s.Ref)
}

// IsSSH reports whether the URL uses the SSH transport.
func (s Source) IsSSH() bool {
	return strings.HasPrefix(s.RepoURL, "git@") || strings.HasPrefix(s.RepoURL, "ssh://")
}

var (
	// Recognises the shapes a user is likely to paste.
	httpsRepo = regexp.MustCompile(`^https?://[^\s/@]+(:[0-9]+)?/\S+$`)
	scpRepo   = regexp.MustCompile(`^[\w.-]+@[\w.-]+:[\w./~-]+$`)
	sshRepo   = regexp.MustCompile(`^ssh://\S+$`)
)

// ValidateRepoURL rejects anything that is not a repository URL.
//
// This value is interpolated into a shell command on the target server, so a
// permissive check here would be a command injection. The allowed shapes
// contain no shell metacharacters by construction.
func ValidateRepoURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("a repository URL is required")
	}
	if len(trimmed) > 512 {
		return fmt.Errorf("that repository URL is too long")
	}
	if strings.ContainsAny(trimmed, " \t\n\r\"'`$\\;|&<>()") {
		return fmt.Errorf("that does not look like a repository URL")
	}

	switch {
	case httpsRepo.MatchString(trimmed), scpRepo.MatchString(trimmed), sshRepo.MatchString(trimmed):
		return nil
	default:
		return fmt.Errorf("use an https:// or git@host:path repository URL")
	}
}

// ValidateRef rejects refs that could break out of a git command.
func ValidateRef(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil // an empty ref means the remote default branch
	}
	if len(trimmed) > 255 {
		return fmt.Errorf("that ref is too long")
	}
	// git's own rules, tightened: no shell metacharacters, no leading dash
	// that could be read as a flag.
	if strings.ContainsAny(trimmed, " \t\n\r\"'`$\\;|&<>()~^:?*[") || strings.HasPrefix(trimmed, "-") {
		return fmt.Errorf("that is not a valid branch, tag or commit")
	}
	return nil
}

// AuthenticatedURL embeds a token in an HTTPS URL.
//
// The result is a secret: it must never be logged, echoed into a shell command
// where it would appear in the process list, or stored. It is written to a
// 0600 credentials file instead, and RedactionsFor covers it in log output.
func AuthenticatedURL(repoURL string, cred *Credential) (string, error) {
	if cred == nil || cred.Kind != KindToken {
		return repoURL, nil
	}

	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("gitx: parse repository URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return repoURL, nil
	}

	username := cred.Username
	if username == "" {
		// GitHub ignores the username for token auth; other forges require
		// something non-empty, so a placeholder is the safest default.
		username = "git"
	}
	parsed.User = url.UserPassword(username, cred.Secret)
	return parsed.String(), nil
}

// RedactionsFor lists the strings that must be scrubbed from build output for
// this credential.
func RedactionsFor(cred *Credential) []string {
	if cred == nil || cred.Secret == "" {
		return nil
	}
	// The raw secret, and the URL-encoded form it takes inside a credentials
	// file, are both worth covering.
	return []string{cred.Secret, url.QueryEscape(cred.Secret)}
}

// Provider fetches source into a working directory.
//
// Implementations exist for the two places a checkout can happen: on the
// target server over SSH, and on the controller for the registry build
// strategy.
type Provider interface {
	// Fetch clones or updates the repository and returns the resolved commit.
	Fetch(source Source, workdir string) (commit string, err error)
}
