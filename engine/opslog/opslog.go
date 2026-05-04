// Package opslog provides an append-only JSONL operational log for boxx.
//
// The log lives at $BOXX_HOME/boxx.log (typically /var/lib/boxx/boxx.log).
// Each line is one JSON-encoded Event. Secret values are never written here;
// for env changes only key names and counts are logged.
package opslog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/plainwork/boxx/engine/state"
)

// Event is one entry in the boxx operational log.
type Event struct {
	Time       time.Time `json:"time"`
	Op         string    `json:"op"`                    // install|deploy|restart|remove|env_push|env_import|env_rollback|update_check|update_deploy|timer|settings
	Slug       string    `json:"slug,omitempty"`
	AppSlug    string    `json:"app_slug,omitempty"`    // for group apps
	Status     string    `json:"status"`                // started|ok|error
	Message    string    `json:"message,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// LogFile returns the path to the boxx operational log.
func LogFile() string {
	return filepath.Join(state.Root(), "boxx.log")
}

// Append writes a single event to the log. Time is set to now if zero.
// Errors here are non-fatal — operational logging must not break the caller.
func Append(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	f, err := os.OpenFile(LogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return // silently skip if log cannot be written
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	fmt.Fprintf(f, "%s\n", b)
}

// Tail returns the last n events from the log (all events when n <= 0).
func Tail(n int) ([]Event, error) {
	f, err := os.Open(LogFile())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opslog: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		var e Event
		if json.Unmarshal([]byte(line), &e) == nil {
			events = append(events, e)
		}
	}
	return events, nil
}

// Op is a convenience wrapper for logging a completed operation with timing.
// Call it with defer: defer opslog.Op("deploy", "myapp", "", start, &err)
func Op(op, slug, appSlug string, start time.Time, errp *error) {
	e := Event{
		Time:       time.Now().UTC(),
		Op:         op,
		Slug:       slug,
		AppSlug:    appSlug,
		DurationMS: time.Since(start).Milliseconds(),
	}
	if errp != nil && *errp != nil {
		e.Status = "error"
		e.Error = (*errp).Error()
	} else {
		e.Status = "ok"
	}
	Append(e)
}
