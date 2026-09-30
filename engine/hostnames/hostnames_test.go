package hostnames

import (
	"strings"
	"testing"

	"github.com/plainwork/boxx/engine/state"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in, want, err string
	}{
		{in: "Example.COM", want: "example.com"},
		{in: "  www.example.com.  ", want: "www.example.com"},
		{in: "*.example.com", want: "*.example.com"},
		{in: "*.a.example.com", want: "*.a.example.com"},
		{in: "xn--bcher-kva.example", want: "xn--bcher-kva.example"},
		{in: "a.localhost", want: "a.localhost"},
		{in: "", err: "empty"},
		{in: "https://example.com", err: "http"},
		{in: "example.com/app", err: "path"},
		{in: "example.com:8080", err: "port"},
		{in: "bücher.example", err: "punycode"},
		{in: "*.com", err: "two labels"},
		{in: "a.*.example.com", err: "first label"},
		{in: "*foo.example.com", err: "first label"},
		{in: "-a.example.com", err: "hyphen"},
		{in: "a..example.com", err: "empty label"},
		{in: "a_b.example.com", err: "only letters"},
		{in: strings.Repeat("a", 64) + ".com", err: "63"},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, host string
		want          bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "www.example.com", false},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "a.b.example.com", false},
		{"*.example.com", "a.example.com.evil.com", false},
		{"*.example.com", "*.example.com", false},
		{"*.a.example.com", "x.a.example.com", true},
	}
	for _, tt := range tests {
		if got := Match(tt.pattern, tt.host); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.host, got, tt.want)
		}
	}
}

func testState() *state.State {
	return &state.State{
		Singles: map[string]state.Single{
			"blog": {Hostname: "blog.example.com", Aliases: []state.Host{{Name: "www.blog.example.com", Redirect: true}}},
			"saas": {Hostname: "example.com", Aliases: []state.Host{{Name: "*.example.com"}}},
		},
		Groups: map[string]state.Group{
			"site": {Hostname: "site.example.org"},
		},
	}
}

func TestCheckAvailable(t *testing.T) {
	s := testState()
	tests := []struct {
		name  string
		hosts []string
		self  Owner
		err   string
	}{
		{"free", []string{"new.example.net"}, Owner{Slug: "new"}, ""},
		{"exact taken", []string{"blog.example.com"}, Owner{Slug: "new"}, "already used by app 'blog'"},
		{"alias taken", []string{"www.blog.example.com"}, Owner{Slug: "new"}, "app 'blog'"},
		{"group taken", []string{"site.example.org"}, Owner{Slug: "new"}, "group 'site'"},
		{"same wildcard taken", []string{"*.example.com"}, Owner{Slug: "new"}, "app 'saas'"},
		{"exact under other wildcard ok", []string{"api.example.com"}, Owner{Slug: "new"}, ""},
		{"own hosts ok", []string{"blog.example.com", "www.blog.example.com"}, Owner{Slug: "blog"}, ""},
		{"group vs single same slug", []string{"site.example.org"}, Owner{Slug: "site"}, "group 'site'"},
		{"listed twice", []string{"a.example.net", "a.example.net"}, Owner{Slug: "new"}, "more than once"},
	}
	for _, tt := range tests {
		err := CheckAvailable(s, tt.hosts, tt.self)
		if tt.err == "" && err != nil {
			t.Errorf("%s: unexpected error %v", tt.name, err)
		}
		if tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("%s: error = %v, want containing %q", tt.name, err, tt.err)
		}
	}
}

func TestOverlapsAndDuplicates(t *testing.T) {
	s := testState()
	if notes := Overlaps(s, []string{"api.example.com"}, Owner{Slug: "new"}); len(notes) != 1 || !strings.Contains(notes[0], "*.example.com") {
		t.Errorf("Overlaps exact-under-wildcard = %v", notes)
	}
	if notes := Overlaps(s, []string{"*.blog.example.com"}, Owner{Slug: "new"}); len(notes) != 1 || !strings.Contains(notes[0], "www.blog.example.com") {
		t.Errorf("Overlaps wildcard-over-exact = %v", notes)
	}
	// saas's own example.com isn't reported; blog's blog.example.com is.
	if notes := Overlaps(s, []string{"*.example.com"}, Owner{Slug: "saas"}); len(notes) != 1 || !strings.Contains(notes[0], "blog.example.com on app 'blog'") {
		t.Errorf("Overlaps with own hosts = %v", notes)
	}

	if d := Duplicates(s); len(d) != 0 {
		t.Errorf("Duplicates = %v, want none", d)
	}
	s.Singles["copy"] = state.Single{Hostname: "blog.example.com"}
	d := Duplicates(s)
	if got := d["blog.example.com"]; len(got) != 2 || got[0].Slug != "blog" || got[1].Slug != "copy" {
		t.Errorf("Duplicates = %v", d)
	}
}
