package installer

import (
	"context"
	"os/exec"
)

// dockerCommand is split out so tests can swap it later.
func dockerCommand(ctx context.Context, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, "docker", args...)
}
