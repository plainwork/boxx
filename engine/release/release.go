// Package release handles self-update checks for boxx.
//
// The latest GitHub release tag is fetched from the GitHub API and cached
// locally at $BOXX_HOME/.boxx-update-check (TTL 6 h). All network I/O is
// done in background goroutines so callers are never blocked.
package release

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/state"
)

const (
	repo     = "plainwork/boxx"
	cacheTTL = 6 * time.Hour
)

type cacheEntry struct {
	Tag       string    `json:"tag"`
	CheckedAt time.Time `json:"checked_at"`
}

func cacheFile() string {
	return filepath.Join(state.Root(), ".boxx-update-check")
}

// Cached returns the latest known tag from the local cache without network I/O.
// Returns "" if the cache is absent or older than cacheTTL.
func Cached() string {
	data, err := os.ReadFile(cacheFile())
	if err != nil {
		return ""
	}
	var c cacheEntry
	if json.Unmarshal(data, &c) != nil || time.Since(c.CheckedAt) > cacheTTL {
		return ""
	}
	return c.Tag
}

// RefreshAsync fetches the latest GitHub release tag in a background goroutine
// and writes it to the local cache. Errors are silently discarded.
func RefreshAsync() {
	go func() {
		tag, err := FetchLatest()
		if err != nil {
			return
		}
		data, _ := json.Marshal(cacheEntry{Tag: tag, CheckedAt: time.Now().UTC()})
		_ = os.WriteFile(cacheFile(), data, 0o644)
	}()
}

// FetchLatest fetches the latest release tag from the GitHub API (blocking).
func FetchLatest() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.TagName, nil
}

// IsNewer returns true when latest is a higher semver tag than current.
// If current is "dev" (local build), any tagged release is considered newer.
func IsNewer(latest, current string) bool {
	if latest == "" {
		return false
	}
	if current == "dev" || current == "" {
		return false
	}
	l := strings.TrimPrefix(latest, "v")
	c := strings.TrimPrefix(current, "v")
	return l != c && semverGT(l, c)
}

// semverGT returns true if a > b using simple numeric segment comparison.
func semverGT(a, b string) bool {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		ai, bi := 0, 0
		if i < len(aParts) {
			ai, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bi, _ = strconv.Atoi(bParts[i])
		}
		if ai > bi {
			return true
		}
		if ai < bi {
			return false
		}
	}
	return false
}
