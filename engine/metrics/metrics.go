// Package metrics reads Caddy's JSON access log and docker stats to produce
// per-app visit counts, request rates, and traffic rates for the TUI.
package metrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/plainwork/boxx/engine/state"
)

// AppMetrics holds the current metrics snapshot for one app.
type AppMetrics struct {
	Visits    int64   // total requests since process start
	ReqPerMin float64 // rolling 1-minute request rate
}

// Collector tails the Caddy access log and aggregates per-host request counts.
type Collector struct {
	mu       sync.RWMutex
	visits   map[string]int64
	window   map[string][]time.Time // rolling 60-second window
	lastRead int64                  // byte offset in log file
}

var global = &Collector{
	visits: make(map[string]int64),
	window: make(map[string][]time.Time),
}

// Poll reads any new lines from the Caddy access log and updates in-memory counters.
// Call this on every TUI tick (every 3 seconds).
func Poll() {
	logPath := filepath.Join(state.CaddyDataDir(), "logs", "access.log")
	f, err := os.Open(logPath)
	if err != nil {
		return // log doesn't exist yet — no traffic
	}
	defer f.Close()

	global.mu.Lock()
	defer global.mu.Unlock()

	if _, err := f.Seek(global.lastRead, io.SeekStart); err != nil {
		global.lastRead = 0
		_, _ = f.Seek(0, io.SeekStart)
	}

	now := time.Now()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry struct {
			Request struct {
				Host string `json:"host"`
			} `json:"request"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		host := entry.Request.Host
		if idx := strings.LastIndex(host, ":"); idx > 0 {
			host = host[:idx]
		}
		if host == "" {
			continue
		}
		global.visits[host]++
		global.window[host] = append(global.window[host], now)
	}

	global.lastRead, _ = f.Seek(0, io.SeekCurrent)

	// Trim window to last 60 seconds.
	cutoff := now.Add(-60 * time.Second)
	for h, ts := range global.window {
		i := 0
		for i < len(ts) && ts[i].Before(cutoff) {
			i++
		}
		global.window[h] = ts[i:]
	}
}

// Get returns the current metrics for a hostname.
func Get(hostname string) AppMetrics {
	global.mu.RLock()
	defer global.mu.RUnlock()
	return AppMetrics{
		Visits:    global.visits[hostname],
		ReqPerMin: float64(len(global.window[hostname])),
	}
}

// FmtBytes formats a byte count as a compact human-readable string.
func FmtBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

