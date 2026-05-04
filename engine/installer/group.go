package installer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/db"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
	"github.com/plainwork/boxx/engine/util"
)

// GroupSpec is the input for installing a group of apps behind one hostname.
type GroupSpec struct {
	Slug     string     // optional; derived from Hostname if empty
	Hostname string     // required
	DBEngine string     // "" | "mysql" | "postgres" — provisioned once, shared by all apps
	Apps     []GroupApp // required, len >= 1
}

// GroupApp describes one app within a group install.
type GroupApp struct {
	Slug  string            // optional; derived from Image if empty
	Image string            // required
	Path  string            // required, e.g. "/" or "/admin"
	Env   map[string]string // optional; extra env vars injected into the container
}

// InstallGroup runs the install state machine for a group of apps that share
// one hostname and (optionally) one database container.
func InstallGroup(ctx context.Context, spec GroupSpec, progress Progress) (*state.Group, error) {
	if progress == nil {
		progress = noop
	}
	if spec.Hostname == "" {
		return nil, errors.New("hostname is required")
	}
	if len(spec.Apps) == 0 {
		return nil, errors.New("at least one app is required")
	}

	gslug := spec.Slug
	if gslug == "" {
		// Slug the full hostname so two groups on the same box that share a
		// first label (e.g. dev.nurun.co and dev.foo.com) don't collide.
		// "dev.nurun.co" → "dev-nurun-co"
		gslug = util.Slugify(spec.Hostname)
	}

	s, err := state.Load()
	if err != nil {
		return nil, err
	}
	if _, exists := s.Groups[gslug]; exists {
		return nil, fmt.Errorf("group %q is already installed — use 'boxx deploy' to update it", gslug)
	}

	// Validate paths up-front and assign per-app slugs.
	seen := map[string]bool{}
	for i, a := range spec.Apps {
		if a.Image == "" {
			return nil, fmt.Errorf("apps[%d]: image is required", i)
		}
		if a.Path == "" {
			return nil, fmt.Errorf("apps[%d]: path is required (use \"/\" for root)", i)
		}
		if !strings.HasPrefix(a.Path, "/") {
			return nil, fmt.Errorf("apps[%d]: path must start with /", i)
		}
		if a.Slug == "" {
			// Derive slug from path (e.g. "/admin" → "admin", "/" → image-derived).
			pathSlug := strings.Trim(a.Path, "/")
			if pathSlug == "" {
				pathSlug = util.Slugify(a.Image)
			} else {
				pathSlug = util.Slugify(pathSlug)
			}
			spec.Apps[i].Slug = pathSlug
		}
		if seen[spec.Apps[i].Slug] {
			return nil, fmt.Errorf("duplicate app slug %q in group", spec.Apps[i].Slug)
		}
		seen[spec.Apps[i].Slug] = true
	}

	// 1. proxy
	progress("proxy", "ensuring boxx-proxy is running")
	if err := caddy.Ensure(ctx, s.Proxy.Image); err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}

	// 2. shared DB (one per group)
	var dbRec *state.DB
	if spec.DBEngine != "" {
		progress("db", "provisioning shared "+spec.DBEngine+" container for group")
		dbRec, err = db.Provision(ctx, gslug, spec.DBEngine, "", nil)
		if err != nil {
			return nil, fmt.Errorf("db: %w", err)
		}
	}

	// 3. pull all images first so we fail early on auth/registry issues.
	for _, a := range spec.Apps {
		progress("pull", "docker pull "+a.Image)
		if err := dockerx.Pull(ctx, a.Image); err != nil {
			return nil, err
		}
	}

	// 4. start each app blue and probe /up. Container name is unique per group:
	//    boxx-app-<gslug>-<aslug>-blue
	apps := map[string]state.GroupApp{}
	startedContainers := []string{}
	rollback := func() {
		for _, n := range startedContainers {
			_ = dockerx.Rm(ctx, n)
		}
	}
	for _, a := range spec.Apps {
		fullSlug := gslug + "-" + a.Slug
		container := caddyContainerName(fullSlug, "blue")
		env := []string{}
		// spec env (from --env-file) first, then stored env from a prior attempt,
		// then auto-managed keys last so they always win.
		for k, v := range a.Env {
			env = append(env, k+"="+v)
		}
		// stored env first; auto-managed keys appended after so they always win
		if sa, ok := s.Groups[gslug]; ok {
			for k, v := range sa.Apps[a.Slug].Env {
				env = append(env, k+"="+v)
			}
		}
		if dbRec != nil {
			env = append(env, "DATABASE_URL="+db.URL(dbRec))
		}
		if a.Path != "" && a.Path != "/" {
			env = append(env, "BASE_PATH="+strings.TrimRight(a.Path, "/"))
		}
		env = append(env, "PORT=80") // always enforce boxx contract; overrides any .env value
		progress("app", "starting "+container+" at "+a.Path)
		// Remove any stale container from a previous partial install attempt.
		if exists, _ := dockerx.ContainerExists(ctx, container); exists {
			_ = dockerx.Rm(ctx, container)
		}
		if err := dockerx.Run(ctx, dockerx.RunOpts{
			Name:    container,
			Image:   a.Image,
			Network: caddy.Network,
			Restart: "unless-stopped",
			Env:     env,
			Volumes: map[string]string{
				"boxx_storage_" + fullSlug: "/storage",
			},
		}); err != nil {
			rollback()
			return nil, err
		}
		startedContainers = append(startedContainers, container)

		progress("up", "waiting for /up on "+a.Slug)
		if err := waitForUp(ctx, container, 60*time.Second); err != nil {
			logs, _ := dockerx.LogsTail(ctx, container, 50)
			rollback()
			return nil, fmt.Errorf("%s /up never returned 2xx: %w\n--- last logs ---\n%s", a.Slug, err, logs)
		}

		apps[a.Slug] = state.GroupApp{
			Slug:      a.Slug,
			Image:     a.Image,
			Path:      a.Path,
			LiveColor: "blue",
			Registry:  util.RegistryHost(a.Image),
			Env:       a.Env,
		}
	}

	// 5. persist + apply caddy
	g := state.Group{
		Slug:     gslug,
		Hostname: spec.Hostname,
		DB:       dbRec,
		Apps:     apps,
	}
	s.Groups[gslug] = g

	progress("caddy", "applying group routes for "+spec.Hostname)
	if err := caddy.Apply(ctx, s); err != nil {
		rollback()
		delete(s.Groups, gslug)
		return nil, fmt.Errorf("caddy apply: %w", err)
	}
	if err := state.Save(s); err != nil {
		return nil, err
	}
	progress("done", "installed group "+gslug+" → https://"+spec.Hostname)
	return &g, nil
}
