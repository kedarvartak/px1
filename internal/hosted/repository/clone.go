// Package repository reads repository objects as untrusted data. It never
// checks out or executes repository-controlled files.
package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxGitOutput = 64 << 10

type Runner interface {
	Run(context.Context, string, []string, []string) error
}

type Cloner struct {
	GitPath      string
	TempRoot     string
	AllowedHosts map[string]bool
	Timeout      time.Duration
	Runner       Runner
}

type Clone struct {
	Dir  string
	root string
}

func (c Clone) Remove() error { return os.RemoveAll(c.root) }

func (c Cloner) Clone(ctx context.Context, remoteURL, ref string) (clone Clone, err error) {
	if err := c.validateRemote(remoteURL); err != nil {
		return Clone{}, err
	}
	if strings.ContainsAny(ref, "\x00\r\n") {
		return Clone{}, errors.New("repository ref contains control characters")
	}
	dir, err := os.MkdirTemp(c.TempRoot, "px1-clone-*")
	if err != nil {
		return Clone{}, fmt.Errorf("create clone directory: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()

	duration := c.Timeout
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	cloneCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	args := []string{
		"-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never",
		"-c", "credential.helper=", "clone", "--bare", "--depth=1",
		"--filter=blob:limit=1m", "--no-tags", "--single-branch",
	}
	if ref != "" {
		args = append(args, "--branch="+ref)
	}
	args = append(args, "--", remoteURL, filepath.Join(dir, "objects.git"))
	runner := c.Runner
	if runner == nil {
		runner = commandRunner{path: first(c.GitPath, "git")}
	}
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	if err = runner.Run(cloneCtx, first(c.GitPath, "git"), args, env); err != nil {
		return Clone{}, fmt.Errorf("filtered shallow clone failed: %w", err)
	}
	return Clone{Dir: filepath.Join(dir, "objects.git"), root: dir}, nil
}

func (c Cloner) validateRemote(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("repository URL must be HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("repository URL must not contain credentials, query parameters, or fragments")
	}
	allowed := c.AllowedHosts
	if len(allowed) == 0 {
		allowed = map[string]bool{"github.com": true}
	}
	if !allowed[strings.ToLower(parsed.Hostname())] {
		return fmt.Errorf("repository host %q is not allowed", parsed.Hostname())
	}
	return nil
}

type commandRunner struct{ path string }

func (r commandRunner) Run(ctx context.Context, _ string, args, extraEnv []string) error {
	cmd := exec.CommandContext(ctx, r.path, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	var output limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	remaining := maxGitOutput - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return written, nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
