// Package deploy turns a queued run into a running application.
//
// The orchestration is deliberately shell-shaped on the remote side: compose
// has no usable Go library, and `docker build` on the target is exactly what a
// developer would type. What this package adds is the parts that are easy to
// get wrong by hand -- credentials that never touch a command line, secrets
// scrubbed from logs, ports allocated so two applications cannot collide, and
// a record of every line of output.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// Config bounds where deployments live and which ports they may use.
type Config struct {
	// PortMin and PortMax bound the loopback ports handed to deployments.
	PortMin int
	PortMax int
	// RemoteRoot is the directory on each server that holds checkouts.
	RemoteRoot string
}

func (c Config) withDefaults() Config {
	if c.PortMin == 0 {
		c.PortMin = 20000
	}
	if c.PortMax == 0 {
		c.PortMax = 29999
	}
	if c.RemoteRoot == "" {
		c.RemoteRoot = ".dockdeploy/apps"
	}
	return c
}

type Engine struct {
	store   *store.Store
	sealer  store.Sealer
	servers *servers.Manager
	hub     *Hub
	log     *slog.Logger
	config  Config
}

func NewEngine(db *store.Store, sealer store.Sealer, manager *servers.Manager, hub *Hub, log *slog.Logger, config Config) *Engine {
	return &Engine{
		store:   db,
		sealer:  sealer,
		servers: manager,
		hub:     hub,
		log:     log,
		config:  config.withDefaults(),
	}
}

func (e *Engine) Hub() *Hub { return e.hub }

// Execute carries out one run from start to finish.
//
// It always records an outcome, including for a panic or a cancelled context,
// because a run left in "running" makes the deployment look permanently stuck
// with nothing working on it.
func (e *Engine) Execute(ctx context.Context, run *store.Run) {
	deployment, err := e.store.DeploymentByID(ctx, run.DeploymentID)
	if err != nil {
		e.finish(ctx, run, nil, "", "", fmt.Errorf("load deployment: %w", err))
		return
	}

	// Everything sensitive this run will touch, so log output can be scrubbed
	// before a single line is persisted.
	redactions, err := e.redactions(ctx, deployment)
	if err != nil {
		e.finish(ctx, run, deployment, "", "", err)
		return
	}

	recorder := NewRecorder(ctx, e.store, e.hub, run.ID, redactions)
	defer recorder.Close()

	defer func() {
		if recovered := recover(); recovered != nil {
			e.log.Error("deploy panicked", "run", run.ID, "panic", recovered)
			recorder.Fail("The deploy failed unexpectedly.")
			e.finish(context.WithoutCancel(ctx), run, deployment, "", "", errors.New("internal error"))
		}
	}()

	if err := e.store.UpdateDeploymentStatus(ctx, deployment.ID, store.DeploymentDeploying, &run.ID); err != nil {
		e.log.Warn("mark deploying", "deployment", deployment.ID, "error", err)
	}

	recorder.Info("Deploy #%d of %s (%s, %s build)",
		run.Number, deployment.Name, deployment.SourceType, deployment.BuildStrategy)

	result, err := e.run(ctx, recorder, deployment, run)
	if err != nil {
		recorder.Fail("%s", err.Error())
		recorder.Info("Deploy failed after %s.", run.Duration().Round(time.Second))
		e.finish(context.WithoutCancel(ctx), run, deployment, "", "", err)
		return
	}

	recorder.Info("Deploy finished in %s.", run.Duration().Round(time.Second))
	e.finish(context.WithoutCancel(ctx), run, deployment, result.ImageRef, result.CommitSHA, nil)
}

type result struct {
	ImageRef  string
	CommitSHA string
}

func (e *Engine) finish(ctx context.Context, run *store.Run, deployment *store.Deployment, imageRef, commit string, failure error) {
	status := store.RunSucceeded
	message := ""
	if failure != nil {
		status = store.RunFailed
		message = failure.Error()
	}

	if err := e.store.FinishRun(ctx, run.ID, status, imageRef, commit, message); err != nil {
		e.log.Error("record run outcome", "run", run.ID, "error", err)
	}
	if deployment != nil && failure != nil {
		e.log.Info("deploy failed", "deployment", deployment.Name, "run", run.Number, "error", failure)
	}
}

// redactions gathers every secret that could appear in build output.
func (e *Engine) redactions(ctx context.Context, deployment *store.Deployment) ([]string, error) {
	values, err := e.store.SecretEnvValues(ctx, e.sealer, deployment.ID)
	if err != nil {
		return nil, fmt.Errorf("read deployment secrets: %w", err)
	}

	if deployment.GitCredentialID != nil {
		credential, err := e.store.ResolveGitCredential(ctx, e.sealer, deployment.GitCredentialID)
		if err != nil {
			return nil, fmt.Errorf("read git credential: %w", err)
		}
		values = append(values, gitx.RedactionsFor(credential)...)
	}

	if deployment.RegistryID != nil {
		registry, err := e.store.RegistryByID(ctx, *deployment.RegistryID)
		if err == nil {
			if password, err := e.store.RegistryPassword(ctx, e.sealer, registry); err == nil && password != "" {
				values = append(values, password)
			}
		}
	}
	return values, nil
}

// run dispatches on strategy and carries out the deploy.
func (e *Engine) run(ctx context.Context, rec *Recorder, deployment *store.Deployment, dbRun *store.Run) (result, error) {
	server, err := e.store.ServerByID(ctx, deployment.ServerID)
	if err != nil {
		return result{}, fmt.Errorf("load server: %w", err)
	}

	rec.Info("Connecting to %s (%s).", server.Name, server.Host)
	session, err := e.servers.Session(ctx, server)
	if err != nil {
		return result{}, fmt.Errorf("connect to %s: %w", server.Name, err)
	}
	defer session.Close()

	caps := serverCapabilities(server)
	remote := &remoteHost{
		conn:     session.Conn,
		rec:      rec,
		workdir:  e.workdir(deployment),
		credsDir: e.credsDir(deployment),
		compose:  caps.ComposeCommand,
	}

	if err := remote.ensureWorkdir(ctx); err != nil {
		return result{}, err
	}

	var commit string
	if deployment.SourceType.NeedsGit() && deployment.BuildStrategy == store.BuildRemote {
		credential, err := e.store.ResolveGitCredential(ctx, e.sealer, deployment.GitCredentialID)
		if err != nil {
			return result{}, fmt.Errorf("read git credential: %w", err)
		}
		commit, err = remote.fetch(ctx, gitx.Source{
			RepoURL:    deployment.RepoURL,
			Ref:        deployment.GitRef,
			Credential: credential,
		})
		if err != nil {
			return result{}, err
		}
		rec.Info("Checked out %s at %s.", deployment.GitRef, shortSHA(commit))
	}

	// The .env is written for every source type: compose reads it implicitly,
	// and `docker run --env-file` uses the same file.
	env, err := e.store.ResolveDeploymentEnv(ctx, e.sealer, deployment.ID)
	if err != nil {
		return result{}, fmt.Errorf("read environment: %w", err)
	}
	if err := remote.writeEnvFile(ctx, env); err != nil {
		return result{}, err
	}
	if len(env) > 0 {
		rec.Info("Wrote %d environment variable(s) to .env (0600).", len(env))
	}

	if deployment.SourceType.UsesCompose() {
		return e.deployCompose(ctx, rec, remote, deployment)
	}

	// Ask the server what is actually bound before allocating a port. The
	// database alone is not enough: a deleted deployment leaves its container
	// running, and containers this platform never created may also sit in the
	// range.
	var occupied []int
	if containers, err := session.Docker.ListContainers(ctx); err == nil {
		for _, container := range containers {
			// This deployment's own container is removed before the new one
			// starts, so the port it holds is about to become free.
			if container.State != "running" || container.Name == deployment.Slug {
				continue
			}
			for _, port := range container.Ports {
				if port.HostPort != 0 {
					occupied = append(occupied, int(port.HostPort))
				}
			}
		}
	} else {
		rec.Info("Note: could not list containers to check port usage; relying on recorded ports only.")
	}

	return e.deployContainer(ctx, rec, remote, deployment, dbRun, commit, occupied)
}

// deployCompose writes the compose file and hands off to the compose CLI.
func (e *Engine) deployCompose(ctx context.Context, rec *Recorder, remote *remoteHost, deployment *store.Deployment) (result, error) {
	if remote.compose == "" {
		return result{}, errors.New("this server has no Docker Compose available, so a compose deployment cannot run here")
	}

	composePath := deployment.ComposePath
	if deployment.SourceType == store.SourceRawCompose {
		composePath = "docker-compose.yml"
		if err := remote.writeFile(ctx, composePath, []byte(deployment.ComposeContent), 0o644); err != nil {
			return result{}, err
		}
		rec.Info("Wrote compose file.")
	}

	// --build makes compose rebuild images defined with `build:`; without it a
	// code change would redeploy the previous image and look like a no-op.
	command := fmt.Sprintf("%s -f %s -p %s up -d --build --remove-orphans",
		remote.compose, shellQuote(composePath), shellQuote(deployment.Slug))

	rec.Info("Running compose up.")
	if err := remote.exec(ctx, command); err != nil {
		return result{}, fmt.Errorf("compose up failed: %w", err)
	}
	return result{}, nil
}

// deployContainer builds (or pulls) a single image and replaces the container.
func (e *Engine) deployContainer(ctx context.Context, rec *Recorder, remote *remoteHost, deployment *store.Deployment, dbRun *store.Run, commit string, occupiedPorts []int) (result, error) {
	imageRef := deployment.ImageRef

	switch {
	case dbRun.Trigger == store.TriggerRollback && dbRun.ImageRef != "":
		imageRef = dbRun.ImageRef
		rec.Info("Rolling back to image %s.", imageRef)

	case deployment.SourceType == store.SourceImage:
		rec.Info("Pulling %s.", imageRef)
		if err := remote.exec(ctx, "docker pull "+shellQuote(imageRef)); err != nil {
			return result{}, fmt.Errorf("pull failed: %w", err)
		}

	case deployment.BuildStrategy == store.BuildRemote:
		// Tag with the run number so a rollback has a distinct image to
		// return to rather than a moving :latest.
		imageRef = fmt.Sprintf("dockdeploy/%s:%d", deployment.Slug, dbRun.Number)
		rec.Info("Building %s on the server.", imageRef)

		command := fmt.Sprintf("docker build -t %s -f %s %s",
			shellQuote(imageRef),
			shellQuote(deployment.DockerfilePath),
			shellQuote(deployment.BuildContext))
		if err := remote.exec(ctx, command); err != nil {
			return result{}, fmt.Errorf("build failed: %w", err)
		}

	case deployment.BuildStrategy == store.BuildRegistry:
		builtRef, sha, err := e.buildAndPushRegistry(ctx, rec, remote, deployment, dbRun)
		if err != nil {
			return result{}, err
		}
		imageRef = builtRef
		if sha != "" {
			commit = sha
		}

	default:
		return result{}, fmt.Errorf("unknown build strategy %q", deployment.BuildStrategy)
	}

	// A previously recorded port is re-checked rather than trusted. It can be
	// stale in ways the database cannot see: an earlier run may have recorded
	// a port and then failed to bind it, or something else on the machine may
	// have taken it since. occupiedPorts excludes this deployment's own
	// container, which is removed just below.
	port := deployment.HostPort
	if port != nil && slices.Contains(occupiedPorts, *port) {
		rec.Info("Port %d is taken by something else on this server; choosing another.", *port)
		port = nil
	}

	if port == nil {
		allocated, err := e.store.AllocateHostPort(ctx, deployment.ServerID, e.config.PortMin, e.config.PortMax, occupiedPorts)
		if err != nil {
			return result{}, err
		}
		if err := e.store.SetDeploymentPort(ctx, deployment.ID, allocated); err != nil {
			return result{}, fmt.Errorf("record allocated port: %w", err)
		}
		port = &allocated
		rec.Info("Allocated host port %d.", allocated)
	}

	// Replace rather than restart: the new container runs a different image,
	// and leaving the old one would hold the port.
	rec.Info("Replacing container %s.", deployment.Slug)
	_ = remote.exec(ctx, "docker rm -f "+shellQuote(deployment.Slug)+" 2>/dev/null || true")

	// Published on loopback only. Nothing should be reachable from the
	// internet until nginx is put in front of it deliberately.
	command := fmt.Sprintf(
		"docker run -d --name %s --restart unless-stopped "+
			"--label %s=dockdeploy --label %s=%s "+
			"--env-file %s -p 127.0.0.1:%d:%d %s",
		shellQuote(deployment.Slug),
		labelManagedBy, labelDeploymentID, shellQuote(deployment.ID),
		shellQuote(envFileName), *port, deployment.ContainerPort,
		shellQuote(imageRef))

	if err := remote.exec(ctx, command); err != nil {
		return result{}, fmt.Errorf("could not start the container: %w", err)
	}

	rec.Info("Running on 127.0.0.1:%d.", *port)
	return result{ImageRef: imageRef, CommitSHA: commit}, nil
}

func (e *Engine) workdir(deployment *store.Deployment) string {
	if deployment.Workdir != "" {
		return deployment.Workdir
	}
	return e.config.RemoteRoot + "/" + deployment.Slug
}

// credsDir is a sibling of the checkout, never a child of it, so credential
// material can never end up in a Docker build context.
func (e *Engine) credsDir(deployment *store.Deployment) string {
	root := e.config.RemoteRoot
	if index := strings.LastIndex(root, "/"); index > 0 {
		root = root[:index]
	}
	return root + "/credentials/" + deployment.Slug
}

// Labels written on containers the platform creates, so they can be told apart
// from whatever else is running on the machine.
const (
	labelManagedBy    = "dev.dockdeploy.managed"
	labelDeploymentID = "dev.dockdeploy.deployment"
	envFileName       = ".env"
)

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// serverCapabilities reads the cached probe result off a server row.
func serverCapabilities(server *store.Server) sshx.Capabilities {
	var caps sshx.Capabilities
	if len(server.Capabilities) > 0 {
		_ = unmarshalCapabilities(server.Capabilities, &caps)
	}
	return caps
}

// shellQuote wraps a value in single quotes for a POSIX shell.
//
// Repository URLs and refs are validated separately by gitx, but slugs, paths
// and image names all reach a command line too. Quoting everything means a
// value can never be read as a second command, whatever it contains.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// sortedKeys gives .env files a stable order, so an unchanged environment
// produces an identical file and does not look like a change.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (e *Engine) buildAndPushRegistry(ctx context.Context, rec *Recorder, remote *remoteHost, deployment *store.Deployment, dbRun *store.Run) (string, string, error) {
	if deployment.RegistryID == nil {
		return "", "", errors.New("deployment has no container registry configured")
	}

	registry, err := e.store.RegistryByID(ctx, *deployment.RegistryID)
	if err != nil {
		return "", "", fmt.Errorf("load registry: %w", err)
	}

	registryPass, err := e.store.RegistryPassword(ctx, e.sealer, registry)
	if err != nil {
		return "", "", fmt.Errorf("read registry password: %w", err)
	}

	// Determine registry host for tagging
	regHost := registry.URL
	regHost = strings.TrimPrefix(regHost, "https://")
	regHost = strings.TrimPrefix(regHost, "http://")
	regHost = strings.TrimRight(regHost, "/")

	imageRef := fmt.Sprintf("%s/%s:%d", regHost, deployment.Slug, dbRun.Number)
	if deployment.ImageName != "" {
		imageRef = fmt.Sprintf("%s:%d", deployment.ImageName, dbRun.Number)
	}

	tempDir, err := os.MkdirTemp("", "dockdeploy-build-"+deployment.Slug+"-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp build directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	rec.Info("Cloning repository on controller.")
	credential, err := e.store.ResolveGitCredential(ctx, e.sealer, deployment.GitCredentialID)
	if err != nil {
		return "", "", fmt.Errorf("read git credential: %w", err)
	}

	commit, err := e.localGitClone(ctx, rec, gitx.Source{
		RepoURL:    deployment.RepoURL,
		Ref:        deployment.GitRef,
		Credential: credential,
	}, tempDir)
	if err != nil {
		return "", "", fmt.Errorf("git checkout on controller: %w", err)
	}
	rec.Info("Checked out %s at %s.", deployment.GitRef, shortSHA(commit))

	rec.Info("Building %s on controller.", imageRef)
	dockerfile := deployment.DockerfilePath
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	buildContext := deployment.BuildContext
	if buildContext == "" {
		buildContext = "."
	}

	buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", imageRef, "-f", dockerfile, buildContext)
	buildCmd.Dir = tempDir
	buildCmd.Stdout = rec.Stream("stdout")
	buildCmd.Stderr = rec.Stream("stderr")
	if err := buildCmd.Run(); err != nil {
		return "", "", fmt.Errorf("local docker build failed: %w", err)
	}

	rec.Info("Pushing %s to registry %s.", imageRef, registry.Name)
	loginCmd := exec.CommandContext(ctx, "docker", "login", registry.URL, "-u", registry.Username, "--password-stdin")
	loginCmd.Stdin = strings.NewReader(registryPass)
	loginCmd.Stdout = rec.Stream("stdout")
	loginCmd.Stderr = rec.Stream("stderr")
	if err := loginCmd.Run(); err != nil {
		return "", "", fmt.Errorf("controller docker login failed: %w", err)
	}

	pushCmd := exec.CommandContext(ctx, "docker", "push", imageRef)
	pushCmd.Stdout = rec.Stream("stdout")
	pushCmd.Stderr = rec.Stream("stderr")
	if err := pushCmd.Run(); err != nil {
		return "", "", fmt.Errorf("docker push to %s failed: %w", registry.URL, err)
	}

	// Pull on target server
	rec.Info("Logging in and pulling %s on target server.", imageRef)
	remoteLogin := fmt.Sprintf("echo %s | docker login %s -u %s --password-stdin",
		shellQuote(registryPass), shellQuote(registry.URL), shellQuote(registry.Username))
	if err := remote.exec(ctx, remoteLogin); err != nil {
		return "", "", fmt.Errorf("target server docker login failed: %w", err)
	}

	if err := remote.exec(ctx, "docker pull "+shellQuote(imageRef)); err != nil {
		return "", "", fmt.Errorf("target server docker pull failed: %w", err)
	}

	_ = remote.exec(ctx, "docker logout "+shellQuote(registry.URL)+" 2>/dev/null || true")

	return imageRef, commit, nil
}

func (e *Engine) localGitClone(ctx context.Context, rec *Recorder, source gitx.Source, targetDir string) (string, error) {
	if err := gitx.ValidateRepoURL(source.RepoURL); err != nil {
		return "", err
	}
	if err := gitx.ValidateRef(source.Ref); err != nil {
		return "", err
	}

	cloneURL := source.RepoURL
	var env []string

	if source.Credential != nil {
		switch source.Credential.Kind {
		case gitx.KindToken:
			authURL, err := gitx.AuthenticatedURL(source.RepoURL, source.Credential)
			if err != nil {
				return "", err
			}
			cloneURL = authURL
		case gitx.KindSSHKey:
			keyFile, err := os.CreateTemp("", "dockdeploy-git-key-*")
			if err != nil {
				return "", err
			}
			defer os.Remove(keyFile.Name())
			key := strings.TrimRight(source.Credential.Secret, "\n") + "\n"
			if _, err := keyFile.WriteString(key); err != nil {
				_ = keyFile.Close()
				return "", err
			}
			_ = keyFile.Chmod(0o600)
			_ = keyFile.Close()

			env = append(env, fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o StrictHostKeyChecking=accept-new -o IdentitiesOnly=yes", keyFile.Name()))
		}
	}

	env = append(env, "GIT_TERMINAL_PROMPT=0", "PATH="+os.Getenv("PATH"))

	cloneCmd := exec.CommandContext(ctx, "git", "clone", cloneURL, targetDir)
	cloneCmd.Env = env
	cloneCmd.Stdout = rec.Stream("stdout")
	cloneCmd.Stderr = rec.Stream("stderr")
	if err := cloneCmd.Run(); err != nil {
		return "", fmt.Errorf("git clone failed: %w", err)
	}

	ref := source.Reference()
	if ref != "HEAD" {
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", ref)
		checkoutCmd.Dir = targetDir
		checkoutCmd.Env = env
		checkoutCmd.Stdout = rec.Stream("stdout")
		checkoutCmd.Stderr = rec.Stream("stderr")
		if err := checkoutCmd.Run(); err != nil {
			return "", fmt.Errorf("git checkout %s failed: %w", ref, err)
		}
	}

	revCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	revCmd.Dir = targetDir
	out, err := revCmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

