package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// titleBar renders a full-width title line: ─────── boxx [v0.1.6] ─────────
func titleBar(title, version string, width int) string {
	t := " " + title + " [" + version + "] "
	tw := lipgloss.Width(t)
	dashes := width - tw
	if dashes < 0 {
		dashes = 0
	}
	left := dashes / 2
	right := dashes - left
	bar := strings.Repeat("─", left) + t + strings.Repeat("─", right)
	return mutedStyle.Render(bar)
}

// keyBar renders the bottom hints line, with optional flash message.
func keyBar(width int, flash string, showActions bool, showInspect bool, inspecting bool, showDetails bool, showAppActions bool) string {
	kv := func(k, v string) string {
		return keyStyle.Render(k) + " " + mutedStyle.Render(v)
	}
	sep := mutedStyle.Render("  ·  ")
	parts := []string{
		kv("↑/k", "up"),
		kv("↓/j", "down"),
		kv("n", "new"),
		kv("L", "ops log"),
		kv("q", "quit"),
	}
	if showAppActions {
		parts = append(parts, kv("a", "actions"))
	}
	if showActions {
		parts = append(parts, kv("a", "db actions"))
	}
	if showInspect {
		parts = append(parts, kv("i", "inspect"))
	}
	if inspecting {
		parts = append(parts, kv("esc", "stats"))
	}
	if showDetails {
		parts = append(parts, kv("d", "details"))
	}
	hints := strings.Join(parts, sep)
	line := hints
	if flash != "" {
		line = flash + "   " + mutedStyle.Render("·") + "   " + hints
	}
	return centerLine(line, width)
}

// centerLine centers a (possibly multi-line) string within a given width.
func centerLine(s string, width int) string {
	sw := lipgloss.Width(s)
	if sw >= width {
		return s
	}
	left := (width - sw) / 2
	if left <= 0 {
		return s
	}
	pad := strings.Repeat(" ", left)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}


