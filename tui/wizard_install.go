package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/plainwork/boxx/engine/installer"
)

// wizardKind selects the install flow.
type wizardKind int

const (
	kindSingle wizardKind = iota
	kindGroup
)

// installWizard drives both single and group install flows.
//
// Steps for single: 0 kind, 1 image, 2 host, 3 db, 4 confirm, 5 running.
// Steps for group : 0 kind, 1 host, 2 db, 3 apps (image+path repeated), 4 confirm, 5 running.
type installWizard struct {
	step int
	kind wizardKind

	// shared inputs
	image textinput.Model // single only
	host  textinput.Model

	// group-only "add app" inputs
	groupImage textinput.Model
	groupPath  textinput.Model
	groupApps  []installer.GroupApp

	dbIdx int

	// run state
	running bool
	logLine string
	err     error
	done    bool
}

var dbChoices = []string{"none", "mysql", "postgres"}

func newInstallWizard(kind wizardKind) installWizard {
	mk := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 200
		ti.Width = 50
		return ti
	}
	w := installWizard{
		kind:       kind,
		image:      mk("ghcr.io/acme/myapp:latest"),
		host:       mk("myapp.example.com"),
		groupImage: mk("ghcr.io/acme/myapp:latest"),
		groupPath:  mk("/admin"),
	}
	w.step = 1
	switch kind {
	case kindSingle:
		w.image.Focus()
	case kindGroup:
		w.host.Focus()
	}
	return w
}

func (w installWizard) Update(msg tea.Msg) (installWizard, tea.Cmd) {
	if w.running {
		switch m := msg.(type) {
		case installResultMsg:
			w.running = false
			w.err = m.err
			w.done = true
		case installLogMsg:
			w.logLine = string(m)
		}
		return w, nil
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			w.done = true
			return w, nil
		case "enter":
			return w.advance()
		}
	}

	// Forward keys to the focused input depending on step.
	if w.kind == kindSingle {
		switch w.step {
		case 1:
			var cmd tea.Cmd
			w.image, cmd = w.image.Update(msg)
			return w, cmd
		case 2:
			var cmd tea.Cmd
			w.host, cmd = w.host.Update(msg)
			return w, cmd
		case 3:
			return w.dbKey(msg), nil
		}
	} else {
		switch w.step {
		case 1:
			var cmd tea.Cmd
			w.host, cmd = w.host.Update(msg)
			return w, cmd
		case 2:
			return w.dbKey(msg), nil
		case 3:
			// editing image OR path; tab cycles focus
			if k, ok := msg.(tea.KeyMsg); ok {
				switch k.String() {
				case "tab", "shift+tab":
					if w.groupImage.Focused() {
						w.groupImage.Blur()
						w.groupPath.Focus()
					} else {
						w.groupPath.Blur()
						w.groupImage.Focus()
					}
					return w, nil
				case "ctrl+a":
					return w.commitGroupApp(), nil
				case "ctrl+d":
					if len(w.groupApps) > 0 {
						w.groupApps = w.groupApps[:len(w.groupApps)-1]
					}
					return w, nil
				}
			}
			var cmd tea.Cmd
			if w.groupImage.Focused() {
				w.groupImage, cmd = w.groupImage.Update(msg)
			} else {
				w.groupPath, cmd = w.groupPath.Update(msg)
			}
			return w, cmd
		}
	}
	return w, nil
}

func (w installWizard) dbKey(msg tea.Msg) installWizard {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "left", "h":
			if w.dbIdx > 0 {
				w.dbIdx--
			}
		case "right", "l":
			if w.dbIdx < len(dbChoices)-1 {
				w.dbIdx++
			}
		}
	}
	return w
}

func (w installWizard) commitGroupApp() installWizard {
	img := strings.TrimSpace(w.groupImage.Value())
	p := strings.TrimSpace(w.groupPath.Value())
	if img == "" || p == "" {
		return w
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	w.groupApps = append(w.groupApps, installer.GroupApp{Image: img, Path: p})
	w.groupImage.SetValue("")
	w.groupPath.SetValue("")
	w.groupImage.Focus()
	w.groupPath.Blur()
	return w
}

func (w installWizard) advance() (installWizard, tea.Cmd) {
	if w.kind == kindSingle {
		switch w.step {
		case 1:
			if strings.TrimSpace(w.image.Value()) == "" {
				return w, nil
			}
			w.step = 2
			w.image.Blur()
			w.host.Focus()
			return w, textinput.Blink
		case 2:
			if strings.TrimSpace(w.host.Value()) == "" {
				return w, nil
			}
			w.step = 3
			w.host.Blur()
			return w, nil
		case 3:
			w.step = 4
			return w, nil
		case 4:
			w.running = true
			db := dbChoices[w.dbIdx]
			if db == "none" {
				db = ""
			}
			return w, runInstallSingle(w.image.Value(), w.host.Value(), db)
		}
	} else {
		switch w.step {
		case 1:
			if strings.TrimSpace(w.host.Value()) == "" {
				return w, nil
			}
			w.step = 2
			w.host.Blur()
			return w, nil
		case 2:
			w.step = 3
			w.groupImage.Focus()
			return w, textinput.Blink
		case 3:
			// enter on apps step commits a row if both fields filled,
			// or advances if there's at least one app and both fields empty.
			img := strings.TrimSpace(w.groupImage.Value())
			p := strings.TrimSpace(w.groupPath.Value())
			if img != "" && p != "" {
				return w.commitGroupApp(), nil
			}
			if len(w.groupApps) > 0 {
				w.step = 4
				return w, nil
			}
			return w, nil
		case 4:
			w.running = true
			db := dbChoices[w.dbIdx]
			if db == "none" {
				db = ""
			}
			return w, runInstallGroup(w.host.Value(), db, w.groupApps)
		}
	}
	return w, nil
}

func (w installWizard) View(width int) string {
	if width < 50 {
		width = 50
	}
	header := titleStyle.Render(" INSTALL WIZARD ") + " " +
		mutedStyle.Render(map[wizardKind]string{kindSingle: "single app", kindGroup: "group"}[w.kind])

	var body string
	switch {
	case w.running:
		body = okStyle.Render("● installing…") + "\n\n" +
			mutedStyle.Render(w.logLine) + "\n\n" +
			footerStyle.Render("(this can take a minute on first run)")
	case w.done && w.err != nil:
		body = badStyle.Render("✗ failed") + "\n\n" +
			bodyStyle.Render(w.err.Error()) + "\n\n" +
			footerStyle.Render("press esc to close")
	case w.done:
		body = okStyle.Render("✓ installed") + "\n\n" +
			footerStyle.Render("press esc to close")
	default:
		body = w.viewStep()
	}

	return header + "\n\n" + body
}

func (w installWizard) viewStep() string {
	if w.kind == kindSingle {
		switch w.step {
		case 1:
			return labelStyle.Render("Container image") + "\n" +
				inputBox(w.image, true) + "\n\n" +
				hint("[enter] next   [esc] cancel")
		case 2:
			return labelStyle.Render("Public hostname") + "\n" +
				inputBox(w.host, true) + "\n\n" +
				hint("[enter] next   [esc] cancel")
		case 3:
			return labelStyle.Render("Database") + "\n\n" +
				dbPicker(w.dbIdx) + "\n\n" +
				hint("[← →] choose   [enter] next   [esc] cancel")
		case 4:
			db := dbChoices[w.dbIdx]
			return labelStyle.Render("Confirm") + "\n\n" +
				kv("image", w.image.Value()) +
				kv("host", w.host.Value()) +
				kv("db", db) + "\n" +
				btnStyle.Render(" Install ") + "\n\n" +
				hint("[enter] install   [esc] cancel")
		}
	} else {
		switch w.step {
		case 1:
			return labelStyle.Render("Public hostname (shared)") + "\n" +
				inputBox(w.host, true) + "\n\n" +
				hint("[enter] next   [esc] cancel")
		case 2:
			return labelStyle.Render("Shared database") + "\n\n" +
				dbPicker(w.dbIdx) + "\n\n" +
				hint("[← →] choose   [enter] next   [esc] cancel")
		case 3:
			rows := ""
			if len(w.groupApps) == 0 {
				rows = mutedStyle.Render("  (no apps yet — fill image + path and press [ctrl+a])\n")
			} else {
				for i, a := range w.groupApps {
					rows += fmt.Sprintf("  %d. %s  %s\n",
						i+1, bodyStyle.Render(a.Path), mutedStyle.Render(a.Image))
				}
			}
			return labelStyle.Render("Apps in this group") + "\n" + rows + "\n" +
				labelStyle.Render("Image") + "\n" +
				inputBox(w.groupImage, w.groupImage.Focused()) + "\n" +
				labelStyle.Render("Path") + "\n" +
				inputBox(w.groupPath, w.groupPath.Focused()) + "\n\n" +
				hint("[tab] switch field   [ctrl+a] add app   [ctrl+d] remove last   [enter] next   [esc] cancel")
		case 4:
			db := dbChoices[w.dbIdx]
			rows := ""
			for i, a := range w.groupApps {
				rows += fmt.Sprintf("  %d. %s  %s\n", i+1, bodyStyle.Render(a.Path), mutedStyle.Render(a.Image))
			}
			return labelStyle.Render("Confirm") + "\n\n" +
				kv("host", w.host.Value()) +
				kv("db", db) +
				labelStyle.Render("apps") + "\n" + rows + "\n" +
				btnStyle.Render(" Install group ") + "\n\n" +
				hint("[enter] install   [esc] cancel")
		}
	}
	return ""
}

func kv(k, v string) string {
	return labelStyle.Render(fmt.Sprintf("%-6s", k)) + " " + bodyStyle.Render(v) + "\n"
}

func hint(s string) string { return footerStyle.Render(s) }

func inputBox(ti textinput.Model, focused bool) string {
	style := inputBoxStyle
	if focused {
		style = inputBoxFocused
	}
	return style.Width(ti.Width + 2).Render(ti.View())
}

func dbPicker(idx int) string {
	parts := make([]string, len(dbChoices))
	for i, c := range dbChoices {
		if i == idx {
			parts[i] = btnStyle.Render(" " + c + " ")
		} else {
			parts[i] = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colBorder).
				Padding(0, 1).
				Render(c)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// ---- async install commands ----

type installResultMsg struct{ err error }
type installLogMsg string

func runInstallSingle(image, host, dbEngine string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, err := installer.InstallSingle(ctx, installer.SingleSpec{
			Image:    image,
			Hostname: host,
			DBEngine: dbEngine,
		}, nil)
		return installResultMsg{err: err}
	}
}

func runInstallGroup(host, dbEngine string, apps []installer.GroupApp) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		_, err := installer.InstallGroup(ctx, installer.GroupSpec{
			Hostname: host,
			DBEngine: dbEngine,
			Apps:     apps,
		}, nil)
		return installResultMsg{err: err}
	}
}
