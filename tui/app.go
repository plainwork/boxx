package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/metrics"
	"github.com/plainwork/boxx/engine/release"
	"github.com/plainwork/boxx/engine/state"
)

// Version is injected at startup by cmd.SetVersion → tui.SetVersion.
var Version = "dev"

// SetVersion stores the build-time version for display in the TUI header.
func SetVersion(v string) { Version = v }

// sendToProgram is set when the TUI starts; install/deploy cmds use it to push
// progress messages from goroutines into the Bubble Tea event loop.
var sendToProgram func(tea.Msg)

// Run launches the TUI dashboard.
func Run() error {
	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
	)
	sendToProgram = func(msg tea.Msg) { p.Send(msg) }
	_, err := p.Run()
	sendToProgram = nil
	return err
}

// progressSend returns an installer.Progress that feeds step updates into the TUI.
func progressSend() installer.Progress {
	return func(step, msg string) {
		if sendToProgram != nil {
			sendToProgram(loadingStepMsg(step + ": " + msg))
		}
	}
}

type screen int

const (
	screenDashboard screen = iota
	screenWizard
	screenModal
	screenNewApp
	screenAppModal
	screenAppModalImage
	screenLoading
	screenLogs
	screenAppSettings  // per-app update-policy settings
	screenAppEnvConfig // per-app env view/edit/rollback
	screenOpsLog       // boxx operational log (boxx.log)
)

type model struct {
	width, height  int
	screen         screen
	rows           []row
	cursor         int
	inGroup        bool
	innerCursor    int
	actionCursor   int    // cursor within the DB action modal
	newAppCursor   int    // cursor within the new-app type modal (0=single, 1=group)
	appActionCursor int              // cursor within the app action modal
	appActionSlug   string           // slug of the app whose action modal is open
	appActionImage  textinput.Model  // text input for change-image action
	loadingFrame    int              // animation frame counter for loading modal
	loadingLabel    string           // human label shown while loading
	loadingStep     string           // current step reported by progress callback
	loadingStarted  time.Time        // when the current op started
	pendingOp       *opResultMsg     // op finished but min time not yet elapsed
	logLines        []string         // accumulated log output lines
	logReader       *bufio.Reader    // reader for the live docker logs pipe
	logCmd          *exec.Cmd        // running docker logs process
	logSlug         string           // slug whose logs are being shown
	logScroll       int              // 0 = pinned to newest, >0 = lines scrolled up
	appSettingsCursor    int    // cursor within per-app settings screen
	appSettingsActiveMode string // currently saved update mode for the open app
	appEnvConfigCursor   int    // cursor within per-app env-config screen
	inspectMap     map[string]bool // per-entry: true = showing component list, false = showing stats
	detailMap      map[string]bool // per-container: true = show inline stats in group list
	wizard         installWizard
	flash          string
	flashUntil     time.Time
	hostInfo       dockerx.HostInfo
	containerStats map[string]dockerx.ContainerStats
	netPrev        map[string][2]uint64  // [NetIn, NetOut] from last stats poll
	netPrevTime    map[string]time.Time  // timestamp of last stats poll
	cpuPeak        map[string]float64    // highest CPUPerc seen per container
	memPeak        map[string]uint64     // highest MemUsage bytes seen per container
	visMetrics     map[string]metrics.AppMetrics // hostname → last known metrics (survives loadRows)
	cpuHist        map[string][]float64           // container → CPU% history (reserved for dot-matrix)
	memHist        map[string][]float64
	visHist        map[string][]float64
	boxxLatestTag  string // non-empty when a newer boxx release is available
}

func initialModel() model {
	rows := loadRows()
	return model{
		screen:         screenDashboard,
		rows:           rows,
		appActionImage: func() textinput.Model {
			ti := textinput.New()
			ti.Placeholder = "ghcr.io/acme/myapp:latest"
			ti.CharLimit = 300
			ti.Width = 46
			return ti
		}(),
		containerStats: map[string]dockerx.ContainerStats{},
		netPrev:        map[string][2]uint64{},
		netPrevTime:    map[string]time.Time{},
		cpuPeak:        map[string]float64{},
		memPeak:        map[string]uint64{},
		visMetrics:     map[string]metrics.AppMetrics{},
		inspectMap:     map[string]bool{},
		detailMap:      map[string]bool{},
		cpuHist:        map[string][]float64{},
		memHist:        map[string][]float64{},
		visHist:        map[string][]float64{},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), statsCmd(m.rows), hostInfoCmd(), checkSelfUpdateCmd())
}

// selfUpdateMsg is sent once at startup with the latest boxx tag (may be "").
type selfUpdateMsg struct{ tag string }

// checkSelfUpdateCmd reads the local release cache (no network) and kicks off
// an async refresh so the cache is warm for the next invocation.
func checkSelfUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		tag := release.Cached()
		release.RefreshAsync()
		return selfUpdateMsg{tag: tag}
	}
}

type hostInfoMsg struct{ info dockerx.HostInfo }

func hostInfoCmd() tea.Cmd {
	return func() tea.Msg {
		info, _ := dockerx.GetHostInfo(context.Background())
		return hostInfoMsg{info}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case hostInfoMsg:
		m.hostInfo = msg.info
		return m, nil

	case selfUpdateMsg:
		if release.IsNewer(msg.tag, Version) {
			m.boxxLatestTag = msg.tag
		}
		return m, nil

	case tickMsg:
		m.rows = loadRows()
		if entries := dashEntries(m.rows); m.cursor >= len(entries) && len(entries) > 0 {
			m.cursor = len(entries) - 1
		}
		m.attachHistory()
		return m, tea.Batch(tickCmd(), statsCmd(m.rows))

	case statsMsg:
		now := time.Now()
		for name, s := range msg.stats {
			if prev, hasPrev := m.netPrev[name]; hasPrev {
				if t, hasTime := m.netPrevTime[name]; hasTime {
					elapsed := now.Sub(t).Minutes()
					if elapsed > 0 {
						s.NetInRate = float64(s.NetIn-prev[0]) / elapsed
						s.NetOutRate = float64(s.NetOut-prev[1]) / elapsed
						if s.NetInRate < 0 {
							s.NetInRate = 0
						}
						if s.NetOutRate < 0 {
							s.NetOutRate = 0
						}
					}
				}
			}
			m.netPrev[name] = [2]uint64{s.NetIn, s.NetOut}
			m.netPrevTime[name] = now
			// track peaks
			if s.CPUPerc > m.cpuPeak[name] {
				m.cpuPeak[name] = s.CPUPerc
			}
			s.CPUPeak = m.cpuPeak[name]
			if s.MemUsage > m.memPeak[name] {
				m.memPeak[name] = s.MemUsage
			}
			s.MemPeak = m.memPeak[name]
			msg.stats[name] = s
		}
		m.containerStats = msg.stats
		metrics.Poll() // pull new log lines first
		for i, r := range m.rows {
			if r.container != "" {
				if st, ok := m.containerStats[r.container]; ok {
					m.rows[i].cpuPerc = st.CPUPerc
					m.rows[i].memUsage = st.MemUsage
					m.rows[i].memLimit = st.MemLimit
					m.cpuHist[r.container] = appendHist(m.cpuHist[r.container], st.CPUPerc, 20)
					mp := 0.0
					if st.MemLimit > 0 {
						mp = float64(st.MemUsage) * 100.0 / float64(st.MemLimit)
					}
					m.memHist[r.container] = appendHist(m.memHist[r.container], mp, 20)
				}
			}
			if r.hostname != "" {
				vm := metrics.Get(r.hostname)
				m.rows[i].visitMetrics = vm
				m.visMetrics[r.hostname] = vm // persist so attachHistory restores it after loadRows
				m.visHist[r.hostname] = appendHist(m.visHist[r.hostname], vm.ReqPerMin, 20)
			}
		}
		m.attachHistory()
		return m, nil

	case opResultMsg:
		if m.screen == screenLoading {
			elapsed := time.Since(m.loadingStarted)
			if elapsed >= installer.MinLoadingDuration {
				m = applyOpResult(m, msg)
				m.screen = screenDashboard
			} else {
				m.pendingOp = &msg
				remaining := installer.MinLoadingDuration - elapsed
				return m, tea.Tick(remaining, func(time.Time) tea.Msg { return loadingReadyMsg{} })
			}
		} else {
			// op finished while not on loading screen (shouldn't happen, but handle cleanly)
			m = applyOpResult(m, msg)
		}
		return m, nil

	case loadingReadyMsg:
		if m.pendingOp != nil {
			m = applyOpResult(m, *m.pendingOp)
			m.pendingOp = nil
		}
		m.screen = screenDashboard
		return m, nil
	}

	switch m.screen {
	case screenWizard:
		wasRunning := m.wizard.running
		newW, cmd := m.wizard.Update(msg)
		m.wizard = newW
		if newW.running && !wasRunning {
			// wizard just fired its install command — hand off to the loading screen
			label := "installing " + newW.host.Value() + "…"
			return m, m.startOp(label, cmd)
		}
		if newW.done && !newW.running {
			m.screen = screenDashboard
			m.rows = loadRows()
			if newW.err == nil {
				m.setFlash(okStyle.Render("✓ installed"))
			}
		}
		return m, cmd

	case screenLoading:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" || msg.String() == "escape" {
				m.screen = screenDashboard
			}
		case loadingTickMsg:
			m.loadingFrame++
			return m, tickLoading()
		case loadingStepMsg:
			m.loadingStep = string(msg)
		}
		return m, nil

	case screenLogs:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "escape":
				if m.logCmd != nil && m.logCmd.Process != nil {
					_ = m.logCmd.Process.Kill()
				}
				m.logLines = nil
				m.logCmd = nil
				m.logReader = nil
				m.screen = screenDashboard
			case "k", "up":
				contentH := m.height - 2
				maxScroll := len(m.logLines) - contentH
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.logScroll < maxScroll {
					m.logScroll++
				}
			case "j", "down":
				if m.logScroll > 0 {
					m.logScroll--
				}
			case "G", "g":
				m.logScroll = 0
			}
		case logLineMsg:
			m.logLines = append(m.logLines, string(msg))
			return m, readLogLine(m.logReader)
		case logDoneMsg:
			if msg.err == nil || msg.err == io.EOF {
				m.logLines = append(m.logLines, mutedStyle.Render("── stream ended ──"))
			} else {
				m.logLines = append(m.logLines, badStyle.Render("── "+msg.err.Error()+" ──"))
			}
		}
		return m, nil

	case screenOpsLog:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "escape":
				if m.logCmd != nil && m.logCmd.Process != nil {
					_ = m.logCmd.Process.Kill()
				}
				m.logLines = nil
				m.logCmd = nil
				m.logReader = nil
				m.screen = screenDashboard
			case "k", "up":
				contentH := m.height - 2
				maxScroll := len(m.logLines) - contentH
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.logScroll < maxScroll {
					m.logScroll++
				}
			case "j", "down":
				if m.logScroll > 0 {
					m.logScroll--
				}
			case "G", "g":
				m.logScroll = 0
			}
		case opsLogReadyMsg:
			m.logLines = msg.initial
			m.logCmd = msg.cmd
			m.logReader = msg.reader
			return m, readOpsLogLine(m.logReader)
		case opsLogLineMsg:
			m.logLines = append(m.logLines, string(msg))
			return m, readOpsLogLine(m.logReader)
		case opsLogDoneMsg:
			if msg.err == nil || msg.err == io.EOF {
				m.logLines = append(m.logLines, mutedStyle.Render("── end of log ──"))
			} else {
				m.logLines = append(m.logLines, badStyle.Render("── "+msg.err.Error()+" ──"))
			}
		}
		return m, nil

	case screenAppModalImage:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "escape", "esc":
				m.appActionImage.Blur()
				m.screen = screenAppModal
				return m, nil
			case "enter":
				img := strings.TrimSpace(m.appActionImage.Value())
				if img == "" {
					return m, nil
				}
				slug := m.appActionSlug
				m.appActionImage.Blur()
				return m, m.startOp("deploying "+slug+"…", deployWithImageCmd(slug, img))
			}
		}
		var imgCmd tea.Cmd
		m.appActionImage, imgCmd = m.appActionImage.Update(msg)
		return m, imgCmd

	case screenAppModal:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "escape", "esc":
				m.screen = screenDashboard
			case "j", "down":
				if m.appActionCursor < len(appActions)-1 {
					m.appActionCursor++
				}
			case "k", "up":
				if m.appActionCursor > 0 {
					m.appActionCursor--
				}
			case "enter":
				switch appActions[m.appActionCursor].key {
				case "deploy":
					slug := m.appActionSlug
					return m, m.startOp("deploying "+slug+"…", deployCmd(slug))
				case "change-image":
					m.appActionImage.SetValue("")
					m.appActionImage.Placeholder = lookupCurrentImage(m.appActionSlug)
					m.appActionImage.Focus()
					m.screen = screenAppModalImage
					return m, textinput.Blink
				case "restart":
					slug := m.appActionSlug
					return m, m.startOp("restarting "+slug+"…", restartCmd(slug))
				case "settings":
					activeMode := lookupUpdateMode(m.appActionSlug)
					switch activeMode {
					case state.UpdateModeOff:
						m.appSettingsCursor = 0
					case state.UpdateModeAuto:
						m.appSettingsCursor = 2
					default:
						m.appSettingsCursor = 1
					}
					m.appSettingsActiveMode = string(activeMode)
					m.screen = screenAppSettings
					return m, nil
				case "env-config":
					m.appEnvConfigCursor = 0
					m.screen = screenAppEnvConfig
					return m, nil
				case "remove":
					slug := m.appActionSlug
					return m, m.startOp("removing "+slug+"…", removeCmd(slug))
				case "logs":
					slug := m.appActionSlug
					container, err := installer.LiveContainer(slug)
					if err != nil {
						m.setFlash(badStyle.Render("logs: ") + err.Error())
						m.screen = screenDashboard
						return m, nil
					}
					cmd := exec.Command("docker", "logs", "--tail", "500", "-f", container)
					stdout, err := cmd.StdoutPipe()
					if err != nil {
						m.setFlash(badStyle.Render("logs: ") + err.Error())
						m.screen = screenDashboard
						return m, nil
					}
					stderr, _ := cmd.StderrPipe()
					if err := cmd.Start(); err != nil {
						m.setFlash(badStyle.Render("logs: ") + err.Error())
						m.screen = screenDashboard
						return m, nil
					}
					m.logLines = nil
					m.logScroll = 0
					m.logSlug = slug
					m.logCmd = cmd
					m.logReader = bufio.NewReader(io.MultiReader(stdout, stderr))
					m.screen = screenLogs
					return m, readLogLine(m.logReader)
				default:
					m.screen = screenDashboard
				}
			}
		}
		return m, nil

	case screenModal:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "escape", "esc":
				m.screen = screenDashboard
			case "j", "down":
				if m.actionCursor < len(dbActions)-1 {
					m.actionCursor++
				}
			case "k", "up":
				if m.actionCursor > 0 {
					m.actionCursor--
				}
			}
		}
		return m, nil

	case screenAppSettings:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "escape", "esc", "q":
				m.screen = screenAppModal
			case "j", "down":
				if m.appSettingsCursor < 2 {
					m.appSettingsCursor++
				}
			case "k", "up":
				if m.appSettingsCursor > 0 {
					m.appSettingsCursor--
				}
			case "enter", " ":
				// Cycle the update mode for the selected app.
				applyUpdateModeCycle(m.appActionSlug, m.appSettingsCursor)
				modes := []state.UpdateMode{state.UpdateModeOff, state.UpdateModeNotify, state.UpdateModeAuto}
				m.appSettingsActiveMode = string(modes[m.appSettingsCursor])
				m.rows = loadRows()
			}
		}
		return m, nil

	case screenAppEnvConfig:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "escape", "esc", "q":
				m.screen = screenAppModal
			case "j", "down":
				if m.appEnvConfigCursor < 2 {
					m.appEnvConfigCursor++
				}
			case "k", "up":
				if m.appEnvConfigCursor > 0 {
					m.appEnvConfigCursor--
				}
			case "enter", " ":
				switch m.appEnvConfigCursor {
				case 0: // env push → launch external editor
					m.screen = screenDashboard
					m.setFlash("Use: boxx env push " + m.appActionSlug)
				case 1: // env import hint
					m.screen = screenDashboard
					m.setFlash("Use: boxx env import " + m.appActionSlug + " --file <path>")
				case 2: // rollback
					slug := m.appActionSlug
					m.screen = screenDashboard
					return m, m.startOp("rolling back env for "+slug+"…", envRollbackCmd(slug))
				}
			}
		}
		return m, nil

	case screenNewApp:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "escape", "esc":
				m.screen = screenDashboard
			case "j", "down":
				if m.newAppCursor < 1 {
					m.newAppCursor++
				}
			case "k", "up":
				if m.newAppCursor > 0 {
					m.newAppCursor--
				}
			case "enter", " ":
				if m.newAppCursor == 0 {
					m.wizard = newInstallWizard(kindSingle)
				} else {
					m.wizard = newInstallWizard(kindGroup)
				}
				m.screen = screenWizard
			}
		}
		return m, nil

	case screenDashboard:
		if k, ok := msg.(tea.KeyMsg); ok {
			return m.handleKey(k)
		}
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.inGroup {
			if e := selectedEntry(m); e != nil {
				total := len(e.apps)
				if e.groupDB != nil {
					total++
				}
				if m.innerCursor < total-1 {
					m.innerCursor++
				}
			}
		} else if m.cursor < len(dashEntries(m.rows))-1 {
			m.cursor++
			m.inGroup = false
			m.innerCursor = 0
		}
	case "k", "up":
		if m.inGroup {
			if m.innerCursor > 0 {
				m.innerCursor--
			}
		} else if m.cursor > 0 {
			m.cursor--
			m.inGroup = false
			m.innerCursor = 0
		}
	case "escape", "esc":
		if se := selectedEntry(m); se != nil && !se.isGroup && m.inspectMap[se.name] {
			m.inspectMap[se.name] = false
			m.inGroup = false
		} else if m.inGroup {
			m.inGroup = false
		}
	case "a":
		if e := selectedEntry(m); e != nil {
			if m.inGroup && e.groupDB != nil && m.innerCursor == 0 {
				// DB actions modal
				m.actionCursor = 0
				m.screen = screenModal
			} else if m.inGroup && e.isGroup {
				// app item in group
				dbOffset := 0
				if e.groupDB != nil {
					dbOffset = 1
				}
				appIdx := m.innerCursor - dbOffset
				if appIdx >= 0 && appIdx < len(e.apps) {
					m.appActionCursor = 0
					m.appActionSlug = e.apps[appIdx].slug
					m.screen = screenAppModal
				}
			} else if !e.isGroup {
				// single app
				m.appActionCursor = 0
				m.appActionSlug = e.name
				m.screen = screenAppModal
			}
		}
	case "i":
		// inspect components for a single app
		if e := selectedEntry(m); e != nil && !e.isGroup && !m.inspectMap[e.name] {
			m.inspectMap[e.name] = true
			m.inGroup = true
			m.innerCursor = 0
		}
	case "d":
		if e := selectedEntry(m); e != nil && e.isGroup {
			dbOffset := 0
			if e.groupDB != nil {
				dbOffset = 1
			}
			if m.inGroup {
				// toggle individual item under cursor (skip db row)
				appIdx := m.innerCursor - dbOffset
				if appIdx >= 0 && appIdx < len(e.apps) {
					ctr := e.apps[appIdx].container
					if ctr != "" {
						m.detailMap[ctr] = !m.detailMap[ctr]
					}
				}
			} else {
				// toggle all apps in the group
				// determine target state: if any are off, turn all on; else turn all off
				anyOff := false
				for _, r := range e.apps {
					if r.container != "" && !m.detailMap[r.container] {
						anyOff = true
						break
					}
				}
				for _, r := range e.apps {
					if r.container != "" {
						m.detailMap[r.container] = anyOff
					}
				}
			}
		}
	case "enter":
		if m.inGroup {
			// deploy selected app — TODO
		}
	case "L":
		m.logLines = nil
		m.logScroll = 0
		m.logSlug = "boxx ops log"
		m.screen = screenOpsLog
		return m, startOpsLog()
	case "U":
		if m.boxxLatestTag != "" {
			// Replace this process with `boxx upgrade` — TUI exits cleanly.
			self, err := os.Executable()
			if err == nil {
				syscall.Exec(self, []string{self, "upgrade"}, os.Environ()) //nolint:errcheck
			}
		}
	case "n":
		m.newAppCursor = 0
		m.screen = screenNewApp
	case "m":
		m.loadingFrame = 0
		m.loadingLabel = "deploying stored…"
		m.screen = screenLoading
		return m, tickLoading()
	case " ":
		if m.inGroup {
			m.inGroup = false
		} else if e := selectedEntry(m); e != nil && e.isGroup {
			m.inGroup = true
			m.innerCursor = 0
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	title := titleBar("boxx", Version, m.boxxLatestTag, m.width)
	showActions := m.screen == screenDashboard && m.inGroup && m.innerCursor == 0 &&
		selectedEntry(m) != nil && selectedEntry(m).groupDB != nil
	se := selectedEntry(m)
	seInspecting := se != nil && m.inspectMap[se.name]
	showInspect := m.screen == screenDashboard && se != nil && !se.isGroup && !seInspecting
	showDetails := m.screen == screenDashboard && se != nil && se.isGroup
	// show a · actions when a single is selected, or when inside a group on a non-DB row
	showAppActions := m.screen == screenDashboard && se != nil && (
		(!se.isGroup) ||
		(m.inGroup && se.isGroup && func() bool {
			dbOffset := 0
			if se.groupDB != nil { dbOffset = 1 }
			return m.innerCursor >= dbOffset
		}()))
	kb := keyBar(m.width, m.getFlash(), showActions, showInspect, seInspecting, showDetails, showAppActions, m.boxxLatestTag != "")
	contentH := m.height - 2
	if contentH < 1 {
		contentH = 1
	}

	isModal := m.screen == screenModal || m.screen == screenNewApp ||
		m.screen == screenAppModal || m.screen == screenAppModalImage ||
		m.screen == screenLoading || m.screen == screenAppSettings ||
		m.screen == screenAppEnvConfig
	if isModal {
		contentH = m.height - 1
	}

	var content string
	switch m.screen {
	case screenWizard:
		wizW := m.width - 8
		if wizW > 100 {
			wizW = 100
		}
		if wizW < 40 {
			wizW = 40
		}
		box := panelStyle.Width(wizW).Render(m.wizard.View(wizW - 4))
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, box)
	case screenModal:
		dash := renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
		content = renderActionModal(dash, m.actionCursor, m.width, contentH)
	case screenAppModal:
		dash := renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
		content = renderAppActionModal(dash, m.appActionCursor, m.appActionSlug, m.width, contentH)
	case screenAppModalImage:
		dash := renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
		content = renderAppImageModal(dash, m.appActionImage, m.appActionSlug, m.width, contentH)
	case screenLoading:
		content = renderLoadingModal(m.loadingFrame, m.loadingLabel, m.loadingStep, m.width, contentH)
	case screenLogs, screenOpsLog:
		content = renderLogsScreen(m.logLines, m.logSlug, m.logScroll, m.width, contentH)
	case screenAppSettings:
		dash := renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
		content = renderAppSettingsModal(dash, m.appActionSlug, m.appSettingsCursor, m.width, contentH, m.appSettingsActiveMode)
	case screenAppEnvConfig:
		dash := renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
		content = renderAppEnvConfigModal(dash, m.appActionSlug, m.appEnvConfigCursor, m.width, contentH)
	case screenNewApp:
		dash := renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
		content = renderNewAppModal(dash, m.newAppCursor, m.width, contentH)
	default:
		content = renderDashboard(m.rows, m.cursor, m.inGroup, m.innerCursor, m.width, contentH, m.hostInfo, m.containerStats, m.inspectMap, m.detailMap)
	}

	if m.screen == screenLogs || m.screen == screenOpsLog {
		return lipgloss.JoinVertical(lipgloss.Left, title, content, logsKeyBar(m.width, m.logScroll))
	}
	if isModal {
		return lipgloss.JoinVertical(lipgloss.Left, title, content)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, content, kb)
}

func selectedEntry(m model) *dashEntry {
	entries := dashEntries(m.rows)
	if m.cursor >= 0 && m.cursor < len(entries) {
		e := entries[m.cursor]
		return &e
	}
	return nil
}

func (m model) getFlash() string {
	if m.flash != "" && time.Now().Before(m.flashUntil) {
		return m.flash
	}
	return ""
}

func (m *model) setFlash(s string) {
	m.flash = s
	m.flashUntil = time.Now().Add(4 * time.Second)
}

// ---- ticker ────────────────────────────────────────────────────────────────

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---- async stats ───────────────────────────────────────────────────────────

type statsMsg struct {
	stats map[string]dockerx.ContainerStats
}

func statsCmd(rows []row) tea.Cmd {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.container != "" {
			names = append(names, r.container)
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s, _ := dockerx.Stats(ctx, names)
		return statsMsg{stats: s}
	}
}

// ---- async ops ─────────────────────────────────────────────────────────────

type opResultMsg struct {
	slug  string
	op    string // "deploy", "restart", "remove"
	err   error
}

type loadingReadyMsg struct{}
type loadingStepMsg string // progress update from a running op

func applyOpResult(m model, msg opResultMsg) model {
	if msg.err != nil {
		m.setFlash(badStyle.Render(msg.op+" failed: ") + msg.err.Error())
		return m
	}
	switch msg.op {
	case "deploy":
		m.setFlash(okStyle.Render("✓ deployed: ") + msg.slug)
	case "install":
		m.setFlash(okStyle.Render("✓ installed: ") + msg.slug)
	case "restart":
		m.setFlash(okStyle.Render("✓ restarted: ") + msg.slug)
	case "remove":
		m.setFlash(okStyle.Render("✓ removed: ") + msg.slug)
	case "logs":
		// clean exit from log stream — no flash needed
		return m
	}
	m.rows = loadRows()
	return m
}

func deployCmd(slug string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := installer.Deploy(ctx, installer.DeploySpec{Slug: slug}, progressSend())
		return opResultMsg{slug: slug, op: "deploy", err: err}
	}
}

func deployWithImageCmd(slug, newImage string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := installer.Deploy(ctx, installer.DeploySpec{Slug: slug, NewImage: newImage}, progressSend())
		return opResultMsg{slug: slug, op: "deploy", err: err}
	}
}

func restartCmd(slug string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := installer.Restart(ctx, slug)
		return opResultMsg{slug: slug, op: "restart", err: err}
	}
}

func removeCmd(slug string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := installer.Remove(ctx, slug, false)
		return opResultMsg{slug: slug, op: "remove", err: err}
	}
}

func envRollbackCmd(slug string) tea.Cmd {
	return func() tea.Msg {
		s, err := state.Load()
		if err != nil {
			return opResultMsg{slug: slug, op: "env-rollback", err: err}
		}
		// Parse slug/app form
		parts := strings.SplitN(slug, "/", 2)
		appSlug := ""
		if len(parts) == 2 {
			slug = parts[0]
			appSlug = parts[1]
		}
		// Restore PrevEnv
		var prev *state.EnvBackup
		if appSlug == "" {
			app := s.Singles[slug]
			prev = app.PrevEnv
			if prev == nil {
				return opResultMsg{slug: slug, op: "env-rollback", err: fmt.Errorf("no env backup for %s", slug)}
			}
			app.PrevEnv = nil
			app.Env = prev.Env
			s.Singles[slug] = app
		} else {
			grp := s.Groups[slug]
			app := grp.Apps[appSlug]
			prev = app.PrevEnv
			if prev == nil {
				return opResultMsg{slug: slug, op: "env-rollback", err: fmt.Errorf("no env backup for %s/%s", slug, appSlug)}
			}
			app.PrevEnv = nil
			app.Env = prev.Env
			grp.Apps[appSlug] = app
			s.Groups[slug] = grp
		}
		if err := state.Save(s); err != nil {
			return opResultMsg{slug: slug, op: "env-rollback", err: err}
		}
		fullSlug := slug
		if appSlug != "" {
			fullSlug = slug + "/" + appSlug
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err = installer.Deploy(ctx, installer.DeploySpec{Slug: fullSlug}, progressSend())
		return opResultMsg{slug: fullSlug, op: "env-rollback", err: err}
	}
}

// applyUpdateModeCycle cycles the update mode for the given app through off→notify→auto.
// cursor 0 = off, 1 = notify, 2 = auto
func applyUpdateModeCycle(slug string, cursor int) {
	modes := []state.UpdateMode{state.UpdateModeOff, state.UpdateModeNotify, state.UpdateModeAuto}
	if cursor >= len(modes) {
		return
	}
	mode := modes[cursor]
	s, err := state.Load()
	if err != nil {
		return
	}
	parts := strings.SplitN(slug, "/", 2)
	appSlug := ""
	base := slug
	if len(parts) == 2 {
		base = parts[0]
		appSlug = parts[1]
	}
	if appSlug == "" {
		app := s.Singles[base]
		app.UpdatePolicy.Mode = mode
		s.Singles[base] = app
	} else {
		grp := s.Groups[base]
		app := grp.Apps[appSlug]
		app.UpdatePolicy.Mode = mode
		grp.Apps[appSlug] = app
		s.Groups[base] = grp
	}
	_ = state.Save(s)
}

// startOp switches to screenLoading and fires the given command.
func (m *model) startOp(label string, cmd tea.Cmd) tea.Cmd {
	m.loadingLabel = label
	m.loadingStep = ""
	m.loadingStarted = time.Now()
	m.loadingFrame = 0
	m.pendingOp = nil
	m.screen = screenLoading
	return tea.Batch(tickLoading(), cmd)
}

// ---- log streaming ──────────────────────────────────────────────────────────

type logLineMsg string
type logDoneMsg struct{ err error }

// readLogLine returns a cmd that reads one line from r and returns it as a msg.
// It chains: each logLineMsg handler fires another readLogLine call.
func readLogLine(r *bufio.Reader) tea.Cmd {
	return func() tea.Msg {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\n\r")
		if line != "" && err == io.EOF {
			return logLineMsg(line) // last line without trailing newline
		}
		if err != nil {
			return logDoneMsg{err: err}
		}
		return logLineMsg(line)
	}
}

// ---- history helpers ───────────────────────────────────────────────────────

// lookupUpdateMode returns the current UpdatePolicy.Mode for a slug.
func lookupUpdateMode(slug string) state.UpdateMode {
	s, err := state.Load()
	if err != nil {
		return state.UpdateModeNotify
	}
	if idx := strings.Index(slug, "/"); idx >= 0 {
		gSlug := slug[:idx]
		aSlug := slug[idx+1:]
		if g, ok := s.Groups[gSlug]; ok {
			if a, ok := g.Apps[aSlug]; ok {
				return a.UpdatePolicy.Mode
			}
		}
		return state.UpdateModeNotify
	}
	if a, ok := s.Singles[slug]; ok {
		return a.UpdatePolicy.Mode
	}
	return state.UpdateModeNotify
}

// lookupCurrentImage returns the current image for a slug ("single-slug" or "group-slug/app-slug").
func lookupCurrentImage(slug string) string {
	s, err := state.Load()
	if err != nil {
		return ""
	}
	if idx := strings.Index(slug, "/"); idx >= 0 {
		gSlug := slug[:idx]
		aSlug := slug[idx+1:]
		if g, ok := s.Groups[gSlug]; ok {
			if a, ok := g.Apps[aSlug]; ok {
				return a.Image
			}
		}
		return ""
	}
	if a, ok := s.Singles[slug]; ok {
		return a.Image
	}
	return ""
}

// attachHistory restores per-row stats from model-level maps after loadRows() resets them.
func (m *model) attachHistory() {
	for i, r := range m.rows {
		if st, ok := m.containerStats[r.container]; ok {
			m.rows[i].cpuPerc = st.CPUPerc
			m.rows[i].memUsage = st.MemUsage
			m.rows[i].memLimit = st.MemLimit
		}
		if vm, ok := m.visMetrics[r.hostname]; ok {
			m.rows[i].visitMetrics = vm
		}
	}
}

// appendHist appends v to h and trims to at most maxLen entries.
func appendHist(h []float64, v float64, maxLen int) []float64 {
	h = append(h, v)
	if len(h) > maxLen {
		h = h[len(h)-maxLen:]
	}
	return h
}

