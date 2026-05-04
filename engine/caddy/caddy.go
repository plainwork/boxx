// Package caddy manages the boxx edge proxy:
//
//   - Ensure() makes sure a "boxx-proxy" container is running with our initial
//     config (Admin API on 127.0.0.1:2019, empty HTTP server on :80/:443).
//   - Apply(state) builds the full Caddy JSON config from boxx state and POSTs
//     it to /load — atomic replace, no restart.
//   - SwapUpstream(slug, newDial) does a targeted PATCH so a deploy can flip
//     blue/green without re-loading the whole config.
//
// We only ever talk to Caddy via 127.0.0.1:2019; the Admin API is never
// exposed off-host.
package caddy

import (
	"context"
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

// Status reports whether the proxy container is running.
func Status(ctx context.Context) (running bool, err error) {
	return dockerx.ContainerRunning(ctx, ProxyContainer)
}
