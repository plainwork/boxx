// Package state owns the on-disk state for boxx.
//
// Layout:
//
//	/var/lib/boxx/state.json   — installed apps/groups/proxy info
//	/var/lib/boxx/caddy/data   — Caddy ACME storage (mounted into proxy container)
//	/var/lib/boxx/caddy/config — Caddy persisted config (mounted into proxy container)
//	/etc/boxx/                 — reserved for future operator config
//
// On macOS (where /var/lib is not writable for non-root), we fall back to
// ~/Library/Application Support/boxx so local development works without sudo.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
)

const Version = 1

// State is the full persisted state document.
type State struct {
	Version    int                `json:"version"`
	Proxy      Proxy              `json:"proxy"`
	Singles    map[string]Single  `json:"singles"`
	Groups     map[string]Group   `json:"groups"`
	Registries []string           `json:"registries"`
}

type Proxy struct {
	Running bool   `json:"running"`
	Image   string `json:"image"`
}

// DB describes a database container provisioned by boxx.
type DB struct {
	Engine        string `json:"engine"`         // "mysql" | "postgres"
	Version       string `json:"version"`        // image tag, e.g. "8"
	Container     string `json:"container"`      // boxx-db-<slug>
	Database      string `json:"database"`       // logical db name (== app slug, sanitized)
	Username      string `json:"username"`
	Password      string `json:"password"`       // generated, used to build DATABASE_URL
	RootPassword  string `json:"root_password"`  // stored for admin ops (mysql root / postgres superuser)
}

// Single is an app installed on its own hostname with an optional dedicated DB.
type Single struct {
	Slug      string            `json:"slug"`
	Image     string            `json:"image"`
	Hostname  string            `json:"hostname"`
	LiveColor string            `json:"live_color"` // "blue" | "green"
	DB        *DB               `json:"db,omitempty"`
	Registry  string            `json:"registry,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// Group is a set of apps that share one hostname (and optionally one DB).
type Group struct {
	Slug     string                 `json:"slug"`
	Hostname string                 `json:"hostname"`
	DB       *DB                    `json:"db,omitempty"`
	Apps     map[string]GroupApp    `json:"apps"`
}

type GroupApp struct {
	Slug      string            `json:"slug"`
	Image     string            `json:"image"`
	Path      string            `json:"path"`       // e.g. "/", "/admin"
	LiveColor string            `json:"live_color"` // "blue" | "green"
	Registry  string            `json:"registry,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// ---------- paths ----------

// Root returns the base directory for boxx state.
func Root() string {
	if v := os.Getenv("BOXX_HOME"); v != "" {
		return v
	}
	if runtime.GOOS == "darwin" {
		// On macOS we default to user-scoped storage; the daemon-managed
		// Linux path requires root and isn't usable for local dev.
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "boxx")
		}
	}
	return "/var/lib/boxx"
}

// StateFile is the path to the JSON state document.
func StateFile() string { return filepath.Join(Root(), "state.json") }

// CaddyDataDir is mounted into the proxy container as ACME storage.
func CaddyDataDir() string { return filepath.Join(Root(), "caddy", "data") }

// CaddyConfigDir is mounted into the proxy container for persisted config.
func CaddyConfigDir() string { return filepath.Join(Root(), "caddy", "config") }

// EnsureDirs creates all required directories with safe permissions.
func EnsureDirs() error {
	for _, d := range []string{Root(), CaddyDataDir(), CaddyConfigDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// ---------- load / save ----------

var saveMu sync.Mutex

// Load reads the state file. A missing file returns a zero-valued, ready-to-use State.
func Load() (*State, error) {
	if err := EnsureDirs(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(StateFile())
	if errors.Is(err, os.ErrNotExist) {
		return newDefault(), nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse state.json: %w", err)
	}
	if s.Version == 0 {
		s.Version = Version
	}
	if s.Singles == nil {
		s.Singles = map[string]Single{}
	}
	if s.Groups == nil {
		s.Groups = map[string]Group{}
	}
	return &s, nil
}

func newDefault() *State {
	return &State{
		Version: Version,
		Proxy:   Proxy{Running: false, Image: "caddy:2"},
		Singles: map[string]Single{},
		Groups:  map[string]Group{},
	}
}

// Save atomically writes the state file under an exclusive flock.
func Save(s *State) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	if err := EnsureDirs(); err != nil {
		return err
	}
	lockPath := filepath.Join(Root(), ".state.lock")
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := StateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, StateFile())
}
