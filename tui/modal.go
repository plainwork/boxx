package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// dbAction describes a single action available on a database item.
type dbAction struct {
	key   string // short key hint
	label string // display name
	desc  string // one-line description
}

// appAction describes a single action available on an application.
type appAction struct {
	key   string
	label string
	desc  string
}

var appActions = []appAction{
	{"deploy", "deploy", "Pull the latest image and do a rolling redeploy"},
	{"change-image", "change image", "Deploy with a different container image"},
	{"restart", "restart", "Restart the running container"},
	{"logs", "logs", "Open container log stream (exits with q)"},
	{"settings", "settings", "Configure update policy and other per-app settings"},
	{"env-config", "env / config", "View, edit, import or roll back environment variables"},
	{"remove", "remove", "Stop and remove the app and its resources"},
}

// renderAppActionModal renders a centred app-actions modal over the given content.
func renderAppActionModal(content string, cursor int, slug string, width, height int) string {
	const modalW = 52
	const padH = 2
	innerW := modalW - 2

	title := "─ " + slug + " "
	titleRunes := len([]rune(title))
	topFill := innerW - titleRunes
	if topFill < 0 {
		// truncate slug so title fits
		maxSlug := innerW - len([]rune("─  "))
		if maxSlug < 0 {
			maxSlug = 0
		}
		runes := []rune(slug)
		if len(runes) > maxSlug {
			runes = runes[:maxSlug]
		}
		title = "─ " + string(runes) + " "
		topFill = innerW - len([]rune(title))
		if topFill < 0 {
			topFill = 0
		}
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	var rows []string
	rows = append(rows, top)
	rows = append(rows, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))

	for i, a := range appActions {
		circle := lipgloss.NewStyle().Foreground(colAccent).Render("○")
		var nameStr string
		if i == cursor {
			circle = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			nameStr = lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(a.label)
		} else {
			nameStr = lipgloss.NewStyle().Foreground(colFg).Render(a.label)
		}
		line := "  " + circle + " " + nameStr
		padding := innerW - lipgloss.Width(line)
		if padding < 0 {
			padding = 0
		}
		rows = append(rows, mutedStyle.Render("│")+line+strings.Repeat(" ", padding)+mutedStyle.Render("│"))
	}

	rows = append(rows, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))
	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	desc := appActions[cursor].desc
	maxDesc := innerW - 2
	if len([]rune(desc)) > maxDesc {
		desc = string([]rune(desc)[:maxDesc])
	}
	descLine := "  " + mutedStyle.Render(desc)
	dpadding := innerW - lipgloss.Width(descLine)
	if dpadding < 0 {
		dpadding = 0
	}
	rows = append(rows, mutedStyle.Render("│")+descLine+strings.Repeat(" ", dpadding)+mutedStyle.Render("│"))

	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	hint := "  " + keyStyle.Render("esc") + mutedStyle.Render(" close") + "   " + keyStyle.Render("↵") + mutedStyle.Render(" run")
	hpadding := innerW - lipgloss.Width(hint)
	if hpadding < 0 {
		hpadding = 0
	}
	rows = append(rows, mutedStyle.Render("│")+hint+strings.Repeat(" ", hpadding)+mutedStyle.Render("│"))
	rows = append(rows, bot)

	modal := strings.Join(rows, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(" "),
	)
}


var dbActions = []dbAction{
	{"f", "forward", "Forward a local port to this database"},
	{"u", "unforward", "Stop the local port-forward"},
	{"r", "reset-user", "Re-grant database credentials for the app"},
	{"R", "recreate", "Recreate the database container (preserves data)"},
}

// renderActionModal renders a centred modal listing dbActions over the given content.
func renderActionModal(content string, cursor int, width, height int) string {
	const modalW = 52
	const padH = 2

	innerW := modalW - 2

	// header
	title := "─ db actions "
	topFill := innerW - len([]rune(title))
	if topFill < 0 {
		topFill = 0
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	var rows []string
	rows = append(rows, top)
	rows = append(rows, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))

	for i, a := range dbActions {
		circle := lipgloss.NewStyle().Foreground(colAccent).Render("○")
		var line string
		if i == cursor {
			circle = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			nameStr := lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(a.label)
			line = "  " + circle + " " + nameStr
		} else {
			nameStr := lipgloss.NewStyle().Foreground(colFg).Render(a.label)
			line = "  " + circle + " " + nameStr
		}
		padding := innerW - lipgloss.Width(line)
		if padding < 0 {
			padding = 0
		}
		rows = append(rows, mutedStyle.Render("│")+line+strings.Repeat(" ", padding)+mutedStyle.Render("│"))
	}
	rows = append(rows, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))

	// separator
	sepLine := "├" + strings.Repeat("─", innerW) + "┤"
	rows = append(rows, mutedStyle.Render(sepLine))

	// static description row — updates with selection
	desc := dbActions[cursor].desc
	maxDesc := innerW - 2
	if len([]rune(desc)) > maxDesc {
		desc = string([]rune(desc)[:maxDesc])
	}
	descLine := "  " + mutedStyle.Render(desc)
	dpadding := innerW - lipgloss.Width(descLine)
	if dpadding < 0 {
		dpadding = 0
	}
	rows = append(rows, mutedStyle.Render("│")+descLine+strings.Repeat(" ", dpadding)+mutedStyle.Render("│"))

	// separator before key hints
	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	// key hints row
	hint := "  " + keyStyle.Render("esc") + mutedStyle.Render(" close") + "   " + keyStyle.Render("↵") + mutedStyle.Render(" run")
	hpadding := innerW - lipgloss.Width(hint)
	if hpadding < 0 {
		hpadding = 0
	}
	rows = append(rows, mutedStyle.Render("│")+hint+strings.Repeat(" ", hpadding)+mutedStyle.Render("│"))
	rows = append(rows, bot)

	modal := strings.Join(rows, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// newAppOption describes a choice in the new-app type modal.
type newAppOption struct {
	label string
	desc  string
}

// renderAppImageModal renders a centred modal prompting for a new container image.
func renderAppImageModal(content string, ti textinput.Model, slug string, width, height int) string {
	const modalW = 52
	innerW := modalW - 2

	// Build title with slug, truncating if needed
	titlePrefix := "─ " + slug + " - change image "
	fill := innerW - lipgloss.Width(titlePrefix)
	if fill < 0 {
		maxSlug := innerW - lipgloss.Width("─  - change image ")
		if maxSlug < 0 {
			maxSlug = 0
		}
		runes := []rune(slug)
		if len(runes) > maxSlug {
			runes = runes[:maxSlug]
		}
		titlePrefix = "─ " + string(runes) + " - change image "
		fill = innerW - lipgloss.Width(titlePrefix)
		if fill < 0 {
			fill = 0
		}
	}
	top := mutedStyle.Render("╭" + titlePrefix + strings.Repeat("─", fill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")
	sep := mutedStyle.Render("├" + strings.Repeat("─", innerW) + "┤")

	// padTo pads s to exactly innerW visual columns
	padTo := func(s string) string {
		w := lipgloss.Width(s)
		if w < innerW {
			return s + strings.Repeat(" ", innerW-w)
		}
		return s
	}
	border := func(inner string) string {
		return mutedStyle.Render("│") + inner + mutedStyle.Render("│")
	}

	labelRow := padTo("  " + mutedStyle.Render("new image"))

	inputW := innerW - 4
	tiCopy := ti
	tiCopy.Width = inputW
	inputRow := padTo("  " + tiCopy.View())

	hint := "  " + keyStyle.Render("esc") + mutedStyle.Render(" back") + "   " + keyStyle.Render("↵") + mutedStyle.Render(" deploy")

	rows := []string{
		top,
		border(padTo("")),
		border(labelRow),
		border(inputRow),
		border(padTo("")),
		sep,
		border(padTo(hint)),
		bot,
	}

	modal := strings.Join(rows, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(" "),
	)
}

var newAppOptions = []newAppOption{
	{"single", "Install a standalone application on one host"},
	{"group", "Multiple apps on one host with path-based proxy"},
}

// renderNewAppModal renders a centred modal for selecting new app type over the given content.
func renderNewAppModal(content string, cursor int, width, height int) string {
	const modalW = 52
	const padH = 2

	innerW := modalW - 2

	title := "─ new application "
	topFill := innerW - len([]rune(title))
	if topFill < 0 {
		topFill = 0
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	var rows []string
	rows = append(rows, top)
	rows = append(rows, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))

	for i, o := range newAppOptions {
		circle := lipgloss.NewStyle().Foreground(colAccent).Render("○")
		var nameStr string
		if i == cursor {
			circle = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			nameStr = lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(o.label)
		} else {
			nameStr = lipgloss.NewStyle().Foreground(colFg).Render(o.label)
		}
		line := "  " + circle + " " + nameStr
		padding := innerW - lipgloss.Width(line)
		if padding < 0 {
			padding = 0
		}
		rows = append(rows, mutedStyle.Render("│")+line+strings.Repeat(" ", padding)+mutedStyle.Render("│"))
	}
	rows = append(rows, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))

	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	desc := newAppOptions[cursor].desc
	maxDesc := innerW - 2 // "  " prefix
	if len([]rune(desc)) > maxDesc {
		desc = string([]rune(desc)[:maxDesc])
	}
	descLine := "  " + mutedStyle.Render(desc)
	dpadding := innerW - lipgloss.Width(descLine)
	if dpadding < 0 {
		dpadding = 0
	}
	rows = append(rows, mutedStyle.Render("│")+descLine+strings.Repeat(" ", dpadding)+mutedStyle.Render("│"))

	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	hint := "  " + keyStyle.Render("esc") + mutedStyle.Render(" close") + "   " + keyStyle.Render("↵") + mutedStyle.Render(" select")
	hpadding := innerW - lipgloss.Width(hint)
	if hpadding < 0 {
		hpadding = 0
	}
	rows = append(rows, mutedStyle.Render("│")+hint+strings.Repeat(" ", hpadding)+mutedStyle.Render("│"))
	rows = append(rows, bot)

	modal := strings.Join(rows, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// ---- per-app Settings modal -------------------------------------------------

type settingsOption struct {
	mode  string
	label string
	desc  string
}

var settingsOptions = []settingsOption{
	{"off", "off", "Never check for updates"},
	{"notify", "notify", "Check and surface new versions; never auto-deploy"},
	{"auto", "auto", "Automatically deploy when a new image is available"},
}

func renderAppSettingsModal(content, slug string, cursor, width, height int) string {
	const modalW = 56
	innerW := modalW - 2

	title := "─ settings: " + slug + " "
	topFill := innerW - len([]rune(title))
	if topFill < 0 {
		topFill = 0
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	bfn := func(s string) string {
		pad := innerW - lipgloss.Width(s)
		if pad < 0 {
			pad = 0
		}
		return mutedStyle.Render("│") + s + strings.Repeat(" ", pad) + mutedStyle.Render("│")
	}

	var rows []string
	rows = append(rows, top, bfn(""), bfn("  "+mutedStyle.Render("Update policy")), bfn(""))

	for i, opt := range settingsOptions {
		dot := lipgloss.NewStyle().Foreground(colAccent).Render("○")
		label := lipgloss.NewStyle().Foreground(colFg).Render(opt.label)
		if i == cursor {
			dot = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			label = lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(opt.label)
		}
		rows = append(rows, bfn("  "+dot+" "+label))
	}

	rows = append(rows, bfn(""))
	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))
	desc := settingsOptions[cursor].desc
	rows = append(rows, bfn("  "+mutedStyle.Render(desc)))
	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	hint := "  " + keyStyle.Render("esc") + mutedStyle.Render(" back") + "   " + keyStyle.Render("↵") + mutedStyle.Render(" apply")
	hpad := innerW - lipgloss.Width(hint)
	if hpad < 0 {
		hpad = 0
	}
	rows = append(rows, mutedStyle.Render("│")+hint+strings.Repeat(" ", hpad)+mutedStyle.Render("│"), bot)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		strings.Join(rows, "\n"), lipgloss.WithWhitespaceChars(" "))
}

// ---- per-app Env / Config modal ---------------------------------------------

type envConfigOption struct {
	label string
	desc  string
}

var envConfigOptions = []envConfigOption{
	{"push (editor)", "Open $EDITOR to review and edit env vars"},
	{"import from file", "Replace env from a .env file (CLI only)"},
	{"rollback", "Restore the previous env snapshot and redeploy"},
}

func renderAppEnvConfigModal(content, slug string, cursor, width, height int) string {
	const modalW = 56
	innerW := modalW - 2

	title := "─ env / config: " + slug + " "
	topFill := innerW - len([]rune(title))
	if topFill < 0 {
		topFill = 0
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	bfn := func(s string) string {
		pad := innerW - lipgloss.Width(s)
		if pad < 0 {
			pad = 0
		}
		return mutedStyle.Render("│") + s + strings.Repeat(" ", pad) + mutedStyle.Render("│")
	}

	var rows []string
	rows = append(rows, top, bfn(""))

	for i, opt := range envConfigOptions {
		dot := lipgloss.NewStyle().Foreground(colAccent).Render("○")
		label := lipgloss.NewStyle().Foreground(colFg).Render(opt.label)
		if i == cursor {
			dot = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			label = lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(opt.label)
		}
		rows = append(rows, bfn("  "+dot+" "+label))
	}

	rows = append(rows, bfn(""))
	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))
	desc := envConfigOptions[cursor].desc
	rows = append(rows, bfn("  "+mutedStyle.Render(desc)))
	rows = append(rows, mutedStyle.Render("├"+strings.Repeat("─", innerW)+"┤"))

	hint := "  " + keyStyle.Render("esc") + mutedStyle.Render(" back") + "   " + keyStyle.Render("↵") + mutedStyle.Render(" select")
	hpad := innerW - lipgloss.Width(hint)
	if hpad < 0 {
		hpad = 0
	}
	rows = append(rows, mutedStyle.Render("│")+hint+strings.Repeat(" ", hpad)+mutedStyle.Render("│"), bot)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		strings.Join(rows, "\n"), lipgloss.WithWhitespaceChars(" "))
}
