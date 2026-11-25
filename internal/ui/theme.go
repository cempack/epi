package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

// Colors are ANSI indexes, not hex: the terminal theme decides what they
// look like, so epi always matches the user's desktop theme.
//
// Rule: color carries meaning. Plain text is the default; accent marks focus
// and selection; green/yellow/red appear only for status and badges.
// re draws on stderr, where the UI goes. Styles must be built from it: a
// style keeps the renderer it was made with, and package-level styles are
// made before main runs, when the default renderer still watches stdout.
var re = lipgloss.NewRenderer(os.Stderr)

var (
	cText    = lipgloss.NoColor{}
	cMuted   = lipgloss.Color("8")
	cAccent  = lipgloss.Color("4")
	cCyan    = lipgloss.Color("6")
	cGreen   = lipgloss.Color("2")
	cYellow  = lipgloss.Color("3")
	cRed     = lipgloss.Color("1")
	cSurface = lipgloss.Color("0")
)

var (
	sText   = re.NewStyle()
	sBold   = re.NewStyle().Bold(true)
	sMuted  = re.NewStyle().Foreground(cMuted)
	sFaint  = re.NewStyle().Foreground(cMuted).Faint(true)
	sAccent = re.NewStyle().Foreground(cAccent)
	sCyan   = re.NewStyle().Foreground(cCyan)
	sGreen  = re.NewStyle().Foreground(cGreen)
	sYellow = re.NewStyle().Foreground(cYellow)
	sRed    = re.NewStyle().Foreground(cRed)

	// Brand pill for the top-left corner: compact, high contrast, theme-aware.
	sBrand = re.NewStyle().
		Bold(true).
		Foreground(cSurface).
		Background(cAccent).
		Padding(0, 1)

	// Key cap style for footer & inline shortcut hints.
	sKey = re.NewStyle().
		Bold(true).
		Foreground(cAccent)

	sCard = re.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cMuted).
		Padding(0, 2)

	sPanel = re.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cMuted).
		Padding(0, 1)

	sPanelFocused = re.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cAccent).
			Padding(0, 1)
)

// Rule is a thin divider line.
func Rule(width int) string {
	if width <= 0 {
		return ""
	}
	return sFaint.Render(strings.Repeat("─", width))
}

// Header is the one-line title used at the top of every CLI screen.
func Header(sub string) string {
	if sub == "" {
		return sBrand.Render("epi")
	}
	return sBrand.Render("epi") + " " + sBold.Render(sub)
}

// Pill renders a soft status badge like `[clean]` or `[bootstrap]`.
func Pill(label string, col lipgloss.TerminalColor) string {
	return re.NewStyle().
		Foreground(col).
		Bold(true).
		Render("[" + label + "]")
}

// Tag renders an inverted compact pill for high-signal labels.
func Tag(label string, bg lipgloss.TerminalColor) string {
	return re.NewStyle().
		Foreground(cSurface).
		Background(bg).
		Bold(true).
		Padding(0, 1).
		Render(label)
}

// KeyHint renders a crisp key + action pair.
func KeyHint(key, label string) string {
	return sKey.Render(key) + " " + sMuted.Render(label)
}

// SectionBox renders a rounded panel with a subtle title header inside,
// bounded cleanly to width.
func SectionBox(title, body string, width int, focused bool) string {
	innerW := max(width-4, 16)
	st := sPanel
	titleStyle := sMuted.Bold(true)
	if focused {
		st = sPanelFocused
		titleStyle = sAccent.Bold(true)
	}
	content := strings.TrimRight(body, "\n")
	if title != "" {
		content = titleStyle.Render(title) + "\n" + content
	}
	return st.Width(innerW).Render(content)
}

// IndentBox renders a SectionBox with a consistent 1-space left margin on every line.
func IndentBox(title, body string, width int, focused bool) string {
	return re.NewStyle().MarginLeft(1).Render(SectionBox(title, body, width, focused))
}

// SectionHeading renders a clean CLI section title with optional count and rule.
func SectionHeading(title string, count int) string {
	cnt := ""
	if count >= 0 {
		cnt = " " + sFaint.Render(fmt.Sprintf("(%d)", count))
	}
	return " " + sBold.Render(title) + cnt
}

// TermWidth returns the stderr terminal width, defaulting to 80.
func TermWidth() int {
	w, _, err := term.GetSize(os.Stderr.Fd())
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// Theme is a huh form theme built on the terminal's own colors.
func Theme() *huh.Theme {
	t := huh.ThemeBase16()
	t.Focused.Base = t.Focused.Base.BorderForeground(cAccent).PaddingLeft(1)
	t.Focused.Title = re.NewStyle().Bold(true)
	t.Focused.NoteTitle = re.NewStyle().Bold(true).MarginBottom(1)
	t.Focused.Description = sMuted
	t.Focused.SelectSelector = sAccent.SetString("› ")
	t.Focused.MultiSelectSelector = sAccent.SetString("› ")
	t.Focused.Option = sText
	t.Focused.SelectedOption = sBold
	t.Focused.UnselectedOption = sMuted
	t.Focused.SelectedPrefix = sAccent.SetString("● ")
	t.Focused.UnselectedPrefix = sFaint.SetString("○ ")
	t.Focused.TextInput.Prompt = sAccent.SetString("› ")
	t.Focused.TextInput.Cursor = sAccent
	t.Focused.TextInput.Placeholder = sFaint
	t.Focused.FocusedButton = re.NewStyle().Padding(0, 2).MarginRight(1).Foreground(cSurface).Background(cAccent).Bold(true)
	t.Focused.BlurredButton = re.NewStyle().Padding(0, 2).MarginRight(1).Foreground(cMuted)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.ErrorIndicator = sRed
	t.Focused.ErrorMessage = sRed

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Title = sMuted
	t.Blurred.SelectSelector = re.NewStyle().SetString("  ")
	t.Blurred.MultiSelectSelector = re.NewStyle().SetString("  ")
	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}

// Ago is a compact relative time: "3d", "2h", "now".
func Ago(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := int64(time.Since(t).Seconds())
	switch {
	case d < 60:
		return "now"
	case d < 3600:
		return strconv.FormatInt(d/60, 10) + "m"
	case d < 86400:
		return strconv.FormatInt(d/3600, 10) + "h"
	case d < 86400*30:
		return strconv.FormatInt(d/86400, 10) + "d"
	case d < 86400*365:
		return strconv.FormatInt(d/(86400*30), 10) + "mo"
	}
	return strconv.FormatInt(d/(86400*365), 10) + "y"
}
