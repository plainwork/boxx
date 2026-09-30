// Package caddy manages the boxx edge proxy:
//
//   - Ensure() makes sure a "boxx-proxy" container is running with our initial
//     config (Admin API on 127.0.0.1:2019, empty HTTP server on :80/:443).
//   - Apply(state) builds the full Caddy JSON config from boxx state and POSTs
//     it to /load — atomic replace, no restart. Every change (install, deploy
//     blue/green flip, hostname edits, remove) goes through Apply.
//
// We only ever talk to Caddy via 127.0.0.1:2019; the Admin API is never
// exposed off-host.
package caddy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
)

// Network is the user-defined bridge that all boxx-managed containers share.
const Network = "boxx_net"

// ProxyContainer is the name of the Caddy container.
const ProxyContainer = "boxx-proxy"

// AdminAddr is where the Caddy Admin API is published on the host loopback.
const AdminAddr = "127.0.0.1:2019"

// Ensure makes sure the network and proxy container exist and are running.
// Safe to call repeatedly. Does not modify route configuration — call Apply for that.
func Ensure(ctx context.Context, image string) error {
	if image == "" {
		image = "caddy:2"
	}
	if err := dockerx.NetworkEnsure(ctx, Network); err != nil {
		return err
	}

	running, err := dockerx.ContainerRunning(ctx, ProxyContainer)
	if err != nil {
		return err
	}
	if running {
		return nil
	}

	exists, err := dockerx.ContainerExists(ctx, ProxyContainer)
	if err != nil {
		return err
	}
	if exists {
		// Stale stopped container; nuke it before re-creating.
		if err := dockerx.Rm(ctx, ProxyContainer); err != nil {
			return err
		}
	}

	if err := writeInitialConfig(); err != nil {
		return err
	}

	if err := dockerx.Run(ctx, dockerx.RunOpts{
		Name:    ProxyContainer,
		Image:   image,
		Network: Network,
		Restart: "unless-stopped",
		Ports: map[string]string{
			"0.0.0.0:80":      "80",
			"0.0.0.0:443":     "443",
			"127.0.0.1:2019":  "2019",
		},
		Volumes: map[string]string{
			state.CaddyDataDir():   "/data",
			state.CaddyConfigDir(): "/config",
		},
		Cmd: []string{"caddy", "run", "--config", "/config/caddy.json", "--resume"},
	}); err != nil {
		return err
	}

	// Wait until the Admin API answers.
	return waitAdminReady(ctx, 30*time.Second)
}

// Version returns the Caddy version running in the proxy container, like "v2.11.4".
func Version(ctx context.Context) (string, error) {
	out, err := dockerx.Exec(ctx, ProxyContainer, "caddy", "version")
	if err != nil {
		return "", err
	}
	v, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	return v, nil
}

// SupportsWildcards reports whether a Caddy version understands the on-demand
// TLS "permission" setting boxx uses for wildcard hostnames (added in v2.8).
func SupportsWildcards(version string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(version, "v"), "%d.%d", &major, &minor); err != nil {
		return false
	}
	return major > 2 || major == 2 && minor >= 8
}

// Status reports whether the proxy container is running.
func Status(ctx context.Context) (running bool, err error) {
	return dockerx.ContainerRunning(ctx, ProxyContainer)
}
