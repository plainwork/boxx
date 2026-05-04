package caddy

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/plainwork/boxx/engine/state"
)

// IsLocalHostname reports whether a hostname is a local-only name that
// public ACME (Let's Encrypt) cannot issue a certificate for.
//
// True for: "localhost", *.localhost, *.local, *.test, *.internal,
// *.lan, *.home, *.home.arpa, and raw IP addresses.
func IsLocalHostname(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" {
		return false
	}
	if h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return true
	}
	for _, suffix := range []string{
		".localhost", ".local", ".test", ".internal",
		".lan", ".home", ".home.arpa",
	} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

// BuildConfig produces the full Caddy JSON config that reflects boxx state.
//
// Layout:
//   - One HTTP server "srv0" listening on :80 and :443
//   - automatic_https enabled (Let's Encrypt)
//   - One route per single-app hostname (host matcher → reverse_proxy to live container)
//   - One route per group hostname containing sub-routes per path
//
// We always emit a complete config so /load is fully idempotent.
func BuildConfig(s *state.State) map[string]any {
	routes := []any{}
	localHosts := []string{}
	addHost := func(h string) {
		if IsLocalHostname(h) {
			localHosts = append(localHosts, h)
		}
	}

	// Singles: one route per hostname → live container on port 80 over boxx_net.
	singleSlugs := sortedKeys(s.Singles)
	for _, slug := range singleSlugs {
		app := s.Singles[slug]
		if app.Hostname == "" {
			continue
		}
		addHost(app.Hostname)
		dial := containerName(slug, app.LiveColor) + ":80"
		routes = append(routes, hostRoute(app.Hostname, []any{
			pathHandler("", dial),
		}))
	}

	// Groups: one route per hostname; sub-routes per app path, longest-prefix first.
	groupSlugs := sortedKeys(s.Groups)
	for _, gslug := range groupSlugs {
		g := s.Groups[gslug]
		if g.Hostname == "" {
			continue
		}
		addHost(g.Hostname)
		appSlugs := sortedKeys(g.Apps)
		// longer paths first so /admin matches before /
		sort.SliceStable(appSlugs, func(i, j int) bool {
			return len(g.Apps[appSlugs[i]].Path) > len(g.Apps[appSlugs[j]].Path)
		})
		handlers := []any{}
		for _, aslug := range appSlugs {
			a := g.Apps[aslug]
			dial := containerName(gslug+"-"+aslug, a.LiveColor) + ":80"
			handlers = append(handlers, pathHandler(a.Path, dial))
		}
		routes = append(routes, hostRoute(g.Hostname, handlers))
	}

	// Skip ACME for local-only hostnames (.localhost, .local, .test, IPs, …).
	// Caddy will still serve them over HTTP on :80; HTTPS on :443 falls back
	// to its internal self-signed CA automatically for those names.
	autoHTTPS := map[string]any{"disable": false}
	if len(localHosts) > 0 {
		autoHTTPS["skip_certificates"] = localHosts
	}

	return map[string]any{
		"admin": map[string]any{
			"listen": "0.0.0.0:2019",
		},
		"logging": map[string]any{
			"logs": map[string]any{
				"access": map[string]any{
					"writer": map[string]any{
						"output":   "file",
						"filename": "/data/logs/access.log",
					},
					"encoder":  map[string]any{"format": "json"},
					"include":  []string{"http.log.access"},
					"level":    "INFO",
				},
			},
		},
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"srv0": map[string]any{
						"listen":          []string{":80", ":443"},
						"automatic_https": autoHTTPS,
						"routes":          routes,
						"logs": map[string]any{
							"default_logger_name": "access",
						},
					},
				},
			},
		},
	}
}

// hostRoute wraps a list of handlers in a host matcher.
func hostRoute(host string, handlers []any) map[string]any {
	return map[string]any{
		"match": []any{
			map[string]any{"host": []string{host}},
		},
		"handle": []any{
			map[string]any{
				"handler": "subroute",
				"routes":  handlers,
			},
		},
		"terminal": true,
	}
}

// pathHandler returns a route entry that reverse-proxies a path prefix to dial.
// path == "" or "/" means "match anything".
func pathHandler(path, dial string) map[string]any {
	r := map[string]any{
		"handle": []any{
			map[string]any{
				"handler":   "reverse_proxy",
				"upstreams": []any{map[string]any{"dial": dial}},
			},
		},
	}
	if path != "" && path != "/" {
		// Match both the exact prefix ("/admin") and any sub-path ("/admin/*").
		// Then rewrite to strip the prefix so the app sees "/" internally.
		prefix := strings.TrimRight(path, "/")
		r["match"] = []any{map[string]any{"path": []string{prefix, prefix + "/*"}}}
		r["handle"] = []any{
			map[string]any{
				"handler":            "rewrite",
				"strip_path_prefix": prefix,
			},
			map[string]any{
				"handler":   "reverse_proxy",
				"upstreams": []any{map[string]any{"dial": dial}},
			},
		}
	}
	return r
}

// containerName mirrors installer convention: boxx-app-<slug>-<color>
func containerName(slug, color string) string {
	if color == "" {
		color = "blue"
	}
	return "boxx-app-" + slug + "-" + color
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeInitialConfig writes a minimal /config/caddy.json on the host so the
// proxy container has something to load on first start.
func writeInitialConfig() error {
	cfg := map[string]any{
		"admin": map[string]any{"listen": "0.0.0.0:2019"},
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"srv0": map[string]any{
						"listen": []string{":80", ":443"},
						"routes": []any{},
					},
				},
			},
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(state.CaddyConfigDir(), "caddy.json"), b, 0o644)
}
