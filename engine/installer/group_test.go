package installer

import (
	"strings"
	"testing"

	"github.com/plainwork/boxx/engine/state"
)

func TestPrepareGroupApps(t *testing.T) {
	existing := map[string]state.GroupApp{
		"site":  {Slug: "site", Path: "/"},
		"admin": {Slug: "admin", Path: "/admin"},
	}
	cases := []struct {
		name    string
		apps    []GroupApp
		wantErr string
		slugs   []string
	}{
		{"new path", []GroupApp{{Image: "ghcr.io/acme/docs:latest", Path: "/docs"}}, "", []string{"docs"}},
		{"two new paths", []GroupApp{{Image: "a", Path: "/docs"}, {Image: "b", Path: "/blog/"}}, "", []string{"docs", "blog"}},
		{"root taken", []GroupApp{{Image: "ghcr.io/acme/other:latest", Path: "/"}}, "path / is already served by \"site\"", nil},
		{"path taken with slash", []GroupApp{{Image: "x", Path: "/admin/", Slug: "admin2"}}, "path /admin is already served by \"admin\"", nil},
		{"slug taken", []GroupApp{{Image: "x", Path: "/v2", Slug: "admin"}}, "duplicate app slug \"admin\"", nil},
		{"same path twice", []GroupApp{{Image: "a", Path: "/docs"}, {Image: "b", Path: "/docs", Slug: "docs2"}}, "path /docs is already served by \"docs\"", nil},
		{"no slash", []GroupApp{{Image: "x", Path: "docs"}}, "path must start with /", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := prepareGroupApps(c.apps, existing)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for i, s := range c.slugs {
				if c.apps[i].Slug != s {
					t.Errorf("apps[%d].Slug = %q, want %q", i, c.apps[i].Slug, s)
				}
			}
		})
	}
}

func TestPrepareGroupAppsFreshGroup(t *testing.T) {
	apps := []GroupApp{{Image: "ghcr.io/acme/site:latest", Path: "/"}, {Image: "ghcr.io/acme/admin:latest", Path: "/admin"}}
	if err := prepareGroupApps(apps, nil); err != nil {
		t.Fatal(err)
	}
	if apps[0].Slug != "site" || apps[1].Slug != "admin" {
		t.Fatalf("slugs = %q, %q", apps[0].Slug, apps[1].Slug)
	}
}
