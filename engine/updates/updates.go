// Package updates implements per-app update checking and automated deployment.
//
// Update modes (stored in state per app):
//   - off    — never check or deploy
//   - notify — check and record availability; never auto-deploy
//   - auto   — check and deploy if a new digest is available
//
// The background timer calls RunAuto which handles both "notify" (record) and
// "auto" (record + deploy) apps.
package updates

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/opslog"
	"github.com/plainwork/boxx/engine/state"
)

// Result is the outcome of checking one app for updates.
type Result struct {
	Slug            string // group slug or single slug
	AppSlug         string // empty for singles; app slug for grouped apps
	Image           string
	CurrentDigest   string
	AvailableDigest string
	UpdateAvailable bool
	Error           error
}

// CheckAll checks every non-"off" app for available updates and updates state.
// It does not deploy anything. Progress is reported via the optional func.
func CheckAll(ctx context.Context, s *state.State, progress func(slug, msg string)) ([]Result, error) {
	var results []Result

	for slug, app := range s.Singles {
		if app.UpdatePolicy.Mode == state.UpdateModeOff {
			continue
		}
		r := checkOne(ctx, slug, "", app.Image, app.UpdatePolicy.CurrentDigest)
		storeResult(s, r)
		results = append(results, r)
		if progress != nil {
			progress(slug, statusMsg(r))
		}
	}

	for gslug, grp := range s.Groups {
		for aslug, app := range grp.Apps {
			if app.UpdatePolicy.Mode == state.UpdateModeOff {
				continue
			}
			r := checkOne(ctx, gslug, aslug, app.Image, app.UpdatePolicy.CurrentDigest)
			storeResult(s, r)
			results = append(results, r)
			if progress != nil {
				progress(gslug+"/"+aslug, statusMsg(r))
			}
		}
	}

	return results, state.Save(s)
}

// Check checks a single app or group-app for updates. Use "" for appSlug when
// checking a single app.
func Check(ctx context.Context, s *state.State, slug, appSlug string) (Result, error) {
	image, current, err := resolveImageAndDigest(s, slug, appSlug)
	if err != nil {
		return Result{Slug: slug, AppSlug: appSlug, Error: err}, err
	}
	if !dockerx.IsMutableTag(image) {
		return Result{Slug: slug, AppSlug: appSlug, Image: image, CurrentDigest: current}, nil
	}

	r := checkOne(ctx, slug, appSlug, image, current)
	storeResult(s, r)
	_ = state.Save(s)
	return r, r.Error
}

// RunAuto checks all non-"off" apps and deploys any "auto" apps that have a
// new digest available. It is designed to be called by the systemd timer
// (`boxx updates run`).
//
// progress receives a message for each significant step (check, deploy, skip).
func RunAuto(ctx context.Context, s *state.State, progress func(slug, msg string)) error {
	results, err := CheckAll(ctx, s, progress)
	if err != nil {
		return err
	}

	for _, r := range results {
		if r.Error != nil || !r.UpdateAvailable {
			continue
		}
		// Determine if this app is set to auto.
		mode, mErr := resolveMode(s, r.Slug, r.AppSlug)
		if mErr != nil {
			continue
		}
		if mode != state.UpdateModeAuto {
			continue
		}

		// Deploy via the existing Deploy function.
		fullSlug := r.Slug
		if r.AppSlug != "" {
			fullSlug = r.Slug + "/" + r.AppSlug
		}

		start := time.Now()
		var deployErr error
		func() {
			defer opslog.Op("update_deploy", r.Slug, r.AppSlug, start, &deployErr)
			if progress != nil {
				progress(fullSlug, "auto-deploying "+r.Image)
			}
			deployErr = installer.Deploy(ctx, installer.DeploySpec{
				Slug: fullSlug,
			}, func(_, msg string) {
				if progress != nil {
					progress(fullSlug, msg)
				}
			})
		}()

		if deployErr != nil {
			if progress != nil {
				progress(fullSlug, "auto-deploy failed: "+deployErr.Error())
			}
		} else {
			if progress != nil {
				progress(fullSlug, "auto-deployed successfully")
			}
		}
	}

	return nil
}

// --------- internal helpers ---------

func checkOne(ctx context.Context, slug, appSlug, image, current string) Result {
	r := Result{Slug: slug, AppSlug: appSlug, Image: image, CurrentDigest: current}

	if !dockerx.IsMutableTag(image) {
		r.AvailableDigest = current
		return r
	}

	remote, err := dockerx.RemoteDigest(ctx, image)
	if err != nil {
		r.Error = fmt.Errorf("check %s: %w", image, err)
		return r
	}
	r.AvailableDigest = remote

	if current == "" {
		// We have no baseline — record remote as available but don't flag as
		// "update available" since we don't know what's running.
		return r
	}

	if !digestsMatch(current, remote) {
		r.UpdateAvailable = true
	}
	return r
}

// digestsMatch compares two digest strings tolerantly — handles short vs. full
// form (sha256:abc vs sha256:abcdef…).
func digestsMatch(a, b string) bool {
	a = normalizeDigest(a)
	b = normalizeDigest(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	// Allow prefix match for short digests.
	if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
		return true
	}
	return false
}

func normalizeDigest(d string) string {
	if i := strings.LastIndex(d, ":"); i >= 0 {
		return d[i+1:]
	}
	return d
}

func statusMsg(r Result) string {
	if r.Error != nil {
		return "check error: " + r.Error.Error()
	}
	if r.UpdateAvailable {
		return "update available"
	}
	return "up to date"
}

func storeResult(s *state.State, r Result) {
	now := time.Now().UTC()

	if r.AppSlug == "" {
		app, ok := s.Singles[r.Slug]
		if !ok {
			return
		}
		app.UpdatePolicy.LastCheck = now
		app.UpdatePolicy.AvailableDigest = r.AvailableDigest
		if r.UpdateAvailable {
			app.UpdatePolicy.LastStatus = "update_available"
		} else if r.Error != nil {
			app.UpdatePolicy.LastStatus = "error"
			app.UpdatePolicy.LastError = r.Error.Error()
		} else {
			app.UpdatePolicy.LastStatus = "ok"
			app.UpdatePolicy.LastError = ""
		}
		s.Singles[r.Slug] = app
		return
	}

	grp, ok := s.Groups[r.Slug]
	if !ok {
		return
	}
	app, ok := grp.Apps[r.AppSlug]
	if !ok {
		return
	}
	app.UpdatePolicy.LastCheck = now
	app.UpdatePolicy.AvailableDigest = r.AvailableDigest
	if r.UpdateAvailable {
		app.UpdatePolicy.LastStatus = "update_available"
	} else if r.Error != nil {
		app.UpdatePolicy.LastStatus = "error"
		app.UpdatePolicy.LastError = r.Error.Error()
	} else {
		app.UpdatePolicy.LastStatus = "ok"
		app.UpdatePolicy.LastError = ""
	}
	grp.Apps[r.AppSlug] = app
	s.Groups[r.Slug] = grp
}

func resolveImageAndDigest(s *state.State, slug, appSlug string) (image, digest string, err error) {
	if appSlug == "" {
		app, ok := s.Singles[slug]
		if !ok {
			return "", "", fmt.Errorf("no single app %q", slug)
		}
		return app.Image, app.UpdatePolicy.CurrentDigest, nil
	}
	grp, ok := s.Groups[slug]
	if !ok {
		return "", "", fmt.Errorf("no group %q", slug)
	}
	app, ok := grp.Apps[appSlug]
	if !ok {
		return "", "", fmt.Errorf("group %q has no app %q", slug, appSlug)
	}
	return app.Image, app.UpdatePolicy.CurrentDigest, nil
}

func resolveMode(s *state.State, slug, appSlug string) (state.UpdateMode, error) {
	if appSlug == "" {
		app, ok := s.Singles[slug]
		if !ok {
			return "", fmt.Errorf("no single app %q", slug)
		}
		return app.UpdatePolicy.Mode, nil
	}
	grp, ok := s.Groups[slug]
	if !ok {
		return "", fmt.Errorf("no group %q", slug)
	}
	app, ok := grp.Apps[appSlug]
	if !ok {
		return "", fmt.Errorf("group %q has no app %q", slug, appSlug)
	}
	return app.UpdatePolicy.Mode, nil
}
