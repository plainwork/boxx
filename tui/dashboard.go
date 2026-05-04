package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/metrics"
	"github.com/plainwork/boxx/engine/state"
)

type row struct {
	kind      string // "single" | "group-app"
	slug      string
	hostname  string
	path      string
	color     string
	container string // live container name for docker stats
	// group membership (non-empty for group-app rows)
	groupSlug string
	groupHost string
	groupDB   *state.DB
	// live stats
	cpuPerc      float64
	memUsage     uint64
	memLimit     uint64
	visitMetrics metrics.AppMetrics
}

func loadRows() []row {
	s, err := state.Load()
	if err != nil {
		return []row{{slug: "(state error: " + err.Error() + ")"}}
	}
	var rows []row

	sk := keys(s.Singles)
	sort.Strings(sk)
	for _, k := range sk {
		a := s.Singles[k]
		container := ""
		if a.LiveColor != "" {
			container = "boxx-app-" + a.Slug + "-" + a.LiveColor
		}
		rows = append(rows, row{
			kind: "single", slug: a.Slug,
			hostname: a.Hostname, path: "/",
			color: a.LiveColor, container: container,
			groupDB: a.DB,
		})
	}

	gk := keys(s.Groups)
	sort.Strings(gk)
	for _, k := range gk {
		g := s.Groups[k]
		ak := keys(g.Apps)
		sort.Strings(ak)
		for _, ak := range ak {
			a := g.Apps[ak]
			fullSlug := g.Slug + "-" + a.Slug
			container := ""
			if a.LiveColor != "" {
				container = "boxx-app-" + fullSlug + "-" + a.LiveColor
			}
			rows = append(rows, row{
				kind: "group-app", slug: g.Slug + "/" + a.Slug,
				hostname: g.Hostname, path: a.Path,
				color: a.LiveColor, container: container,
				groupSlug: g.Slug,
				groupHost: g.Hostname,
				groupDB:   g.DB,
			})
		}
	}
	return rows
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// dashEntry is one selectable item in the dashboard list.
type dashEntry struct {
	name      string
	isGroup   bool
	hostname  string
	groupSlug string
	groupDB   *state.DB
	apps      []row
}

// dashEntries derives the ordered list of selectable items from loaded rows.
// Groups are collapsed to a single entry; singles each get their own.
func dashEntries(rows []row) []dashEntry {
	var out []dashEntry
	seen := map[string]bool{}
	for _, r := range rows {
		if r.isGroup() {
			if !seen[r.groupSlug] {
				seen[r.groupSlug] = true
				out = append(out, dashEntry{
					name: r.groupSlug, isGroup: true,
					hostname: r.groupHost, groupSlug: r.groupSlug,
					groupDB: r.groupDB,
				})
			}
			// append app to the last entry (the group we just added or already have)
			out[len(out)-1].apps = append(out[len(out)-1].apps, r)
		} else {
			out = append(out, dashEntry{name: r.slug, hostname: r.hostname, groupDB: r.groupDB})
		}
	}
	return out
}

func (r row) isGroup() bool { return r.groupSlug != "" }

// renderDashboard fills exactly `height` lines at `width` columns.
// inspectMap: per-entry map — true means show component list instead of stats.
func renderDashboard(rows []row, cursor int, inGroup bool, innerCursor int, width, height int, hi dockerx.HostInfo, cs map[string]dockerx.ContainerStats, inspectMap map[string]bool, detailMap map[string]bool) string {
	const outerPad = 1
	const gap = 2
	boxW := (width - 2*outerPad - 2*gap) / 3

	// aggregate totals from live container stats
	var totalCPU float64
	var totalMem uint64
	for _, s := range cs {
		totalCPU += s.CPUPerc
		totalMem += s.MemUsage
	}

	// CPU: fill = totalCPU / (cores * 100)
	cpuCap := float64(hi.CPUs) * 100
	cpuFill := 0.0
	if cpuCap > 0 {
		cpuFill = totalCPU / cpuCap
	}

	// Memory: fill = totalMem / hostMemTotal
	memFill := 0.0
	if hi.MemTotal > 0 {
		memFill = float64(totalMem) / float64(hi.MemTotal)
	}

	// Disk: fill = used / total
	diskUsed := hi.DiskTotal - hi.DiskFree
	diskFill := 0.0
	if hi.DiskTotal > 0 {
		diskFill = float64(diskUsed) / float64(hi.DiskTotal)
	}

	b1 := statBarBox("CPU", hi.CPULabel(), cpuFill,
		"0", fmt.Sprintf("[%.2f%%]", totalCPU), fmt.Sprintf("%d", hi.CPUs),
		"", boxW)
	b2 := statBarBox("Memory", hi.MemLabel(), memFill,
		"0", fmt.Sprintf("[%s]", fmtCap(totalMem)), hi.MemLabel(),
		"", boxW)
	b3 := statBarBox("Disk", hi.DiskLabel(), diskFill,
		"0", fmt.Sprintf("[%s used]", fmtCap(diskUsed)), hi.DiskLabel(),
		"", boxW)

	l1 := strings.Split(b1, "\n")
	l2 := strings.Split(b2, "\n")
	l3 := strings.Split(b3, "\n")
	sp := strings.Repeat(" ", gap)
	var lines []string
	lines = append(lines, "") // top margin
	for i := range l1 {
		lines = append(lines, strings.Repeat(" ", outerPad)+l1[i]+sp+l2[i]+sp+l3[i])
	}

	// app/group list
	lines = append(lines, "") // gap between stat boxes and list
	entries := dashEntries(rows)
	for i, e := range entries {
		inner := -1
		if inGroup && i == cursor {
			inner = innerCursor
		}
		isStats := !inspectMap[e.name] && !e.isGroup
		for _, l := range strings.Split(appListBox(e, i == cursor, inner, width-2*outerPad, isStats, cs, detailMap), "\n") {
			lines = append(lines, strings.Repeat(" ", outerPad)+l)
		}
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).Render(body)
}

// statBarBox renders a labelled braille-bar box of the given width.
// fill is 0.0–1.0. leftVal/centerVal/rightVal appear below the bar.
func statBarBox(label, subtitle string, fill float64, leftVal, centerVal, rightVal, peakLine string, boxW int) string {
	const padH = 2
	innerW := boxW - 2
	dotsW := innerW - 2*padH
	if dotsW < 1 {
		dotsW = 1
	}

	active := int(fill * float64(dotsW))
	if active > dotsW {
		active = dotsW
	}

	// top border: left half holds label + subtitle, right half is dashes
	half := innerW / 2
	labelStr := "─" + label
	if subtitle != "" {
		labelStr += "  " + subtitle
	}
	lw := len([]rune(labelStr))
	if lw < half {
		labelStr += strings.Repeat("─", half-lw)
	} else {
		labelStr = string([]rune(labelStr)[:half])
	}
	top := mutedStyle.Render("╭" + labelStr + strings.Repeat("─", innerW-half) + "╮")

	empty := mutedStyle.Render("│") + strings.Repeat(" ", innerW) + mutedStyle.Render("│")

	mid := mutedStyle.Render("│") +
		strings.Repeat(" ", padH) +
		lipgloss.NewStyle().Foreground(colAccent).Render(strings.Repeat("⣿", active)) +
		lipgloss.NewStyle().Foreground(colDim).Render(strings.Repeat("⣿", dotsW-active)) +
		strings.Repeat(" ", padH) +
		mutedStyle.Render("│")

	// value line: leftVal on left, rightVal on right, centerVal centered
	contentW := innerW - 2*padH
	lvw := len([]rune(leftVal))
	rvw := len([]rune(rightVal))
	cvw := len([]rune(centerVal))
	leftSpace := (contentW-cvw)/2 - lvw
	if leftSpace < 1 {
		leftSpace = 1
	}
	rightSpace := contentW - lvw - leftSpace - cvw - rvw
	if rightSpace < 1 {
		rightSpace = 1
	}
	valLine := mutedStyle.Render("│") +
		strings.Repeat(" ", padH) +
		mutedStyle.Render(leftVal) +
		strings.Repeat(" ", leftSpace) +
		mutedStyle.Render(centerVal) +
		strings.Repeat(" ", rightSpace) +
		mutedStyle.Render(rightVal) +
		strings.Repeat(" ", padH) +
		mutedStyle.Render("│")

	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	var peakRow string
	if peakLine != "" {
		pw := innerW - 2*padH
		ps := "peak " + peakLine
		leftPad := (pw - len([]rune(ps))) / 2
		if leftPad < 0 {
			leftPad = 0
		}
		rightPad := pw - len([]rune(ps)) - leftPad
		if rightPad < 0 {
			rightPad = 0
		}
		peakRow = mutedStyle.Render("│") +
			strings.Repeat(" ", padH+leftPad) +
			mutedStyle.Render(ps) +
			strings.Repeat(" ", rightPad+padH) +
			mutedStyle.Render("│")
	} else {
		peakRow = empty
	}

	return strings.Join([]string{top, empty, mid, valLine, empty, peakRow, bot}, "\n")
}

// fmtCap formats bytes as a short human-readable string (delegates to dockerx).
func fmtCap(b uint64) string { return dockerx.FmtCap(b) }

// fmtRate formats a bytes-per-minute rate as a short human-readable string.
func fmtRate(bpm float64) string {
	switch {
	case bpm >= 1024*1024:
		return fmt.Sprintf("%.1fMB/m", bpm/(1024*1024))
	case bpm >= 1024:
		return fmt.Sprintf("%.1fKB/m", bpm/1024)
	default:
		return fmt.Sprintf("%.0fB/m", bpm)
	}
}

// netBarBox renders a 6-line network box with a split braille bar.
// Down traffic fills left→right; up traffic fills right→left from the center.
// highRate is the bytes/min considered "full" — anything above pins at 100%.
func netBarBox(downRate, upRate, errPerc float64, boxW int) string {
	const padH = 2
	const highRate = 50.0 * 1024 * 1024 // 50 MB/min
	const gapCols = 2

	innerW := boxW - 2
	barW := innerW - 2*padH
	halfW := (barW - gapCols) / 2
	if halfW < 1 {
		halfW = 1
	}
	actualGap := barW - 2*halfW // absorbs odd remainder so mid row fills innerW exactly

	clamp := func(f float64) float64 {
		if f < 0 {
			return 0
		}
		if f > 1 {
			return 1
		}
		return f
	}
	downActive := int(clamp(downRate/highRate) * float64(halfW))
	upActive := int(clamp(upRate/highRate) * float64(halfW))

	// down: active on left side
	downBar := lipgloss.NewStyle().Foreground(colAccent).Render(strings.Repeat("⣿", downActive)) +
		lipgloss.NewStyle().Foreground(colDim).Render(strings.Repeat("⣿", halfW-downActive))
	// up: active on right side (fills inward from right)
	upBar := lipgloss.NewStyle().Foreground(colDim).Render(strings.Repeat("⣿", halfW-upActive)) +
		lipgloss.NewStyle().Foreground(colAccent).Render(strings.Repeat("⣿", upActive))

	title := "─ Network "
	topFill := innerW - len([]rune(title))
	if topFill < 0 {
		topFill = 0
	}
	top := mutedStyle.Render("╭" + title + strings.Repeat("─", topFill) + "╮")
	empty := mutedStyle.Render("│") + strings.Repeat(" ", innerW) + mutedStyle.Render("│")

	mid := mutedStyle.Render("│") +
		strings.Repeat(" ", padH) +
		downBar + strings.Repeat(" ", actualGap) + upBar +
		strings.Repeat(" ", padH) +
		mutedStyle.Render("│")

	downStr := mutedStyle.Render("↓ " + fmtRate(downRate))
	upStr := mutedStyle.Render("↑ " + fmtRate(upRate))
	valW := innerW - 2*padH - lipgloss.Width(downStr) - lipgloss.Width(upStr)
	if valW < 1 {
		valW = 1
	}
	valLine := mutedStyle.Render("│") +
		strings.Repeat(" ", padH) +
		downStr + strings.Repeat(" ", valW) + upStr +
		strings.Repeat(" ", padH) +
		mutedStyle.Render("│")

	bot := mutedStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	errStr := fmt.Sprintf("%.1f%% errors", errPerc)
	errW := innerW - 2*padH
	errLeftPad := (errW - len([]rune(errStr))) / 2
	if errLeftPad < 0 {
		errLeftPad = 0
	}
	errRightPad := errW - len([]rune(errStr)) - errLeftPad
	if errRightPad < 0 {
		errRightPad = 0
	}
	errStyle := mutedStyle
	if errPerc > 0 {
		errStyle = lipgloss.NewStyle().Foreground(colWarn)
	}
	errRow := mutedStyle.Render("│") +
		strings.Repeat(" ", padH+errLeftPad) +
		errStyle.Render(errStr) +
		strings.Repeat(" ", errRightPad+padH) +
		mutedStyle.Render("│")

	return strings.Join([]string{top, empty, mid, valLine, empty, errRow, bot}, "\n")
}

// appStatLines renders the three stat boxes (CPU / Memory / Network) for a
// container into lines that fit inside a box of innerW content columns.
// Each returned line is the content between the outer │ borders — callers
// must wrap them with the appropriate border characters.
// Returns an empty+statRows+empty slice (includes the surrounding blank rows).
func appStatLines(st dockerx.ContainerStats, innerW int) []string {
	const statOuterPad = 2
	const statGap = 1
	boxW := (innerW - 2*statOuterPad - 2*statGap) / 3
	boxW3 := innerW - 2*statOuterPad - 2*statGap - 2*boxW

	cpuFill := st.CPUPerc / 100
	if cpuFill > 1 {
		cpuFill = 1
	}
	memFill := 0.0
	if st.MemLimit > 0 {
		memFill = float64(st.MemUsage) / float64(st.MemLimit)
	}

	b1 := statBarBox("CPU", "", cpuFill,
		"0", fmt.Sprintf("[%.2f%%]", st.CPUPerc), "100",
		fmt.Sprintf("%.2f%%", st.CPUPeak), boxW)
	b2 := statBarBox("Memory", "", memFill,
		"0", "["+fmtCap(st.MemUsage)+"]", fmtCap(st.MemLimit),
		fmtCap(st.MemPeak), boxW)
	b3 := netBarBox(st.NetInRate, st.NetOutRate, st.NetErrPerc, boxW3)

	l1 := strings.Split(b1, "\n")
	l2 := strings.Split(b2, "\n")
	l3 := strings.Split(b3, "\n")
	sp := strings.Repeat(" ", statGap)
	pad := strings.Repeat(" ", statOuterPad)

	var out []string
	out = append(out, strings.Repeat(" ", innerW)) // top blank
	for i := range l1 {
		rowStr := pad + l1[i] + sp + l2[i] + sp + l3[i]
		rowPad := innerW - lipgloss.Width(rowStr)
		if rowPad < 0 {
			rowPad = 0
		}
		out = append(out, rowStr+strings.Repeat(" ", rowPad))
	}
	out = append(out, strings.Repeat(" ", innerW)) // bottom blank
	return out
}


// Groups always show their hostname header and app list.
// For singles: viewStats=true (default) shows container stats; false shows component list.
func appListBox(e dashEntry, selected bool, innerCursor int, width int, viewStats bool, cs map[string]dockerx.ContainerStats, detailMap map[string]bool) string {
	innerW := width - 2

	circle := lipgloss.NewStyle().Foreground(colAccent).Render("●")
	if !selected {
		circle = lipgloss.NewStyle().Foreground(colAccent).Render("○")
	}

	// top border label: hostname (groupSlug) for groups, hostname (name) for singles
	var topName string
	if e.isGroup {
		topName = e.hostname + " (" + e.groupSlug + ")"
	} else {
		topName = e.hostname + " (" + e.name + ")"
	}
	nameRunes := len([]rune(topName))
	dashCount := width - 5 - nameRunes
	if dashCount < 0 {
		dashCount = 0
	}
	top := mutedStyle.Render("╭─") + circle + mutedStyle.Render(" "+topName+strings.Repeat("─", dashCount)+"╮")

	var bodyLines []string

	if e.isGroup {
		// blank breathing room
		bodyLines = append(bodyLines, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))

		// build selectable items: db first (if present), then apps
		type item struct {
			label string
		}
		var items []item
		if e.groupDB != nil {
			db := e.groupDB
			items = append(items, item{
				label: warnStyle.Render(db.Engine+":"+db.Version),
			})
		}
		for _, r := range e.apps {
			appName := r.slug[strings.LastIndex(r.slug, "/")+1:]
			items = append(items, item{
				label: lipgloss.NewStyle().Foreground(colFg).Render(appName) +
					"  " + mutedStyle.Render("("+r.path+")"),
			})
		}

		for i, it := range items {
			itemCircle := lipgloss.NewStyle().Foreground(colAccent).Render("○")
			if i == innerCursor {
				itemCircle = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			}
			dbOffset := 0
			if e.groupDB != nil {
				dbOffset = 1
			}
			var labelStr string
			if i == innerCursor {
				// re-render name bold for apps (db label stays as-is)
				if i >= dbOffset {
					r := e.apps[i-dbOffset]
					appName := r.slug[strings.LastIndex(r.slug, "/")+1:]
					labelStr = lipgloss.NewStyle().Bold(true).Foreground(colFg).Render(appName) +
						"  " + mutedStyle.Render("("+r.path+")")
				} else {
					labelStr = it.label
				}
			} else {
				labelStr = it.label
			}
			line := "  " + itemCircle + " " + labelStr
			padding := innerW - lipgloss.Width(line)
			if padding < 0 {
				padding = 0
			}
			bodyLines = append(bodyLines, mutedStyle.Render("│")+line+strings.Repeat(" ", padding)+mutedStyle.Render("│"))

			// inline braille stats when detail is toggled
			if i >= dbOffset {
				r := e.apps[i-dbOffset]
				if r.container != "" && detailMap[r.container] {
					if st, ok := cs[r.container]; ok {
						for _, content := range appStatLines(st, innerW) {
							bodyLines = append(bodyLines, mutedStyle.Render("│")+content+mutedStyle.Render("│"))
						}
					}
				}
			}
		}

		bodyLines = append(bodyLines, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))
	} else if viewStats {
		// --- container stats view ---
		var st dockerx.ContainerStats
		for name, s := range cs {
			if strings.HasPrefix(name, "boxx-app-"+e.name+"-") {
				st = s
				break
			}
		}
		for _, content := range appStatLines(st, innerW) {
			bodyLines = append(bodyLines, mutedStyle.Render("│")+content+mutedStyle.Render("│"))
		}
	} else {
		bodyLines = append(bodyLines, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))
		if e.groupDB != nil {
			db := e.groupDB
			itemCircle := lipgloss.NewStyle().Foreground(colAccent).Render("○")
			if innerCursor == 0 {
				itemCircle = lipgloss.NewStyle().Foreground(colAccent).Render("●")
			}
			labelStr := warnStyle.Render(db.Engine + ":" + db.Version)
			line := "  " + itemCircle + " " + labelStr
			padding := innerW - lipgloss.Width(line)
			if padding < 0 {
				padding = 0
			}
			bodyLines = append(bodyLines, mutedStyle.Render("│")+line+strings.Repeat(" ", padding)+mutedStyle.Render("│"))
			bodyLines = append(bodyLines, mutedStyle.Render("│")+strings.Repeat(" ", innerW)+mutedStyle.Render("│"))
		}
	}

	bot := mutedStyle.Render("╰"+strings.Repeat("─", innerW)+"╯")
	return strings.Join(append([]string{top}, append(bodyLines, bot)...), "\n")
}

// padBetween fills `left` and `right` with spaces to fill `width` columns total.
func padBetween(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	pad := width - lw - rw
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}
