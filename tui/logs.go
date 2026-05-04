package tui

import (
	"strings"
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
