// Package dockerx is a thin wrapper around the local docker CLI.
//
// We shell out to `docker` rather than vendoring the full Docker Engine SDK.
// Rationale: the Engine SDK pulls in a lot of transitive dependencies and
// platform-specific code; for the small set of operations boxx needs (pull,
// run, stop, rm, inspect, exec, logs, stats stream) the CLI is simpler,
// already installed on every supported host, and matches the user's mental
// model 1:1 — anything boxx does can be reproduced manually with `docker`.
package dockerx

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Ping verifies the docker daemon is reachable.
func Ping(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker CLI not found on PATH")
	}
	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// ServerVersion returns the docker server version string.
func ServerVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
