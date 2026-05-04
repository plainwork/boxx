package tui

import "github.com/charmbracelet/lipgloss"

// AdaptiveColor lets the TUI look right on both light and dark terminals.
var (
	colFg     = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e6e6e6"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	colDim    = lipgloss.AdaptiveColor{Light: "#d1d5db", Dark: "#374151"}
	colBorder = lipgloss.AdaptiveColor{Light: "#9ca3af", Dark: "#4b5563"}
	colPanel  = lipgloss.AdaptiveColor{Light: "#f3f4f6", Dark: "#111827"}
	colAccent = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#60a5fa"}
	colOK     = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#34d399"}
	colBad    = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#fbbf24"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#1d4ed8"}
	colSelFg  = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(colAccent).
			Padding(0, 2)

	subtitleStyle = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	panelStyleFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colAccent).
				Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colAccent).
			MarginBottom(1)

	footerStyle = lipgloss.NewStyle().
			Foreground(colMuted).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colFg).
			Background(colPanel).
			Padding(0, 1)

	okStyle    = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	badStyle   = lipgloss.NewStyle().Foreground(colBad).Bold(true)
	warnStyle  = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	keyStyle   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	bodyStyle  = lipgloss.NewStyle().Foreground(colFg)

	listItemStyle = lipgloss.NewStyle().Padding(0, 1)
	listSelStyle  = lipgloss.NewStyle().
			Foreground(colSelFg).
			Background(colSelBg).
			Bold(true).
			Padding(0, 1)

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	inputBoxFocused = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colAccent).
			Padding(0, 1)

	labelStyle = lipgloss.NewStyle().Foreground(colMuted).Bold(true)

	btnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(colAccent).
			Padding(0, 2).
			Bold(true)
)

func keyHelp(items ...[2]string) string {
	out := ""
	for i, kv := range items {
		if i > 0 {
			out += mutedStyle.Render("   ")
		}
		out += keyStyle.Render(kv[0]) + mutedStyle.Render(" "+kv[1])
	}
	return out
}
