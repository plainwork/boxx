package caddy

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/plainwork/boxx/engine/state"
)

// route is the subset of a Caddy route the tests look at.
type route struct {
	Match []struct {
		Host []string `json:"host"`
	} `json:"match"`
	Handle []struct {
		Handler string `json:"handler"`
		Routes  []struct {
			Handle []struct {
				Handler    string              `json:"handler"`
				StatusCode int                 `json:"status_code"`
				Headers    map[string][]string `json:"headers"`
				Upstreams  []struct {
					Dial string `json:"dial"`
				} `json:"upstreams"`
			} `json:"handle"`
		} `json:"routes"`
	} `json:"handle"`
}

type config struct {
	Apps struct {
		HTTP struct {
			Servers map[string]struct {
				Listen         []string       `json:"listen"`
				AutomaticHTTPS map[string]any `json:"automatic_https"`
				Routes         []route        `json:"routes"`
			} `json:"servers"`
		} `json:"http"`
		TLS *struct {
			Automation struct {
				Policies []struct {
					Subjects []string `json:"subjects"`
					OnDemand bool     `json:"on_demand"`
				} `json:"policies"`
			} `json:"automation"`
		} `json:"tls"`
	} `json:"apps"`
}

func build(t *testing.T, s *state.State) config {
	t.Helper()
	if s.Singles == nil {
		s.Singles = map[string]state.Single{}
	}
	if s.Groups == nil {
		s.Groups = map[string]state.Group{}
	}
	b, err := json.Marshal(BuildConfig(s))
	if err != nil {
		t.Fatal(err)
	}
	var c config
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func (r route) hosts() []string { return r.Match[0].Host }

// target describes where a route sends traffic: "dial:<upstream>" or
// "308:<location>".
func (r route) target() string {
	for _, h := range r.Handle[0].Routes[0].Handle {
		switch h.Handler {
		case "static_response":
			return "308:" + h.Headers["Location"][0]
		case "reverse_proxy":
			return "dial:" + h.Upstreams[0].Dial
		}
	}
	return ""
}

func summarize(c config) [][2]any {
	var out [][2]any
	for _, r := range c.Apps.HTTP.Servers["srv0"].Routes {
		out = append(out, [2]any{r.hosts(), r.target()})
	}
	return out
}

func TestBuildConfigLegacyState(t *testing.T) {
	c := build(t, &state.State{
		Singles: map[string]state.Single{
			"b": {Hostname: "b.example.com", LiveColor: "green"},
			"a": {Hostname: "a.example.com"},
		},
		Groups: map[string]state.Group{
			"g": {Hostname: "g.example.com", Apps: map[string]state.GroupApp{
				"web":   {Path: "/", LiveColor: "blue"},
				"admin": {Path: "/admin", LiveColor: "blue"},
			}},
		},
	})
	want := [][2]any{
		{[]string{"a.example.com"}, "dial:boxx-app-a-blue:80"},
		{[]string{"b.example.com"}, "dial:boxx-app-b-green:80"},
		{[]string{"g.example.com"}, "dial:boxx-app-g-admin-blue:80"},
	}
	if got := summarize(c); !reflect.DeepEqual(got, want) {
		t.Errorf("routes = %v\nwant %v", got, want)
	}
	if c.Apps.TLS != nil || len(c.Apps.HTTP.Servers) != 1 {
		t.Errorf("legacy state should produce only srv0 and no tls app")
	}
}

func TestBuildConfigAliases(t *testing.T) {
	c := build(t, &state.State{
		Singles: map[string]state.Single{
			"saas": {Hostname: "example.com", Aliases: []state.Host{
				{Name: "*.example.com"},
				{Name: "www.example.com", Redirect: true},
				{Name: "app.example.net"},
			}},
			"api":  {Hostname: "api.example.com"},
			"deep": {Hostname: "deep.example.org", Aliases: []state.Host{{Name: "*.x.example.com"}}},
		},
	})
	want := [][2]any{
		{[]string{"api.example.com"}, "dial:boxx-app-api-blue:80"},
		{[]string{"deep.example.org"}, "dial:boxx-app-deep-blue:80"},
		{[]string{"example.com", "app.example.net"}, "dial:boxx-app-saas-blue:80"},
		{[]string{"www.example.com"}, "308:{http.request.scheme}://example.com{http.request.uri}"},
		// wildcards last, more specific first
		{[]string{"*.x.example.com"}, "dial:boxx-app-deep-blue:80"},
		{[]string{"*.example.com"}, "dial:boxx-app-saas-blue:80"},
	}
	if got := summarize(c); !reflect.DeepEqual(got, want) {
		t.Errorf("routes = %v\nwant %v", got, want)
	}

	if c.Apps.TLS == nil {
		t.Fatal("expected tls app for public wildcards")
	}
	p := c.Apps.TLS.Automation.Policies
	if len(p) != 1 || !p[0].OnDemand || !reflect.DeepEqual(p[0].Subjects, []string{"*.x.example.com", "*.example.com"}) &&
		!reflect.DeepEqual(p[0].Subjects, []string{"*.example.com", "*.x.example.com"}) {
		t.Errorf("policies = %+v", p)
	}
	ask, ok := c.Apps.HTTP.Servers["ask"]
	if !ok || ask.Listen[0] != AskAddr || len(ask.Routes) != 3 {
		t.Errorf("ask server = %+v", ask)
	}
}

func TestBuildConfigLocalWildcardSkipsOnDemand(t *testing.T) {
	c := build(t, &state.State{
		Singles: map[string]state.Single{
			"dev": {Hostname: "app.localhost", Aliases: []state.Host{{Name: "*.app.localhost"}}},
		},
	})
	if c.Apps.TLS != nil {
		t.Error("local wildcards must not enable on-demand TLS")
	}
	skip := c.Apps.HTTP.Servers["srv0"].AutomaticHTTPS["skip_certificates"]
	if !reflect.DeepEqual(skip, []any{"app.localhost", "*.app.localhost"}) {
		t.Errorf("skip_certificates = %v", skip)
	}
}

func TestBuildConfigDuplicateHostGoesToFirstApp(t *testing.T) {
	c := build(t, &state.State{
		Singles: map[string]state.Single{
			"a": {Hostname: "shared.example.com"},
			"b": {Hostname: "b.example.com", Aliases: []state.Host{{Name: "shared.example.com"}}},
		},
		Groups: map[string]state.Group{
			"g": {Hostname: "shared.example.com", Apps: map[string]state.GroupApp{"web": {Path: "/"}}},
		},
	})
	want := [][2]any{
		{[]string{"shared.example.com"}, "dial:boxx-app-a-blue:80"},
		{[]string{"b.example.com"}, "dial:boxx-app-b-blue:80"},
	}
	if got := summarize(c); !reflect.DeepEqual(got, want) {
		t.Errorf("routes = %v\nwant %v", got, want)
	}
}
