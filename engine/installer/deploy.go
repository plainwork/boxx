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
)

// DeploySpec selects an installed app to redeploy and (optionally) the new image.
type DeploySpec struct {
	Slug      string // single-app slug or "<group>/<app>" for grouped apps
	NewImage  string // optional; when "" we re-pull the existing image tag
	DrainSecs int    // grace period after the proxy swap before removing the old container; default 10
}

// Deploy performs a blue/green deploy:
//  1. determine the idle color
//  2. pull image
//  3. launch new container as the idle color
//  4. probe /up
//  5. swap Caddy upstream atomically (config replace; Caddy diffs internally)
//  6. drain, then remove the old container
//  7. persist new live_color
//
// Any failure before the swap leaves the live container untouched.
func Deploy(ctx context.Context, spec DeploySpec, progress Progress) error {
	if progress == nil {
		progress = noop
	}
	if spec.DrainSecs <= 0 {
		spec.DrainSecs = 10
	}

	s, err := state.Load()
	if err != nil {
		return err
	}

	target, err := resolveTarget(s, spec.Slug)
	if err != nil {
		return err
	}

	image := spec.NewImage
	if image == "" {
		image = target.image()
	}

	progress("pull", "docker pull "+image)
	if err := dockerx.Pull(ctx, image); err != nil {
		return err
	}

	oldColor := target.color()
	newColor := flipColor(oldColor)
	oldContainer := caddyContainerName(target.containerSlug(), oldColor)
	newContainer := caddyContainerName(target.containerSlug(), newColor)

	// Make sure no stale "new" container exists from a prior failed deploy.
	_ = dockerx.Rm(ctx, newContainer)

	env := []string{}
	for k, v := range target.storedEnv() {
		env = append(env, k+"="+v)
	}
	// auto-managed keys always appended last so they take precedence
	if d := target.db(); d != nil {
		env = append(env, "DATABASE_URL="+db.URL(d))
	}
	if p := target.path(); p != "" && p != "/" {
		env = append(env, "BASE_PATH="+strings.TrimRight(p, "/"))
	}
	env = append(env, "PORT=80") // always enforce boxx contract; overrides any .env value

	progress("app", "starting "+newContainer)
	if err := dockerx.Run(ctx, dockerx.RunOpts{
		Name:    newContainer,
		Image:   image,
		Network: caddy.Network,
		Restart: "unless-stopped",
		Env:     env,
		Volumes: map[string]string{
			"boxx_storage_" + target.containerSlug(): "/storage",
		},
	}); err != nil {
		return err
	}

	progress("up", "waiting for /up on "+newContainer)
	if err := waitForUp(ctx, newContainer, 60*time.Second); err != nil {
		// If it looks like a DB auth failure, try resetting the MySQL/Postgres user
		// (happens when the DB container was restarted against an existing volume —
		// MySQL ignores MYSQL_USER env vars after first initialisation).
		if dbRec := target.db(); dbRec != nil {
			progress("db-fix", "retrying after db user reset")
			if resetErr := db.ResetAppUser(ctx, dbRec); resetErr == nil {
				// Give the app container a moment to reconnect.
				_ = dockerx.Stop(ctx, newContainer, 3)
				_ = dockerx.Rm(ctx, newContainer)
				_ = dockerx.Run(ctx, dockerx.RunOpts{
					Name:    newContainer,
					Image:   image,
					Network: caddy.Network,
					Restart: "unless-stopped",
					Env:     env,
					Volumes: map[string]string{
						"boxx_storage_" + target.containerSlug(): "/storage",
					},
				})
				if err2 := waitForUp(ctx, newContainer, 60*time.Second); err2 == nil {
					err = nil // recovery succeeded
				}
			}
		}
		if err != nil {
			logs, _ := dockerx.LogsTail(ctx, newContainer, 50)
			_ = dockerx.Rm(ctx, newContainer)
			return fmt.Errorf("/up never returned 2xx: %w\n--- last logs ---\n%s", err, logs)
		}
	}

	// Flip live_color in state and re-apply Caddy. Caddy's /load is atomic.
	target.setColor(s, newColor)
	progress("swap", "switching proxy to "+newContainer)
	if err := caddy.Apply(ctx, s); err != nil {
		// Roll back state + container.
		target.setColor(s, oldColor)
		_ = dockerx.Rm(ctx, newContainer)
		return fmt.Errorf("caddy swap: %w", err)
	}
	if err := state.Save(s); err != nil {
		return err
	}

	progress("drain", fmt.Sprintf("draining %ds before removing %s", spec.DrainSecs, oldContainer))
	select {
	case <-time.After(time.Duration(spec.DrainSecs) * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}

	progress("clean", "removing "+oldContainer)
	_ = dockerx.Stop(ctx, oldContainer, 5)
	_ = dockerx.Rm(ctx, oldContainer)

	// Update image tag in state if it changed, and record new deployed digest.
	if spec.NewImage != "" {
		target.setImage(s, spec.NewImage)
	}
	if d := dockerx.LocalDigest(ctx, image); d != "" {
		target.setDigest(s, d)
	}
	_ = state.Save(s)

	progress("done", "deployed "+spec.Slug+" → "+newColor)
	return nil
}

func flipColor(c string) string {
	if c == "blue" {
		return "green"
	}
	return "blue"
}

// ---------- target resolution (single app or group/app) ----------

type deployTarget struct {
	kind     string // "single" | "group"
	slug     string
	groupApp string // only when kind == "group"
}

func resolveTarget(s *state.State, slugRef string) (*deployTarget, error) {
	if slugRef == "" {
		return nil, errors.New("slug is required")
	}
	// "group/app" form
	for i := 0; i < len(slugRef); i++ {
		if slugRef[i] == '/' {
			gslug, aslug := slugRef[:i], slugRef[i+1:]
			g, ok := s.Groups[gslug]
			if !ok {
				return nil, fmt.Errorf("no group %q", gslug)
			}
			if _, ok := g.Apps[aslug]; !ok {
				return nil, fmt.Errorf("group %q has no app %q", gslug, aslug)
			}
			return &deployTarget{kind: "group", slug: gslug, groupApp: aslug}, nil
		}
	}
	if _, ok := s.Singles[slugRef]; ok {
		return &deployTarget{kind: "single", slug: slugRef}, nil
	}
	return nil, fmt.Errorf("no installed app with slug %q (try \"group/app\" form)", slugRef)
}

func (t *deployTarget) image() string {
	s, _ := state.Load()
	if t.kind == "single" {
		return s.Singles[t.slug].Image
	}
	return s.Groups[t.slug].Apps[t.groupApp].Image
}

func (t *deployTarget) color() string {
	s, _ := state.Load()
	if t.kind == "single" {
		return s.Singles[t.slug].LiveColor
	}
	return s.Groups[t.slug].Apps[t.groupApp].LiveColor
}

func (t *deployTarget) db() *state.DB {
	s, _ := state.Load()
	if t.kind == "single" {
		return s.Singles[t.slug].DB
	}
	return s.Groups[t.slug].DB
}

func (t *deployTarget) path() string {
	if t.kind == "single" {
		return "/"
	}
	s, _ := state.Load()
	return s.Groups[t.slug].Apps[t.groupApp].Path
}

func (t *deployTarget) storedEnv() map[string]string {
	s, _ := state.Load()
	if t.kind == "single" {
		return s.Singles[t.slug].Env
	}
	return s.Groups[t.slug].Apps[t.groupApp].Env
}

// containerSlug is the slug fragment used in container names. For grouped
// apps it's "<group>-<app>" so each container name remains unique.
func (t *deployTarget) containerSlug() string {
	if t.kind == "single" {
		return t.slug
	}
	return t.slug + "-" + t.groupApp
}

func (t *deployTarget) setColor(s *state.State, color string) {
	if t.kind == "single" {
		a := s.Singles[t.slug]
		a.LiveColor = color
		s.Singles[t.slug] = a
		return
	}
	g := s.Groups[t.slug]
	a := g.Apps[t.groupApp]
	a.LiveColor = color
	g.Apps[t.groupApp] = a
	s.Groups[t.slug] = g
}

func (t *deployTarget) setImage(s *state.State, image string) {
	if t.kind == "single" {
		a := s.Singles[t.slug]
		a.Image = image
		s.Singles[t.slug] = a
		return
	}
	g := s.Groups[t.slug]
	a := g.Apps[t.groupApp]
	a.Image = image
	g.Apps[t.groupApp] = a
	s.Groups[t.slug] = g
}

// setDigest updates CurrentDigest and clears AvailableDigest (deploy consumed any pending update).
func (t *deployTarget) setDigest(s *state.State, digest string) {
	if t.kind == "single" {
		a := s.Singles[t.slug]
		a.UpdatePolicy.CurrentDigest = digest
		a.UpdatePolicy.AvailableDigest = ""
		if a.UpdatePolicy.LastStatus == "update_available" {
			a.UpdatePolicy.LastStatus = "ok"
		}
		s.Singles[t.slug] = a
		return
	}
	g := s.Groups[t.slug]
	a := g.Apps[t.groupApp]
	a.UpdatePolicy.CurrentDigest = digest
	a.UpdatePolicy.AvailableDigest = ""
	if a.UpdatePolicy.LastStatus == "update_available" {
		a.UpdatePolicy.LastStatus = "ok"
	}
	g.Apps[t.groupApp] = a
	s.Groups[t.slug] = g
}
