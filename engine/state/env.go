package state

import (
	"fmt"
	"maps"
	"strings"
	"time"
)

// SplitRef splits an app reference into its slug and, for a group's app, the
// app within it: "blog" → ("blog", ""), "dev/nurun" → ("dev", "nurun").
func SplitRef(ref string) (slug, app string) {
	slug, app, _ = strings.Cut(ref, "/")
	return slug, app
}

// AppEnv returns a copy of the stored env for a single app (app == "") or for
// an app within a group.
func (s *State) AppEnv(slug, app string) (map[string]string, error) {
	if app == "" {
		a, ok := s.Singles[slug]
		if !ok {
			return nil, fmt.Errorf("app %q not found", slug)
		}
		return maps.Clone(a.Env), nil
	}
	g, ok := s.Groups[slug]
	if !ok {
		return nil, fmt.Errorf("group %q not found", slug)
	}
	a, ok := g.Apps[app]
	if !ok {
		return nil, fmt.Errorf("app %q not found in group %q", app, slug)
	}
	return maps.Clone(a.Env), nil
}

// SetAppEnv replaces an app's env, keeping the env it replaces as the one-step
// rollback (PrevEnv). The caller saves the state.
func (s *State) SetAppEnv(slug, app string, env map[string]string, reason string) {
	backup := func(prev map[string]string) *EnvBackup {
		return &EnvBackup{Env: maps.Clone(prev), BackupTime: time.Now().UTC(), Reason: reason}
	}
	if app == "" {
		a := s.Singles[slug]
		a.PrevEnv = backup(a.Env)
		a.Env = env
		s.Singles[slug] = a
		return
	}
	g := s.Groups[slug]
	a := g.Apps[app]
	a.PrevEnv = backup(a.Env)
	a.Env = env
	g.Apps[app] = a
	s.Groups[slug] = g
}
