// Package installer turns user intent ("install this image with this hostname")
// into the concrete docker + caddy operations that realize it.
//
// Phase 3 covers single-app installs. Phase 4 will add groups; Phase 5 deploys.
package installer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/db"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
	"github.com/plainwork/boxx/engine/util"
)

// SingleSpec is the input for installing a standalone app.
type SingleSpec struct {
	Image    string
	Hostname string
	DBEngine string            // "" | "mysql" | "postgres"
	Slug     string            // optional; derived from Image if empty
	Env      map[string]string // optional; extra env vars injected into the container
}

// Progress is a callback for surfacing step-by-step status to a TUI or CLI.
type Progress func(step, msg string)

func noop(string, string) {}

// InstallSingle runs the full install state machine for a standalone app.
func InstallSingle(ctx context.Context, spec SingleSpec, progress Progress) (*state.Single, error) {
	if progress == nil {
		progress = noop
	}
	if spec.Image == "" {
		return nil, errors.New("image is required")
	}
	if spec.Hostname == "" {
		return nil, errors.New("hostname is required")
	}
	slug := spec.Slug
	if slug == "" {
		slug = util.Slugify(spec.Image)
	}

	s, err := state.Load()
	if err != nil {
		return nil, err
	}
	if _, exists := s.Singles[slug]; exists {
		return nil, fmt.Errorf("an app with slug %q is already installed (use 'boxx deploy' to update)", slug)
	}

	// 1. proxy + network
	progress("proxy", "ensuring boxx-proxy is running")
	if err := caddy.Ensure(ctx, s.Proxy.Image); err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}

	// 2. pull image
	progress("pull", "docker pull "+spec.Image)
	if err := dockerx.Pull(ctx, spec.Image); err != nil {
		return nil, err
	}

	// 3. optional database
	var dbRec *state.DB
	if spec.DBEngine != "" {
		progress("db", "provisioning "+spec.DBEngine+" container")
		dbRec, err = db.Provision(ctx, slug, spec.DBEngine, "", nil)
		if err != nil {
			return nil, fmt.Errorf("db: %w", err)
		}
	}

	// 4. start app container as blue
	color := "blue"
	containerName := caddyContainerName(slug, color)
	env := []string{}
	// spec.Env (from --env-file) is applied first; auto-managed keys win
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	// stored env from a previous partial attempt (shouldn't exist for new install,
	// but handle gracefully)
	if rec, ok := s.Singles[slug]; ok {
		for k, v := range rec.Env {
			env = append(env, k+"="+v)
		}
	}
	if dbRec != nil {
		env = append(env, "DATABASE_URL="+db.URL(dbRec))
	}
	progress("app", "starting "+containerName)
	if err := dockerx.Run(ctx, dockerx.RunOpts{
		Name:    containerName,
		Image:   spec.Image,
		Network: caddy.Network,
		Restart: "unless-stopped",
		Env:     env,
		Volumes: map[string]string{
			"boxx_storage_" + slug: "/storage",
		},
	}); err != nil {
		return nil, err
	}

	// 5. health probe via /up on the internal network
	progress("up", "waiting for /up to return 2xx")
	if err := waitForUp(ctx, containerName, 60*time.Second); err != nil {
		logs, _ := dockerx.LogsTail(ctx, containerName, 50)
		// Full rollback: app container + DB container + DB volume.
		_ = dockerx.Rm(ctx, containerName)
		if dbRec != nil {
			progress("rollback", "removing db container and volume")
			_ = dockerx.Stop(ctx, dbRec.Container, 5)
			_ = dockerx.Rm(ctx, dbRec.Container)
			_ = dockerx.RmVolume(ctx, "boxx_db_"+slug)
		}
		_ = dockerx.RmVolume(ctx, "boxx_storage_"+slug)
		return nil, fmt.Errorf("/up never returned 2xx: %w\n--- last logs ---\n%s", err, logs)
	}

	// 6. persist state
	app := state.Single{
		Slug:      slug,
		Image:     spec.Image,
		Hostname:  spec.Hostname,
		LiveColor: color,
		DB:        dbRec,
		Registry:  util.RegistryHost(spec.Image),
		Env:       spec.Env,
	}
	s.Singles[slug] = app

	// 7. tell Caddy about the new route
	progress("caddy", "applying new route for "+spec.Hostname)
	if err := caddy.Apply(ctx, s); err != nil {
		_ = dockerx.Rm(ctx, containerName)
		delete(s.Singles, slug)
		return nil, fmt.Errorf("caddy apply: %w", err)
	}

	if err := state.Save(s); err != nil {
		return nil, err
	}
	progress("done", "installed "+slug+" → https://"+spec.Hostname)
	return &app, nil
}

func caddyContainerName(slug, color string) string {
	return "boxx-app-" + slug + "-" + color
}

// waitForUp polls http://<container>/up from inside boxx_net using a one-shot
// curl container until it returns success or the timeout elapses.
func waitForUp(ctx context.Context, container string, max time.Duration) error {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		_, err := dockerx.Exec(ctx, container, "true") // ensure container is still running
		if err != nil {
			return fmt.Errorf("container exited: %w", err)
		}
		// Use a one-shot curl image attached to the same network.
		probe := mkProbe("curlimages/curl:latest", []string{
			"-fsS", "-m", "3", "http://" + container + "/up",
		})
		if probeOK(ctx, probe) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return errors.New("timeout")
}

type probeCmd struct {
	image string
	args  []string
}

func mkProbe(image string, args []string) probeCmd { return probeCmd{image, args} }

// probeOK runs the probe with `docker run --rm --network boxx_net <image> <args>`.
func probeOK(ctx context.Context, p probeCmd) bool {
	args := []string{"run", "--rm", "--network", caddy.Network, p.image}
	args = append(args, p.args...)
	cmd := dockerCommand(ctx, args)
	return cmd.Run() == nil
}
