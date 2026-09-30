// Package hostnames normalizes, validates and matches the public hostnames
// apps answer to, and detects hostnames claimed by more than one app.
//
// Wildcards follow Caddy's host matcher: "*" is allowed only as the whole
// leftmost label and matches exactly one label, so "*.example.com" matches
// "a.example.com" but not "example.com" or "a.b.example.com".
package hostnames

import (
	"fmt"
	"sort"
	"strings"

	"github.com/plainwork/boxx/engine/state"
)

// Parse normalizes and validates a hostname in one step.
func Parse(h string) (string, error) {
	n, err := Normalize(h)
	if err != nil {
		return "", err
	}
	if err := Validate(n); err != nil {
		return "", err
	}
	return n, nil
}

// Normalize trims, lowercases and strips a trailing dot. It rejects input
// that looks like a URL (scheme, path or port) rather than a bare hostname.
func Normalize(h string) (string, error) {
	raw := h
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimSuffix(h, ".")
	switch {
	case h == "":
		return "", fmt.Errorf("hostname is empty")
	case strings.Contains(h, "://"):
		return "", fmt.Errorf("%q: use a bare hostname, without http:// or https://", raw)
	case strings.Contains(h, "/"):
		return "", fmt.Errorf("%q: use a bare hostname, without a path", raw)
	case strings.Contains(h, ":"):
		return "", fmt.Errorf("%q: use a bare hostname, without a port", raw)
	}
	for _, r := range h {
		if r > 127 {
			return "", fmt.Errorf("%q: non-ASCII hostnames must be written in punycode (xn--…)", raw)
		}
	}
	return h, nil
}

// Validate checks that an already-normalized hostname is well formed.
func Validate(h string) error {
	if len(h) > 253 {
		return fmt.Errorf("%q: hostname is longer than 253 characters", h)
	}
	labels := strings.Split(h, ".")
	if labels[0] == "*" {
		if len(labels) < 3 {
			return fmt.Errorf("%q: a wildcard needs at least two labels after it, like *.example.com", h)
		}
		labels = labels[1:]
	}
	for _, l := range labels {
		if err := validLabel(l); err != nil {
			return fmt.Errorf("%q: %w", h, err)
		}
	}
	return nil
}

func validLabel(l string) error {
	if l == "" {
		return fmt.Errorf("empty label")
	}
	if len(l) > 63 {
		return fmt.Errorf("label %q is longer than 63 characters", l)
	}
	if strings.Contains(l, "*") {
		return fmt.Errorf("\"*\" is only allowed as the whole first label, like *.example.com")
	}
	if l[0] == '-' || l[len(l)-1] == '-' {
		return fmt.Errorf("label %q starts or ends with a hyphen", l)
	}
	for _, r := range l {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return fmt.Errorf("label %q contains %q; only letters, digits and hyphens are allowed", l, r)
		}
	}
	return nil
}

// IsWildcard reports whether h is a wildcard pattern ("*.example.com").
func IsWildcard(h string) bool { return strings.HasPrefix(h, "*.") }

// WildcardSuffix returns ".example.com" for "*.example.com".
func WildcardSuffix(h string) string { return strings.TrimPrefix(h, "*") }

// Match reports whether host is matched by pattern, which may be an exact
// hostname or a single-label wildcard.
func Match(pattern, host string) bool {
	if !IsWildcard(pattern) {
		return pattern == host
	}
	first, rest, ok := strings.Cut(host, ".")
	return ok && first != "" && first != "*" && "."+rest == WildcardSuffix(pattern)
}

// Owner identifies the app or group a hostname belongs to.
type Owner struct {
	Group bool
	Slug  string
}

func (o Owner) String() string {
	if o.Group {
		return fmt.Sprintf("group '%s'", o.Slug)
	}
	return fmt.Sprintf("app '%s'", o.Slug)
}

// Owners maps every hostname in state to the apps and groups that claim it.
func Owners(s *state.State) map[string][]Owner {
	out := map[string][]Owner{}
	for slug, a := range s.Singles {
		for _, h := range state.HostNames(a.Hostname, a.Aliases) {
			out[h] = append(out[h], Owner{Slug: slug})
		}
	}
	for slug, g := range s.Groups {
		for _, h := range state.HostNames(g.Hostname, g.Aliases) {
			out[h] = append(out[h], Owner{Group: true, Slug: slug})
		}
	}
	for h := range out {
		sort.Slice(out[h], func(i, j int) bool {
			a, b := out[h][i], out[h][j]
			if a.Group != b.Group {
				return !a.Group
			}
			return a.Slug < b.Slug
		})
	}
	return out
}

// CheckAvailable returns an error if any of hosts is listed twice or is
// already claimed by an app other than self.
func CheckAvailable(s *state.State, hosts []string, self Owner) error {
	owners := Owners(s)
	seen := map[string]bool{}
	for _, h := range hosts {
		if seen[h] {
			return fmt.Errorf("%s is listed more than once", h)
		}
		seen[h] = true
		for _, o := range owners[h] {
			if o != self {
				return fmt.Errorf("%s is already used by %s", h, o)
			}
		}
	}
	return nil
}

// Overlaps describes, for each of hosts, any other app's hostname that
// overlaps it through a wildcard. These are allowed (the exact hostname
// wins), but worth telling the user about.
func Overlaps(s *state.State, hosts []string, self Owner) []string {
	var notes []string
	owners := Owners(s)
	patterns := sortedKeys(owners)
	for _, h := range hosts {
		for _, p := range patterns {
			if p == h {
				continue
			}
			if !Match(p, h) && !Match(h, p) {
				continue
			}
			for _, o := range owners[p] {
				if o != self {
					notes = append(notes, fmt.Sprintf("%s overlaps %s on %s; the exact hostname takes priority over the wildcard", h, p, o))
				}
			}
		}
	}
	return notes
}

// Duplicates returns every hostname claimed by more than one app or group.
func Duplicates(s *state.State) map[string][]Owner {
	out := map[string][]Owner{}
	for h, os := range Owners(s) {
		if len(os) > 1 {
			out[h] = os
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
