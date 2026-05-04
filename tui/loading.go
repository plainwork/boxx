package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- tick ---------------------------------------------------------------

type loadingTickMsg struct{}

func tickLoading() tea.Cmd {
	return tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg {
		return loadingTickMsg{}
	})
}

// ---- braille animation --------------------------------------------------

const (
	loadingModalW = 72
	loadingInnerW = loadingModalW - 2
	loadingCols   = loadingInnerW - 4 // 2-space pad each side
)

// triangleWave returns a value in [0, period/2) that rises then falls.
func triangleWave(x, period int) int {
	x = ((x % period) + period) % period
	half := period / 2
	if x < half {
		return x
	}
	return period - 1 - x
}

// kittGlyph returns a braille rune that slowly shifts with position and frame.
func kittGlyph(c, frame int) rune {
	seed := ((c * 31) ^ (frame / 4 * 17)) & 0xFF
	if seed < 16 {
		seed += 0x50
	}
	return rune(0x2800 | seed)
}

var (
	kittBright = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#93c5fd", Dark: "#93c5fd"}) // lightest blue
	kittMid    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#3b82f6"}) // medium blue
	kittFaint  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1e3a8a", Dark: "#1e40af"}) // dark blue
	kittBg     = lipgloss.NewStyle().Foreground(colDim)
)

// brailleRow renders the KITT scanner: a blue gradient that bounces left↔right.
func brailleRow(frame int) string {
	period := loadingCols * 2
	head := triangleWave(frame*4, period) // full round-trip ≈ 2.3 s at 70 ms/tick

	var sb strings.Builder
	for c := 0; c < loadingCols; c++ {
		g := string(kittGlyph(c, frame))
		dist := c - head
		if dist < 0 {
			dist = -dist
		}
		switch {
		case dist == 0:
			sb.WriteString(kittBright.Render("⣿"))
		case dist <= 2:
			sb.WriteString(kittBright.Render(g))
		case dist <= 5:
			sb.WriteString(kittMid.Render(g))
		case dist <= 9:
			sb.WriteString(kittFaint.Render(g))
		default:
			sb.WriteString(kittBg.Render(g))
		}
	}
	return sb.String()
}

// ---- phrases ------------------------------------------------------------

var loadingPhrases = []string{
	"engaging turbo boost",
	"scanning for pursuit vehicles",
	"loading molecular bonded shell",
	"calibrating voice synthesizer",
	"accessing KITT mainframe",
	"charging flux capacitor",
	"rerouting power to deflector shields",
	"syncing with the mother ship",
	"recalculating escape vector",
	"engaging stealth mode",
	"hacking the gibson",
	"dialing into the BBS",
	"rewinding the tape",
	"be excellent to each other",
	"initializing self-destruct override",
	"compiling BASIC subroutines",
	"boosting the signal",
	"you wouldn't download a car",
	"don't worry, Michael",
	"stay frosty",
}

// ---- renderer -----------------------------------------------------------

func renderLoadingModal(frame int, label, step string, width, height int) string {
	phraseIdx := (frame / 30) % len(loadingPhrases) // change every ~2.1s
	phrase := mutedStyle.Render(loadingPhrases[phraseIdx])

	topLine := mutedStyle.Render(label)

	// step line: shows live progress, or falls back to the rotating phrase
	var stepLine string
	if step != "" {
		// truncate to avoid wrapping
		r := []rune(step)
		maxW := loadingCols
		if len(r) > maxW {
			r = append(r[:maxW-1], '…')
		}
		stepLine = mutedStyle.Render(string(r))
	} else {
		stepLine = phrase
	}

	block := lipgloss.JoinVertical(lipgloss.Center,
		topLine,
		"",
		brailleRow(frame),
		"",
		stepLine,
	)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block,
		lipgloss.WithWhitespaceChars(" "),
	)
}
