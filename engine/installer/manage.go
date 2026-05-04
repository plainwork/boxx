package installer

import (
	"context"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
)

// Restart restarts the live container for the given slug ("slug" or "group/app").
func Restart(ctx context.Context, slugRef string) error {
	container, err := LiveContainer(slugRef)
	if err != nil {
		return err
	}
	return dockerx.Restart(ctx, container)
}

// Remove removes an app and its resources. keepData preserves storage/db volumes.
func Remove(ctx context.Context, slugRef string, keepData bool) error {
	s, err := state.Load()
	if err != nil {
		return err
	}

	if strings.Contains(slugRef, "/") {
		gslug, aslug, _ := strings.Cut(slugRef, "/")
		g, ok := s.Groups[gslug]
		if !ok {
			return nil
		}
		if _, ok := g.Apps[aslug]; !ok {
			return nil
		}
		fullSlug := gslug + "-" + aslug
		for _, color := range []string{"blue", "green"} {
			_ = dockerx.Rm(ctx, "boxx-app-"+fullSlug+"-"+color)
		}
		if !keepData {
			_ = dockerx.RmVolume(ctx, "boxx_storage_"+fullSlug)
		}
		delete(g.Apps, aslug)
		if len(g.Apps) == 0 {
			if g.DB != nil {
				_ = dockerx.Rm(ctx, g.DB.Container)
				if !keepData {
					_ = dockerx.RmVolume(ctx, "boxx_db_"+gslug)
				}
			}
			delete(s.Groups, gslug)
		} else {
			s.Groups[gslug] = g
		}
		if err := caddy.Apply(ctx, s); err != nil {
			return err
		}
		return state.Save(s)
	}

	if app, ok := s.Singles[slugRef]; ok {
		for _, color := range []string{"blue", "green"} {
			_ = dockerx.Rm(ctx, "boxx-app-"+slugRef+"-"+color)
		}
		if app.DB != nil {
			_ = dockerx.Rm(ctx, app.DB.Container)
		}
		if !keepData {
			_ = dockerx.RmVolume(ctx, "boxx_storage_"+slugRef)
			_ = dockerx.RmVolume(ctx, "boxx_db_"+slugRef)
		}
		delete(s.Singles, slugRef)
		// small drain window
		_ = dockerx.Stop(ctx, "boxx-app-"+slugRef+"-blue", 5)
		_ = dockerx.Stop(ctx, "boxx-app-"+slugRef+"-green", 5)
	}
	if g, ok := s.Groups[slugRef]; ok {
		for aslug := range g.Apps {
			fullSlug := slugRef + "-" + aslug
			for _, color := range []string{"blue", "green"} {
				_ = dockerx.Rm(ctx, "boxx-app-"+fullSlug+"-"+color)
			}
			if !keepData {
				_ = dockerx.RmVolume(ctx, "boxx_storage_"+fullSlug)
			}
		}
		if g.DB != nil {
			_ = dockerx.Rm(ctx, g.DB.Container)
			if !keepData {
				_ = dockerx.RmVolume(ctx, "boxx_db_"+slugRef)
			}
		}
		delete(s.Groups, slugRef)
	}

	if err := caddy.Apply(ctx, s); err != nil {
		return err
	}
	return state.Save(s)
}

// minDuration is the minimum time a loading operation should display.
const MinLoadingDuration = 3 * time.Second
