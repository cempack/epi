package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cempack/epi/internal/epitech"
	"github.com/cempack/epi/internal/workspace"
)

// Action is what the dashboard asks main to do after it closes.
type Action int

const (
	ActNone   Action = iota
	ActCd            // print the selected path (for `cd "$(epi)"`)
	ActNew           // launch the new-project wizard
	ActDoctor        // run the doctor
)

type DashResult struct {
	Action Action
	Path   string
}

type row struct {
	module  *workspace.Module // header row when project == nil
	project *workspace.Project
}

type dashModel struct {
	root    string
	mods    []*workspace.Module
	rows    []row
	cursor  int // index into rows; always on a project row
	offset  int // first visible row
	w, h    int
	filter  textinput.Model
	editing bool
	flash   string
	result  DashResult
}

func RunDashboard(root string) (DashResult, error) {
	mods, err := workspace.Scan(root)
	if err != nil {
		return DashResult{}, err
	}
	ti := textinput.New()
	ti.Prompt = sMuted.Render("/ ")
	ti.Placeholder = "filter projects"
	ti.PlaceholderStyle = sFaint
	ti.TextStyle = sText
	m := &dashModel{root: root, mods: mods, filter: ti}
	m.rebuild()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	if _, err := p.Run(); err != nil {
		return DashResult{}, err
	}
	return m.result, nil
}

// rebuild flattens modules into rows, applying the filter.
func (m *dashModel) rebuild() {
	q := epitech.Squash(m.filter.Value())
	m.rows = m.rows[:0]
	for _, mod := range m.mods {
		var ps []row
		for _, p := range mod.Projects {
			if q == "" || strings.Contains(epitech.Squash(p.Name+mod.Dir+mod.Tek), q) {
				ps = append(ps, row{module: mod, project: p})
			}
		}
		if len(ps) > 0 {
			m.rows = append(m.rows, row{module: mod})
			m.rows = append(m.rows, ps...)
		}
	}
	m.cursor = 0
	m.offset = 0
	m.move(0)
}

// move steps the cursor by d, skipping header rows.
func (m *dashModel) move(d int) {
	if len(m.rows) == 0 {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}
	i := m.cursor
	for n := 0; n < abs(d) || m.rows[i].project == nil; {
		next := i + step
		if next < 0 || next >= len(m.rows) {
			break
		}
		i = next
		if m.rows[i].project != nil {
			n++
		}
	}
	if m.rows[i].project == nil { // stuck on a header at the edge: search forward
		for j := range m.rows {
			if m.rows[j].project != nil {
				i = j
				break
			}
		}
	}
	m.cursor = i
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m *dashModel) selected() *workspace.Project {
	if m.cursor < len(m.rows) {
		return m.rows[m.cursor].project
	}
	return nil
}

func (m *dashModel) Init() tea.Cmd { return nil }

func (m *dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "esc":
				m.editing = false
				m.filter.Blur()
				m.filter.SetValue("")
				m.rebuild()
			case "enter":
				m.editing = false
				m.filter.Blur()
			case "up", "down":
				m.editing = false
				m.filter.Blur()
				return m.Update(msg)
			default:
				var cmd tea.Cmd
				m.filter, cmd = m.filter.Update(msg)
				m.rebuild()
				return m, cmd
			}
			return m, nil
		}
		m.flash = ""
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "g", "home":
			m.cursor = 0
			m.move(0)
		case "G", "end":
			m.cursor = len(m.rows) - 1
			m.move(0)
		case "/":
			m.editing = true
			return m, m.filter.Focus()
		case "enter":
			if p := m.selected(); p != nil {
				m.result = DashResult{Action: ActCd, Path: p.Path}
				return m, tea.Quit
			}
		case "o":
			m.openSubject()
		case "w":
			m.openWeb()
		case "n":
			m.result = DashResult{Action: ActNew}
			return m, tea.Quit
		case "d":
			m.result = DashResult{Action: ActDoctor}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *dashModel) openSubject() {
	p := m.selected()
	if p == nil || len(p.Subjects) == 0 {
		m.flash = sYellow.Render("no subject filed for this project")
		return
	}
	pick := p.Subjects[0]
	for _, s := range p.Subjects { // prefer the main subject over bootstrap
		if strings.TrimSuffix(filepath.Base(s), filepath.Ext(s)) == p.Name {
			pick = s
		}
	}
	_ = exec.Command("xdg-open", pick).Start()
	m.flash = sMuted.Render("opened ") + sText.Render(filepath.Base(pick))
}

func (m *dashModel) openWeb() {
	p := m.selected()
	if p == nil || p.Git.Remote == "" {
		m.flash = sYellow.Render("no remote")
		return
	}
	url := "https://github.com/" + workspace.RemoteName(p.Git.Remote)
	_ = exec.Command("xdg-open", url).Start()
	m.flash = sMuted.Render("opened ") + sText.Render("GitHub")
}

// ---------------------------------------------------------------- view

func statusDot(g workspace.Git) string {
	switch {
	case !g.IsRepo:
		return sFaint.Render("○")
	case g.Ahead > 0:
		return sAccent.Render("↑")
	case g.Dirty > 0 || g.Untracked > 0:
		return sYellow.Render("●")
	}
	return sFaint.Render("●")
}

func (m *dashModel) View() string {
	if m.w == 0 {
		return ""
	}
	// Header
	total, attention := 0, 0
	for _, mod := range m.mods {
		for _, p := range mod.Projects {
			total++
			if p.Git.IsRepo && !p.Git.Clean() {
				attention++
			}
		}
	}
	stats := sMuted.Render(fmt.Sprintf("%d projects · %d modules", total, len(m.mods)))
	if attention > 0 {
		stats += sMuted.Render(" · ") + sYellow.Render(fmt.Sprintf("● %d need attention", attention))
	}
	home, _ := os.UserHomeDir()
	where := sFaint.Render(strings.Replace(m.root, home, "~", 1))
	left := " " + Header("") + "  " + where
	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(stats)-1, 1)
	header := left + strings.Repeat(" ", gap) + stats + "\n" + Rule(m.w)

	// Footer
	keys := [][2]string{{"↑↓", "move"}, {"enter", "cd"}, {"o", "subject"}, {"w", "github"}, {"n", "new"}, {"d", "doctor"}, {"/", "filter"}, {"q", "quit"}}
	var kb []string
	for _, k := range keys {
		kb = append(kb, sText.Render(k[0])+" "+sMuted.Render(k[1]))
	}
	footer := " " + strings.Join(kb, "   ")
	if m.editing || m.filter.Value() != "" {
		footer = " " + m.filter.View()
	}
	if m.flash != "" {
		footer = " " + m.flash
	}

	bodyH := max(m.h-4, 3)
	listW := min(max(m.w*2/5, 34), 48)
	detailW := m.w - listW - 3

	list := m.viewList(listW, bodyH)
	sep := sFaint.Render(strings.TrimRight(strings.Repeat("│\n", bodyH), "\n"))
	detail := m.viewDetail(detailW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, " ", sep, " ", detail)

	return header + "\n" + body + "\n" + footer
}

func (m *dashModel) viewList(w, h int) string {
	if len(m.rows) == 0 {
		return lipgloss.NewStyle().Width(w).Height(h).Render("\n  " + sMuted.Render("nothing matches"))
	}
	// Render every row first, remembering which screen line the cursor is on.
	// Scrolling is then done in screen lines, which include the blank spacers.
	var lines []string
	cursorLine := 0
	for i, r := range m.rows {
		if r.project == nil {
			title := strings.ToUpper(r.module.Code())
			label := ""
			if r.module.Label() != r.module.Dir {
				label = " " + sMuted.Render(r.module.Label())
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, " "+sBold.Render(title)+label+sFaint.Render(" "+r.module.Tek))
			continue
		}
		p := r.project
		ago := Ago(p.Git.LastCommit)
		name := p.Name
		nameW := w - 9 - len([]rune(ago))
		if len([]rune(name)) > nameW {
			name = string([]rune(name)[:nameW-1]) + "…"
		}
		pad := strings.Repeat(" ", max(nameW-len([]rune(name)), 0))
		if i == m.cursor {
			cursorLine = len(lines)
			bar := lipgloss.NewStyle().Foreground(cAccent).Render("▌")
			lines = append(lines, bar+" "+statusDot(p.Git)+" "+sBold.Foreground(cAccent).Render(name)+pad+"  "+sMuted.Render(ago))
		} else {
			lines = append(lines, "  "+statusDot(p.Git)+" "+sText.Render(name)+pad+"  "+sFaint.Render(ago))
		}
	}
	// Keep two lines of context above the cursor (its module header) and
	// one below, then clamp to the content.
	if cursorLine-2 < m.offset {
		m.offset = max(cursorLine-2, 0)
	}
	if cursorLine+1 >= m.offset+h {
		m.offset = cursorLine + 2 - h
	}
	m.offset = max(min(m.offset, len(lines)-h), 0)
	end := min(m.offset+h, len(lines))
	return lipgloss.NewStyle().Width(w).Height(h).Render(strings.Join(lines[m.offset:end], "\n"))
}

func (m *dashModel) viewDetail(w, h int) string {
	p := m.selected()
	if p == nil {
		return ""
	}
	g := p.Git
	home, _ := os.UserHomeDir()
	var b strings.Builder

	b.WriteString("\n" + sBold.Render(p.Name) + "\n")
	mod := p.Module.Dir
	if r, err := epitech.Parse(g.Remote); err == nil {
		mod += sFaint.Render(" · ") + sMuted.Render(r.ModuleID())
	}
	b.WriteString(sMuted.Render(mod) + "\n")
	b.WriteString(sFaint.Render(strings.Replace(p.Path, home, "~", 1)) + "\n\n")

	field := func(k, v string) { b.WriteString(sFaint.Render(fmt.Sprintf("%-9s", k)) + v + "\n") }

	if !g.IsRepo {
		field("git", sFaint.Render("not a repository"))
	} else {
		var st []string
		if g.Clean() {
			st = append(st, sMuted.Render("clean"))
		}
		if g.Dirty > 0 {
			st = append(st, sYellow.Render(fmt.Sprintf("%d modified", g.Dirty)))
		}
		if g.Untracked > 0 {
			st = append(st, sYellow.Render(fmt.Sprintf("%d untracked", g.Untracked)))
		}
		if g.Ahead > 0 {
			st = append(st, sAccent.Render(fmt.Sprintf("↑%d unpushed", g.Ahead)))
		}
		if g.Behind > 0 {
			st = append(st, sMuted.Render(fmt.Sprintf("↓%d behind", g.Behind)))
		}
		field("status", strings.Join(st, sFaint.Render(" · ")))
		field("branch", sText.Render(g.Branch))
		if !g.LastCommit.IsZero() {
			msg := g.LastMsg
			if maxW := w - 20; maxW > 10 && len([]rune(msg)) > maxW {
				msg = string([]rune(msg)[:maxW-1]) + "…"
			}
			field("commit", sMuted.Render(Ago(g.LastCommit)+" ago")+"  "+sText.Render(msg))
		} else {
			field("commit", sFaint.Render("no commits yet"))
		}
		if g.Remote != "" {
			field("remote", sMuted.Render(workspace.RemoteName(g.Remote)))
		}
	}

	b.WriteString("\n" + sFaint.Render("subjects") + "\n")
	if len(p.Subjects) == 0 {
		b.WriteString(sFaint.Render("  none filed · drop the PDF in ~/Downloads and run ") + sMuted.Render("epi doctor") + "\n")
	}
	for _, s := range p.Subjects {
		b.WriteString("  " + sMuted.Render("▤ ") + sText.Render(filepath.Base(s)) + "\n")
	}
	return lipgloss.NewStyle().Width(w).MaxHeight(h).Render(b.String())
}
