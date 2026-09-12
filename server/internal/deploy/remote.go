package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
)

// remoteHost runs the deployment steps on a managed server.
type remoteHost struct {
	conn *sshx.Conn
	rec  *Recorder
	// workdir is relative to the SSH user's home, so it works without knowing
	// the absolute home path and without needing root.
	workdir string
	// credsDir holds credential material during a fetch. It is deliberately
	// NOT inside workdir: that directory is the Docker build context, and a
	// Dockerfile with `COPY . /app` would otherwise bake a deploy key or an
	// access token straight into the published image. Keeping it outside also
	// leaves the checkout empty so `git clone .` works.
	credsDir string
	compose  string
}

// exec runs a command in the working directory and streams its output.
//
// A non-zero exit becomes an error: unlike the capability probe, every command
// here is expected to succeed, and continuing past a failed build would
// deploy the previous image while reporting success.
func (r *remoteHost) exec(ctx context.Context, command string) error {
	full := fmt.Sprintf("cd %s && %s", shellQuote(r.workdir), command)

	stdout := r.rec.Stream("stdout")
	stderr := r.rec.Stream("stderr")

	code, err := r.conn.Stream(ctx, full, stdout, stderr)

	// Commands often end without a trailing newline; flush what is buffered so
	// the last line is not silently dropped.
	if w, ok := stdout.(*streamWriter); ok {
		w.flushPartial()
	}
	if w, ok := stderr.(*streamWriter); ok {
		w.flushPartial()
	}

	if err != nil {
		return fmt.Errorf("could not run the command: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("command exited with status %d", code)
	}
	return nil
}

// execQuiet runs a command and returns its output without logging it. Used for
// anything whose output is not interesting or must not be shown.
func (r *remoteHost) execQuiet(ctx context.Context, command string) (sshx.Result, error) {
	return r.conn.Run(ctx, fmt.Sprintf("cd %s && %s", shellQuote(r.workdir), command))
}

// ensureWorkdir resolves the paths to absolute and creates them.
//
// Making them absolute is not cosmetic. Relative paths mean two different
// things here: SFTP resolves them against the user's home, while a shell
// command resolves them against the working directory it was given. A
// credentials path written by SFTP and then read by ssh would land in two
// different places.
func (r *remoteHost) ensureWorkdir(ctx context.Context) error {
	home, err := r.conn.Run(ctx, `printf %s "$HOME"`)
	if err != nil {
		return fmt.Errorf("could not reach the server: %w", err)
	}
	if root := strings.TrimSpace(home.Stdout); root != "" {
		r.workdir = absolutise(root, r.workdir)
		r.credsDir = absolutise(root, r.credsDir)
	}

	// 0700 on the credentials directory: nothing else on the machine has any
	// business reading what is staged there.
	command := fmt.Sprintf("mkdir -p %s && mkdir -p %s && chmod 700 %s",
		shellQuote(r.workdir), shellQuote(r.credsDir), shellQuote(r.credsDir))

	result, err := r.conn.Run(ctx, command)
	if err != nil {
		return fmt.Errorf("could not prepare the working directory: %w", err)
	}
	if !result.Ok() {
		return fmt.Errorf("could not create %s: %s", r.workdir, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func absolutise(home, path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return strings.TrimSuffix(home, "/") + "/" + path
}

// writeCredential stages secret material outside the build context.
func (r *remoteHost) writeCredential(ctx context.Context, name string, content []byte) (string, error) {
	path := r.credsDir + "/" + name
	if err := r.conn.WriteFile(ctx, path, content, 0o600); err != nil {
		return "", fmt.Errorf("could not stage credentials: %w", err)
	}
	return path, nil
}

func (r *remoteHost) writeFile(ctx context.Context, name string, content []byte, mode uint32) error {
	path := r.workdir + "/" + name
	if err := r.conn.WriteFile(ctx, path, content, mode); err != nil {
		return fmt.Errorf("could not write %s: %w", name, err)
	}
	return nil
}

// writeEnvFile renders the deployment environment.
//
// The format is Docker env-file, not shell: everything after the first "=" is
// the literal value. Quoting a value here would make the quotes part of it --
// `printenv` would return `"hello"` rather than `hello` -- so values are
// written raw. Compose reads the same format for `env_file:`, and uses this
// file for ${...} interpolation in the compose file itself.
//
// Mode 0600 is the point of writing it ourselves: on a shared machine any
// other user could otherwise read every database password the application has.
func (r *remoteHost) writeEnvFile(ctx context.Context, env map[string]string) error {
	var builder strings.Builder
	builder.WriteString("# Written by dockdeploy. Do not edit; changes are overwritten on deploy.\n")
	for _, key := range sortedKeys(env) {
		// A newline would start a new assignment and silently corrupt the rest
		// of the file, so it is rejected when the variable is saved. Guard
		// again here in case a value reached the database another way.
		value := strings.ReplaceAll(env[key], "\n", " ")
		fmt.Fprintf(&builder, "%s=%s\n", key, value)
	}
	return r.writeFile(ctx, envFileName, []byte(builder.String()), 0o600)
}

// unmarshalCapabilities decodes the cached probe result.
func unmarshalCapabilities(raw []byte, out *sshx.Capabilities) error {
	return json.Unmarshal(raw, out)
}
