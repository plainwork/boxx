package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/plainwork/boxx/engine/opslog"
)

// renderLogsScreen renders log lines into the content area.
// scroll=0 means pinned to newest (bottom); scroll>0 = lines scrolled up.
func renderLogsScreen(lines []string, slug string, scroll, width, height int) string {
	total := len(lines)

	// compute the visible window
	end := total - scroll
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}

	var sb strings.Builder
	shown := end - start

	// pad the top with blank lines when we don't have a full screen yet
	for i := 0; i < height-shown; i++ {
		sb.WriteString("\n")
	}

	for i := start; i < end; i++ {
		line := lines[i]
		// truncate to terminal width (respects rune width, not ANSI codes — good enough for log lines)
		runes := []rune(line)
		if len(runes) > width {
			line = string(runes[:width])
		}
		sb.WriteString(line)
		if i < end-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// logsKeyBar renders the bottom hint line for the logs screen.
func logsKeyBar(width int, scroll int) string {
	kv := func(k, v string) string {
		return keyStyle.Render(k) + " " + mutedStyle.Render(v)
	}
	sep := mutedStyle.Render("  ·  ")

	var liveIndicator string
	if scroll == 0 {
		liveIndicator = okStyle.Render("● live")
	} else {
		liveIndicator = mutedStyle.Render("↓ scroll for live")
	}

	parts := []string{
		liveIndicator,
		kv("↑/k", "older"),
		kv("↓/j", "newer"),
		kv("G", "jump to end"),
		kv("esc", "exit"),
	}
	hints := strings.Join(parts, sep)
	return centerLine(hints, width)
}

// ---- boxx ops log streaming -------------------------------------------------

type opsLogLineMsg string
type opsLogDoneMsg struct{ err error }
type opsLogReadyMsg struct {
	initial []string
	reader  *bufio.Reader
	cmd     *exec.Cmd
}

// startOpsLog returns a tea.Cmd that:
//  1. Reads existing events from boxx.log via opslog.Tail.
//  2. Starts `tail -f` on the same file to stream new events.
//  3. Returns an opsLogReadyMsg carrying the initial lines + the live reader.
func startOpsLog() tea.Cmd {
	return func() tea.Msg {
		// Load history.
		events, _ := opslog.Tail(500)
		initial := make([]string, 0, len(events))
		for _, e := range events {
			initial = append(initial, formatOpsEvent(e))
		}

		// Start tail -f to stream new lines.
		logPath := opslog.LogFile()
		cmd := exec.Command("tail", "-f", "-n", "0", logPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			// No live stream available; return static view only.
			return opsLogReadyMsg{initial: initial}
		}
		return opsLogReadyMsg{
			initial: initial,
			reader:  bufio.NewReader(stdout),
			cmd:     cmd,
		}
	}
}

// readOpsLogLine reads one JSONL line from r, parses it as an opslog.Event,
// and returns a formatted opsLogLineMsg (or opsLogDoneMsg on error/EOF).
func readOpsLogLine(r *bufio.Reader) tea.Cmd {
	if r == nil {
		return nil
	}
	return func() tea.Msg {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\n\r")
		if line != "" {
			var e opslog.Event
			if json.Unmarshal([]byte(line), &e) == nil {
				return opsLogLineMsg(formatOpsEvent(e))
			}
			return opsLogLineMsg(line)
		}
		if err != nil {
			return opsLogDoneMsg{err: err}
		}
		return opsLogLineMsg(line)
	}
}

// formatOpsEvent formats an opslog.Event as a fixed-width display line.
func formatOpsEvent(e opslog.Event) string {
	ts := e.Time.Format("2006-01-02 15:04:05")
	slug := e.Slug
	if e.AppSlug != "" {
		slug += "/" + e.AppSlug
	}
	status := e.Status
	if e.Error != "" {
		status = badStyle.Render("error: " + e.Error)
	} else if e.Status == "ok" {
		status = okStyle.Render("ok")
	}
	dur := ""
	if e.DurationMS > 0 {
		dur = mutedStyle.Render(fmt.Sprintf(" (%dms)", e.DurationMS))
	}
	return fmt.Sprintf("%s  %-14s  %-28s  %s%s",
		mutedStyle.Render(ts),
		lipglossOp(e.Op),
		slug,
		status,
		dur,
	)
}

func lipglossOp(op string) string {
	switch op {
	case "install":
		return okStyle.Render(fmt.Sprintf("%-14s", op))
	case "deploy", "update_deploy":
		return lipglossAccent(fmt.Sprintf("%-14s", op))
	case "remove":
		return badStyle.Render(fmt.Sprintf("%-14s", op))
	default:
		return mutedStyle.Render(fmt.Sprintf("%-14s", op))
	}
}

func lipglossAccent(s string) string {
	return keyStyle.Render(s)
}
