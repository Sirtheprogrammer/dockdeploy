package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
)

// gitAuth is everything needed to run git against one repository.
//
// The environment prefix and the git arguments are kept apart rather than
// pasted into one string, because assembling them by hand is easy to get
// subtly wrong -- an env var landing after the subcommand, or a second "git"
// creeping in. cmd is the only way to build a command from them.
type gitAuth struct {
	// env is a run of VAR=value assignments that precede the binary.
	env []string
	// args are git's own global flags, before the subcommand.
	args []string
	// cleanup removes any credential material staged on the server.
	cleanup func()
}

// cmd renders a full shell command: `<env> git <global args> <subcommand>`.
func (a gitAuth) cmd(subcommand string) string {
	var builder strings.Builder
	for _, assignment := range a.env {
		builder.WriteString(assignment)
		builder.WriteByte(' ')
	}
	builder.WriteString("git ")
	for _, arg := range a.args {
		builder.WriteString(arg)
		builder.WriteByte(' ')
	}
	builder.WriteString(subcommand)
	return builder.String()
}

// Names for credential material staged alongside, but never inside, the
// checkout. Both are written 0600 and removed as soon as the fetch ends.
const (
	gitCredentialFile = "git-credentials"
	gitKeyFile        = "git-key"
)

// prepareGitAuth stages credentials for a fetch.
//
// A token never reaches a command line. An https URL with the token embedded
// would show up in `ps` output for every user on the machine and in shell
// history, so it goes into a 0600 credentials file that git reads through a
// credential helper, and the file is deleted afterwards.
func (r *remoteHost) prepareGitAuth(ctx context.Context, source gitx.Source) (gitAuth, error) {
	// Never prompt. Without a terminal, git would otherwise hang forever on a
	// private repository rather than failing with a useful message.
	auth := gitAuth{
		env:     []string{"GIT_TERMINAL_PROMPT=0"},
		cleanup: func() {},
	}

	if source.Credential == nil {
		return auth, nil
	}

	switch source.Credential.Kind {
	case gitx.KindToken:
		authenticated, err := gitx.AuthenticatedURL(source.RepoURL, source.Credential)
		if err != nil {
			return auth, err
		}
		// A non-https remote cannot use a token; fall through unauthenticated
		// rather than writing a useless file.
		if authenticated == source.RepoURL {
			return auth, nil
		}

		path, err := r.writeCredential(ctx, gitCredentialFile, []byte(authenticated+"\n"))
		if err != nil {
			return auth, err
		}

		auth.args = append(auth.args,
			"-c "+shellQuote("credential.helper=store --file="+path))
		auth.cleanup = func() {
			r.removeQuietly(ctx, path, "git credentials file")
		}
		return auth, nil

	case gitx.KindSSHKey:
		// Keys pasted through a browser routinely lose their trailing newline,
		// and ssh rejects a key without one.
		key := strings.TrimRight(source.Credential.Secret, "\n") + "\n"
		path, err := r.writeCredential(ctx, gitKeyFile, []byte(key))
		if err != nil {
			return auth, err
		}

		// accept-new trusts the forge on first contact. The alternative is a
		// deploy that blocks on an interactive host-key prompt nobody can see.
		auth.env = append(auth.env, "GIT_SSH_COMMAND="+shellQuote(fmt.Sprintf(
			"ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new", path)))
		auth.cleanup = func() {
			r.removeQuietly(ctx, path, "git deploy key")
		}
		return auth, nil

	default:
		return auth, fmt.Errorf("unsupported git credential type %q", source.Credential.Kind)
	}
}

// removeQuietly deletes staged credential material. The deploy has already
// finished by this point, so a failure is worth a note rather than an error.
func (r *remoteHost) removeQuietly(ctx context.Context, path, description string) {
	result, err := r.conn.Run(context.WithoutCancel(ctx), "rm -f "+shellQuote(path))
	if err != nil || !result.Ok() {
		r.rec.Info("Note: the temporary %s at %s could not be removed.", description, path)
	}
}

// fetch clones or updates the repository and returns the checked-out commit.
func (r *remoteHost) fetch(ctx context.Context, source gitx.Source) (string, error) {
	if err := gitx.ValidateRepoURL(source.RepoURL); err != nil {
		return "", err
	}
	if err := gitx.ValidateRef(source.Ref); err != nil {
		return "", err
	}

	auth, err := r.prepareGitAuth(ctx, source)
	if err != nil {
		return "", err
	}
	defer auth.cleanup()

	// Update in place rather than deleting and re-cloning: an incremental
	// checkout keeps the Docker layer cache useful on the next build.
	existing, err := r.execQuiet(ctx, "test -d .git && echo yes")
	if err != nil {
		return "", fmt.Errorf("could not inspect the working directory: %w", err)
	}

	if existing.Output() == "yes" {
		r.rec.Info("Fetching %s.", source.RepoURL)
		update := auth.cmd("remote set-url origin "+shellQuote(source.RepoURL)) +
			" && " + auth.cmd("fetch --prune --tags origin")
		if err := r.exec(ctx, update); err != nil {
			return "", err
		}
	} else {
		r.rec.Info("Cloning %s.", source.RepoURL)
		if err := r.exec(ctx, auth.cmd("clone "+shellQuote(source.RepoURL)+" .")); err != nil {
			return "", err
		}
	}

	if err := r.checkout(ctx, auth, source.Reference()); err != nil {
		return "", err
	}

	commit, err := r.execQuiet(ctx, "git rev-parse HEAD")
	if err != nil || !commit.Ok() {
		// The checkout succeeded; not knowing the sha is cosmetic.
		return "", nil
	}
	return commit.Output(), nil
}

// checkout moves the working tree to a ref.
//
// A detached hard checkout, not a merge: the tree is disposable, and a merge
// conflict on a deploy server has nobody to resolve it.
func (r *remoteHost) checkout(ctx context.Context, auth gitAuth, ref string) error {
	target := "origin/HEAD"
	if ref != "HEAD" {
		// A ref may be a branch, a tag or a commit. origin/<ref> is tried
		// first so a branch tracks the remote rather than a stale local copy.
		target = fmt.Sprintf("$(git rev-parse --verify --quiet %s || echo %s)",
			shellQuote("origin/"+ref), shellQuote(ref))
	}

	command := fmt.Sprintf("git checkout --force --detach %s && git reset --hard", target)
	if err := r.exec(ctx, command); err != nil {
		return fmt.Errorf("could not check out %q: %w", ref, err)
	}

	// Submodules need the same credentials as the parent repository.
	if err := r.exec(ctx, auth.cmd("submodule update --init --recursive")); err != nil {
		return fmt.Errorf("could not update submodules: %w", err)
	}
	return nil
}
