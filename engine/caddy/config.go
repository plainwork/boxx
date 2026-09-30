package caddy

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/plainwork/boxx/engine/hostnames"
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

// AskAddr is where the in-container on-demand TLS permission endpoint listens.
// It is served by Caddy itself and never published outside the container.
const AskAddr = "127.0.0.1:5555"

// site is one app (single or group) as the proxy sees it.
type site struct {
	primary  string
	aliases  []state.Host
	handlers []any
}

// BuildConfig produces the full Caddy JSON config that reflects boxx state.
//
// Layout:
//   - One HTTPS server "srv0" listening on :443 (Caddy auto-redirects :80 → :443)
//   - automatic_https enabled (Let's Encrypt)
//   - One route per app matching its exact hostnames (host matcher →
//     reverse_proxy to the live container; groups get sub-routes per path)
//   - One route per wildcard hostname, after all exact routes so an exact
//     name on one app wins over a wildcard on another
//   - Redirect aliases get a route that 308s to the app's primary hostname
//   - A hostname claimed by two apps goes to the first one only (singles
//     before groups, by slug); `boxx doctor` reports the duplicate
//   - Public wildcards get certificates on demand, gated by the "ask" server
//
// We always emit a complete config so /load is fully idempotent.
func BuildConfig(s *state.State) map[string]any {
	sites := []site{}

	// Singles: live container on port 80 over boxx_net.
	for _, slug := range sortedKeys(s.Singles) {
		app := s.Singles[slug]
		dial := containerName(slug, app.LiveColor) + ":80"
		sites = append(sites, site{app.Hostname, app.Aliases, []any{pathHandler("", dial)}})
	}

	// Groups: sub-routes per app path, longest-prefix first.
	for _, gslug := range sortedKeys(s.Groups) {
		g := s.Groups[gslug]
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
		sites = append(sites, site{g.Hostname, g.Aliases, handlers})
	}

	routes := []any{}
	wildRoutes := map[string]map[string]any{}
	claimed := map[string]bool{}
	localHosts := []string{}
	onDemand := []string{}
	for _, st := range sites {
		if st.primary == "" {
			continue
		}
		var served, redirects []string
		all := append([]state.Host{{Name: st.primary}}, st.aliases...)
		for _, h := range all {
			if claimed[h.Name] {
				continue
			}
			claimed[h.Name] = true
			switch {
			case IsLocalHostname(h.Name):
				localHosts = append(localHosts, h.Name)
			case hostnames.IsWildcard(h.Name):
				onDemand = append(onDemand, h.Name)
			}
			handlers := st.handlers
			if h.Redirect {
				handlers = []any{redirectHandler(st.primary)}
			}
			switch {
			case hostnames.IsWildcard(h.Name):
				wildRoutes[h.Name] = hostRoute([]string{h.Name}, handlers)
			case h.Redirect:
				redirects = append(redirects, h.Name)
			default:
				served = append(served, h.Name)
			}
		}
		if len(served) > 0 {
			routes = append(routes, hostRoute(served, st.handlers))
		}
		if len(redirects) > 0 {
			routes = append(routes, hostRoute(redirects, []any{redirectHandler(st.primary)}))
		}
	}

	// Wildcards after every exact host; more specific (more labels) first.
	wilds := sortedKeys(wildRoutes)
	sort.SliceStable(wilds, func(i, j int) bool {
		return strings.Count(wilds[i], ".") > strings.Count(wilds[j], ".")
	})
	for _, w := range wilds {
		routes = append(routes, wildRoutes[w])
	}

	// Skip ACME for local-only hostnames (.localhost, .local, .test, IPs, …).
	// Caddy will still serve them over HTTP on :80; HTTPS on :443 falls back
	// to its internal self-signed CA automatically for those names.
	autoHTTPS := map[string]any{"disable": false}
	if len(localHosts) > 0 {
		autoHTTPS["skip_certificates"] = localHosts
	}

	servers := map[string]any{
		"srv0": map[string]any{
			"listen":          []string{":443"},
			"automatic_https": autoHTTPS,
			"routes":          routes,
			"logs": map[string]any{
				"default_logger_name": "access",
			},
		},
	}
	apps := map[string]any{
		"http": map[string]any{"servers": servers},
	}
	if len(onDemand) > 0 {
		servers["ask"] = askServer(onDemand)
		apps["tls"] = onDemandTLS(onDemand)
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
		"apps": apps,
	}
}

// onDemandTLS issues certificates for wildcard hostnames one subdomain at a
// time, on the first TLS handshake, after the ask server approves the name.
// This avoids wildcard certificates, which need a DNS-provider plugin.
func onDemandTLS(wildcards []string) map[string]any {
	return map[string]any{
		"automation": map[string]any{
			"on_demand": map[string]any{
				"permission": map[string]any{
					"module":   "http",
					"endpoint": "http://" + AskAddr + "/ask",
				},
			},
			"policies": []any{
				map[string]any{"subjects": wildcards, "on_demand": true},
			},
		},
	}
}

// askServer answers Caddy's on-demand permission check: 200 when ?domain=
// is exactly one label under a configured wildcard, 403 otherwise.
func askServer(wildcards []string) map[string]any {
	routes := []any{}
	for _, w := range wildcards {
		re := `^[a-z0-9-]+` + regexp.QuoteMeta(hostnames.WildcardSuffix(w)) + `$`
		routes = append(routes, map[string]any{
			"match": []any{map[string]any{
				"vars_regexp": map[string]any{
					"{http.request.uri.query.domain}": map[string]any{"pattern": re},
				},
			}},
			"handle":   []any{map[string]any{"handler": "static_response", "status_code": 200}},
			"terminal": true,
		})
	}
	routes = append(routes, map[string]any{
		"handle": []any{map[string]any{"handler": "static_response", "status_code": 403}},
	})
	return map[string]any{
		"listen":          []string{AskAddr},
		"automatic_https": map[string]any{"disable": true},
		"routes":          routes,
	}
}

// redirectHandler 308s to the same path on the primary hostname.
func redirectHandler(primary string) map[string]any {
	return map[string]any{
		"handle": []any{
			map[string]any{
				"handler":     "static_response",
				"status_code": 308,
				"headers": map[string]any{
					"Location": []string{"{http.request.scheme}://" + primary + "{http.request.uri}"},
				},
			},
		},
	}
}

// hostRoute wraps a list of handlers in a host matcher.
func hostRoute(hosts []string, handlers []any) map[string]any {
	return map[string]any{
		"match": []any{
			map[string]any{"host": hosts},
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
						"listen": []string{":443"},
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
