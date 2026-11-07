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
	"github.com/plainwork/boxx/engine/hostnames"
	"github.com/plainwork/boxx/engine/state"
	"github.com/plainwork/boxx/engine/util"
)

// GroupSpec is the input for installing a group of apps behind one hostname.
type GroupSpec struct {
	Slug     string       // optional; derived from Hostname if empty
	Hostname string       // required; primary hostname
	Aliases  []state.Host // optional; extra hostnames, served or redirecting
	DBEngine string       // "" | "mysql" | "postgres" — provisioned once, shared by all apps
	Apps     []GroupApp   // required, len >= 1
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
	spec.Hostname, spec.Aliases, err = PrepareHosts(s, spec.Hostname, spec.Aliases, hostnames.Owner{Group: true, Slug: gslug})
	if err != nil {
		return nil, err
	}

	if err := prepareGroupApps(spec.Apps, nil); err != nil {
		return nil, err
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

	// 3. pull all images first so we fail early on auth/registry issues,
	// 4. then start each app blue and probe /up.
	apps, rollback, err := startGroupApps(ctx, gslug, spec.Apps, dbRec, progress)
	if err != nil {
		return nil, err
	}

	// 5. persist + apply caddy
	g := state.Group{
		Slug:     gslug,
		Hostname: spec.Hostname,
		Aliases:  spec.Aliases,
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

// AddGroupApps installs more apps into an existing group. The new apps share
// the group's hostnames and database; the apps already running are untouched.
func AddGroupApps(ctx context.Context, gslug string, add []GroupApp, progress Progress) (*state.Group, error) {
	if progress == nil {
		progress = noop
	}
	if len(add) == 0 {
		return nil, errors.New("at least one app is required")
	}
	s, err := state.Load()
	if err != nil {
		return nil, err
	}
	g, ok := s.Groups[gslug]
	if !ok {
		return nil, fmt.Errorf("no group %q — run 'boxx ls' to list groups", gslug)
	}
	if err := prepareGroupApps(add, g.Apps); err != nil {
		return nil, err
	}

	progress("proxy", "ensuring boxx-proxy is running")
	if err := caddy.Ensure(ctx, s.Proxy.Image); err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}

	started, rollback, err := startGroupApps(ctx, gslug, add, g.DB, progress)
	if err != nil {
		return nil, err
	}

	apps := make(map[string]state.GroupApp, len(g.Apps)+len(started))
	for k, v := range g.Apps {
		apps[k] = v
	}
	for k, v := range started {
		apps[k] = v
	}
	prev := g
	g.Apps = apps
	s.Groups[gslug] = g

	progress("caddy", "applying group routes for "+g.Hostname)
	if err := caddy.Apply(ctx, s); err != nil {
		rollback()
		s.Groups[gslug] = prev
		return nil, fmt.Errorf("caddy apply: %w", err)
	}
	if err := state.Save(s); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(add))
	for _, a := range add {
		names = append(names, a.Slug+" at "+a.Path)
	}
	progress("done", "added "+strings.Join(names, ", ")+" to group "+gslug)
	return &g, nil
}

// routePath is the prefix Caddy matches for a group app: "" for the root.
func routePath(p string) string { return strings.TrimRight(p, "/") }

// prepareGroupApps validates new group apps and fills in their slugs. existing
// holds the apps already in the group; a new app may not reuse their slugs or paths.
func prepareGroupApps(apps []GroupApp, existing map[string]state.GroupApp) error {
	slugs := map[string]bool{}
	paths := map[string]string{} // route path → slug
	for slug, a := range existing {
		slugs[slug] = true
		paths[routePath(a.Path)] = slug
	}
	for i, a := range apps {
		if a.Image == "" {
			return fmt.Errorf("apps[%d]: image is required", i)
		}
		if a.Path == "" {
			return fmt.Errorf("apps[%d]: path is required (use \"/\" for root)", i)
		}
		if !strings.HasPrefix(a.Path, "/") {
			return fmt.Errorf("apps[%d]: path must start with /", i)
		}
		if a.Slug == "" {
			// Derive slug from path (e.g. "/admin" → "admin", "/" → image-derived).
			pathSlug := strings.Trim(a.Path, "/")
			if pathSlug == "" {
				pathSlug = util.Slugify(a.Image)
			} else {
				pathSlug = util.Slugify(pathSlug)
			}
			apps[i].Slug = pathSlug
		}
		if slugs[apps[i].Slug] {
			return fmt.Errorf("duplicate app slug %q in group", apps[i].Slug)
		}
		slugs[apps[i].Slug] = true
		rp := routePath(a.Path)
		if other, taken := paths[rp]; taken {
			shown := rp
			if shown == "" {
				shown = "/"
			}
			return fmt.Errorf("path %s is already served by %q in this group", shown, other)
		}
		paths[rp] = apps[i].Slug
	}
	return nil
}

// startGroupApps pulls every image, then starts each app as its blue container
// and waits for /up. On error it removes what it started. On success it returns
// the apps' state and a rollback func for failures later in the caller.
func startGroupApps(ctx context.Context, gslug string, add []GroupApp, dbRec *state.DB, progress Progress) (map[string]state.GroupApp, func(), error) {
	// pull first so we fail early on auth/registry issues
	for _, a := range add {
		progress("pull", "docker pull "+a.Image)
		if err := dockerx.Pull(ctx, a.Image); err != nil {
			return nil, nil, err
		}
	}

	// Container name is unique per group: boxx-app-<gslug>-<aslug>-blue
	apps := map[string]state.GroupApp{}
	started := []string{}
	rollback := func() {
		for _, n := range started {
			_ = dockerx.Rm(ctx, n)
		}
	}
	for _, a := range add {
		fullSlug := gslug + "-" + a.Slug
		container := caddyContainerName(fullSlug, "blue")
		// spec env (from --env-file) first, then auto-managed keys last so they always win.
		env := []string{}
		for k, v := range a.Env {
			env = append(env, k+"="+v)
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
			return nil, nil, err
		}
		started = append(started, container)

		progress("up", "waiting for /up on "+a.Slug)
		if err := waitForUp(ctx, container, 60*time.Second); err != nil {
			logs, _ := dockerx.LogsTail(ctx, container, 50)
			rollback()
			return nil, nil, fmt.Errorf("%s /up never returned 2xx: %w\n--- last logs ---\n%s", a.Slug, err, logs)
		}

		up := state.UpdatePolicy{Mode: state.UpdateModeNotify}
		if d := dockerx.LocalDigest(ctx, a.Image); d != "" {
			up.CurrentDigest = d
		}
		apps[a.Slug] = state.GroupApp{
			Slug:         a.Slug,
			Image:        a.Image,
			Path:         a.Path,
			LiveColor:    "blue",
			Registry:     util.RegistryHost(a.Image),
			Env:          a.Env,
			UpdatePolicy: up,
		}
	}
	return apps, rollback, nil
}
