package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Colors are ANSI indexes, not hex: the terminal theme decides what they
// look like, so epi always matches the desktop theme.
//
// Rule: color carries meaning. Plain text is the default; one accent marks
// focus; green/yellow/red only appear for status.
var (
	cText   = lipgloss.NoColor{}
	cMuted  = lipgloss.Color("8")
	cAccent = lipgloss.Color("4")
	cGreen  = lipgloss.Color("2")
	cYellow = lipgloss.Color("3")
	cRed    = lipgloss.Color("1")
)

var (
	sText   = lipgloss.NewStyle()
	sBold   = lipgloss.NewStyle().Bold(true)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sFaint  = lipgloss.NewStyle().Foreground(cMuted).Faint(true)
	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sGreen  = lipgloss.NewStyle().Foreground(cGreen)
	sYellow = lipgloss.NewStyle().Foreground(cYellow)
	sRed    = lipgloss.NewStyle().Foreground(cRed)

	sCard = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cMuted).
		Padding(0, 2)
)

// Rule is a thin divider line.
func Rule(width int) string { return sFaint.Render(strings.Repeat("─", width)) }

// Header is the one-line title used at the top of every screen: "epi  new project".
func Header(sub string) string {
	if sub == "" {
		return sBold.Render("epi")
	}
	return sBold.Render("epi") + sMuted.Render("  "+sub)
}

// Theme is a huh form theme built on the terminal's own colors.
func Theme() *huh.Theme {
	t := huh.ThemeBase16()
	t.Focused.Base = t.Focused.Base.BorderForeground(cMuted)
	t.Focused.Title = lipgloss.NewStyle().Bold(true)
	t.Focused.NoteTitle = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	t.Focused.Description = sMuted
	t.Focused.SelectSelector = sAccent.SetString("› ")
	t.Focused.MultiSelectSelector = sAccent.SetString("› ")
	t.Focused.Option = sText
	t.Focused.SelectedOption = sText
	t.Focused.UnselectedOption = sMuted
	t.Focused.SelectedPrefix = sAccent.SetString("● ")
	t.Focused.UnselectedPrefix = sFaint.SetString("○ ")
	t.Focused.TextInput.Prompt = sAccent.SetString("› ")
	t.Focused.TextInput.Cursor = sAccent
	t.Focused.TextInput.Placeholder = sFaint
	t.Focused.FocusedButton = lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Reverse(true).Bold(true)
	t.Focused.BlurredButton = lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Foreground(cMuted)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.ErrorIndicator = sRed
	t.Focused.ErrorMessage = sRed

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Title = sMuted
	t.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
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
