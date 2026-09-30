package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/plainwork/boxx/engine/hostnames"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/state"
)

// hostsScreen is the per-app hostnames editor. Hostnames belong to a single
// app or to a whole group, so ref is a single slug or a group slug.
type hostsScreen struct {
	ref     string
	primary string
	aliases []state.Host
	cursor  int // 0 = primary, i = aliases[i-1]
	input   textinput.Model
	adding  bool
	busy    bool
	err     string
}

// hostsEditedMsg reports the result of an async hostname change.
type hostsEditedMsg struct{ err error }

func newHostsScreen(slugRef string) hostsScreen {
	ref, _, _ := strings.Cut(slugRef, "/")
	ti := textinput.New()
	ti.Placeholder = "www.example.com  or  *.example.com"
	ti.CharLimit = 253
	ti.Width = 44
	h := hostsScreen{ref: ref, input: ti}
	return h.reload()
}

func (h hostsScreen) reload() hostsScreen {
	primary, aliases, err := installer.Hosts(h.ref)
	if err != nil {
		h.err = err.Error()
		return h
	}
	h.primary, h.aliases = primary, aliases
	if h.cursor > len(h.aliases) {
		h.cursor = len(h.aliases)
	}
	return h
}

// selected returns the hostname under the cursor, or "" on the primary.
func (h hostsScreen) selected() (state.Host, bool) {
	if h.cursor == 0 || h.cursor > len(h.aliases) {
		return state.Host{}, false
	}
	return h.aliases[h.cursor-1], true
}

func hostsEditCmd(edit func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return hostsEditedMsg{err: edit(ctx)}
	}
}

// updateHosts handles input on the hostnames screen.
func (m model) updateHosts(msg tea.Msg) (tea.Model, tea.Cmd) {
	h := &m.hosts
	if res, ok := msg.(hostsEditedMsg); ok {
		h.busy = false
		h.err = ""
		if res.err != nil {
			h.err = res.err.Error()
		}
		*h = h.reload()
		m.rows = loadRows()
		m.attachHistory()
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok && h.adding {
		var cmd tea.Cmd
		h.input, cmd = h.input.Update(msg) // cursor blink
		return m, cmd
	}
	if !ok || h.busy {
		return m, nil
	}

	if h.adding {
		switch k.String() {
		case "esc", "escape":
			h.adding = false
			h.input.Blur()
			return m, nil
		case "enter":
			name := strings.TrimSpace(h.input.Value())
			if name == "" {
				return m, nil
			}
			h.adding = false
			h.busy = true
			h.input.Blur()
			ref := h.ref
			return m, hostsEditCmd(func(ctx context.Context) error {
				return installer.AddHosts(ctx, ref, []state.Host{{Name: name}})
			})
		}
		var cmd tea.Cmd
		h.input, cmd = h.input.Update(msg)
		return m, cmd
	}

	ref := h.ref
	sel, isAlias := h.selected()
	switch k.String() {
	case "esc", "escape", "q":
		m.screen = screenAppModal
	case "j", "down":
		if h.cursor < len(h.aliases) {
			h.cursor++
		}
	case "k", "up":
		if h.cursor > 0 {
			h.cursor--
		}
	case "a":
		h.err = ""
		h.adding = true
		h.input.SetValue("")
		h.input.Focus()
		return m, textinput.Blink
	case "d", "x":
		if isAlias {
			h.busy = true
			return m, hostsEditCmd(func(ctx context.Context) error {
				return installer.RemoveHost(ctx, ref, sel.Name)
			})
		}
		h.err = "the primary hostname can't be removed; make another one primary first"
	case "r":
		if isAlias {
			h.busy = true
			return m, hostsEditCmd(func(ctx context.Context) error {
				return installer.SetRedirect(ctx, ref, sel.Name, !sel.Redirect)
			})
		}
	case "p":
		if isAlias {
			h.busy = true
			h.cursor = 0
			return m, hostsEditCmd(func(ctx context.Context) error {
				return installer.SetPrimary(ctx, ref, sel.Name)
			})
		}
	}
	return m, nil
}

func renderAppHostsModal(h hostsScreen, width, height int) string {
	const modalW = 60
	innerW := modalW - 2

	title := "─ hostnames: " + h.ref + " "
	topFill := innerW - len([]rune(title))
	if topFill < 0 {
		topFill = 0
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")
	sep := mutedStyle.Render("├" + strings.Repeat("─", innerW) + "┤")

	bfn := func(s string) string {
		pad := innerW - lipgloss.Width(s)
		if pad < 0 {
			pad = 0
		}
		return mutedStyle.Render("│") + s + strings.Repeat(" ", pad) + mutedStyle.Render("│")
	}

	line := func(i int, name, role string) string {
		dot := lipgloss.NewStyle().Foreground(colAccent).Render("○")
		label := lipgloss.NewStyle().Foreground(colFg).Render(name)
		if i == h.cursor && !h.adding {
			dot = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			label = lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(name)
		}
		return bfn("  " + dot + " " + label + "  " + mutedStyle.Render(role))
	}

	rows := []string{top, bfn("")}
	rows = append(rows, line(0, h.primary, "★ primary"))
	for i, a := range h.aliases {
		role := "serves"
		if a.Redirect {
			role = "↪ redirects"
		}
		if hostnames.IsWildcard(a.Name) {
			role += " · every subdomain"
		}
		rows = append(rows, line(i+1, a.Name, role))
	}
	rows = append(rows, bfn(""))

	if h.adding {
		rows = append(rows, sep, bfn("  "+mutedStyle.Render("Add hostname")))
		for _, l := range strings.Split(inputBox(h.input, true), "\n") {
			rows = append(rows, bfn("  "+l))
		}
	}
	if h.busy {
		rows = append(rows, sep, bfn("  "+mutedStyle.Render("applying…")))
	} else if h.err != "" {
		rows = append(rows, sep)
		for _, l := range wrapText("✗ "+h.err, innerW-4) {
			rows = append(rows, bfn("  "+badStyle.Render(l)))
		}
	}
	rows = append(rows, sep)

	var hint string
	if h.adding {
		hint = "  " + keyStyle.Render("↵") + mutedStyle.Render(" add") + "   " + keyStyle.Render("esc") + mutedStyle.Render(" cancel")
	} else {
		hint = "  " + keyStyle.Render("a") + mutedStyle.Render(" add") + "  " +
			keyStyle.Render("d") + mutedStyle.Render(" remove") + "  " +
			keyStyle.Render("r") + mutedStyle.Render(" redirect") + "  " +
			keyStyle.Render("p") + mutedStyle.Render(" primary") + "  " +
			keyStyle.Render("esc") + mutedStyle.Render(" back")
	}
	rows = append(rows, bfn(hint), bot)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		strings.Join(rows, "\n"), lipgloss.WithWhitespaceChars(" "))
}

// wrapText breaks s into lines of at most w runes, on spaces where possible.
func wrapText(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > w {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
