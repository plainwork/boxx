package installer

import (
	"context"
	"fmt"
	"strings"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/hostnames"
	"github.com/plainwork/boxx/engine/state"
)

// PrepareHosts normalizes and validates a primary hostname and its aliases,
// and checks none of them is already used by an app other than self.
func PrepareHosts(s *state.State, primary string, aliases []state.Host, self hostnames.Owner) (string, []state.Host, error) {
	p, err := hostnames.Parse(primary)
	if err != nil {
		return "", nil, err
	}
	if hostnames.IsWildcard(p) {
		return "", nil, fmt.Errorf("%s: the primary hostname can't be a wildcard; add it as an extra hostname instead", p)
	}
	all := []string{p}
	out := make([]state.Host, 0, len(aliases))
	for _, a := range aliases {
		n, err := hostnames.Parse(a.Name)
		if err != nil {
			return "", nil, err
		}
		all = append(all, n)
		out = append(out, state.Host{Name: n, Redirect: a.Redirect})
	}
	if err := hostnames.CheckAvailable(s, all, self); err != nil {
		return "", nil, err
	}
	return p, out, nil
}

// HostOwner resolves an app reference to the owner of its hostnames. A
// "group/app" reference is rejected: hostnames belong to the whole group.
func HostOwner(s *state.State, ref string) (hostnames.Owner, error) {
	if g, _, ok := strings.Cut(ref, "/"); ok {
		return hostnames.Owner{}, fmt.Errorf("hostnames belong to the group; use '%s'", g)
	}
	if _, ok := s.Singles[ref]; ok {
		return hostnames.Owner{Slug: ref}, nil
	}
	if _, ok := s.Groups[ref]; ok {
		return hostnames.Owner{Group: true, Slug: ref}, nil
	}
	return hostnames.Owner{}, fmt.Errorf("no app or group %q", ref)
}

// Hosts returns the primary hostname and aliases for an app or group.
func Hosts(ref string) (primary string, aliases []state.Host, err error) {
	s, err := state.Load()
	if err != nil {
		return "", nil, err
	}
	o, err := HostOwner(s, ref)
	if err != nil {
		return "", nil, err
	}
	primary, aliases = getHosts(s, o)
	return primary, aliases, nil
}

// AddHosts adds extra hostnames to an app or group.
func AddHosts(ctx context.Context, ref string, add []state.Host) error {
	return editHosts(ctx, ref, func(primary string, aliases []state.Host) (string, []state.Host, error) {
		return primary, append(aliases, add...), nil
	})
}

// RemoveHost removes an extra hostname. The primary can't be removed.
func RemoveHost(ctx context.Context, ref, name string) error {
	return editHosts(ctx, ref, func(primary string, aliases []state.Host) (string, []state.Host, error) {
		name, err := hostnames.Normalize(name)
		if err != nil {
			return "", nil, err
		}
		if name == primary {
			return "", nil, fmt.Errorf("%s is the primary hostname; make another hostname primary first", name)
		}
		i := indexHost(aliases, name)
		if i < 0 {
			return "", nil, fmt.Errorf("%s is not one of this app's hostnames", name)
		}
		return primary, append(aliases[:i:i], aliases[i+1:]...), nil
	})
}

// SetRedirect switches an extra hostname between serving the app and
// redirecting to the primary.
func SetRedirect(ctx context.Context, ref, name string, redirect bool) error {
	return editHosts(ctx, ref, func(primary string, aliases []state.Host) (string, []state.Host, error) {
		name, err := hostnames.Normalize(name)
		if err != nil {
			return "", nil, err
		}
		if name == primary {
			return "", nil, fmt.Errorf("%s is the primary hostname; it can't redirect to itself", name)
		}
		i := indexHost(aliases, name)
		if i < 0 {
			return "", nil, fmt.Errorf("%s is not one of this app's hostnames", name)
		}
		out := append([]state.Host(nil), aliases...)
		out[i].Redirect = redirect
		return primary, out, nil
	})
}

// SetPrimary makes an extra hostname the primary. The old primary becomes a
// served extra hostname.
func SetPrimary(ctx context.Context, ref, name string) error {
	return editHosts(ctx, ref, func(primary string, aliases []state.Host) (string, []state.Host, error) {
		name, err := hostnames.Normalize(name)
		if err != nil {
			return "", nil, err
		}
		if name == primary {
			return primary, aliases, nil
		}
		i := indexHost(aliases, name)
		if i < 0 {
			return "", nil, fmt.Errorf("%s is not one of this app's hostnames", name)
		}
		out := append([]state.Host(nil), aliases...)
		out[i] = state.Host{Name: primary}
		return name, out, nil
	})
}

// editHosts loads state, applies fn to the app's hostnames, validates the
// result, and applies it to Caddy before saving. If Caddy rejects the new
// config, state is left untouched.
func editHosts(ctx context.Context, ref string, fn func(string, []state.Host) (string, []state.Host, error)) error {
	s, err := state.Load()
	if err != nil {
		return err
	}
	o, err := HostOwner(s, ref)
	if err != nil {
		return err
	}
	primary, aliases, err := fn(getHosts(s, o))
	if err != nil {
		return err
	}
	primary, aliases, err = PrepareHosts(s, primary, aliases, o)
	if err != nil {
		return err
	}
	setHosts(s, o, primary, aliases)
	if err := caddy.Apply(ctx, s); err != nil {
		return fmt.Errorf("caddy apply: %w", err)
	}
	return state.Save(s)
}

func getHosts(s *state.State, o hostnames.Owner) (string, []state.Host) {
	if o.Group {
		g := s.Groups[o.Slug]
		return g.Hostname, g.Aliases
	}
	a := s.Singles[o.Slug]
	return a.Hostname, a.Aliases
}

func setHosts(s *state.State, o hostnames.Owner, primary string, aliases []state.Host) {
	if o.Group {
		g := s.Groups[o.Slug]
		g.Hostname, g.Aliases = primary, aliases
		s.Groups[o.Slug] = g
		return
	}
	a := s.Singles[o.Slug]
	a.Hostname, a.Aliases = primary, aliases
	s.Singles[o.Slug] = a
}

func indexHost(hosts []state.Host, name string) int {
	for i, h := range hosts {
		if h.Name == name {
			return i
		}
	}
	return -1
}
