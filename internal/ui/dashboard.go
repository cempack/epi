package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	ActNone Action = iota
	ActCd          // print the selected path (for `cd "$(epi)"`)
	ActNew         // launch the new-project wizard
	ActDoctor      // run the doctor
	ActSubject     // file subjects for the selected project
	ActBootstrap   // set up bootstrap/ for the selected project
	ActPredeliver  // run epitech-predeliver on the selected project
	ActEditor      // open $EDITOR in the selected project
	ActLazygit     // open lazygit in the selected project
)

type DashResult struct {
	Action  Action
	Path    string
	Project string
}

type pickMode int

const (
	pickNone pickMode = iota
	pickSubject
	pickDocs
	pickHelp
)

type row struct {
	module    *workspace.Module // header row when project == nil
	project   *workspace.Project
	collapsed bool // true for a selectable collapsed module header
}

type fetchedMsg struct {
	mods   []*workspace.Module
	behind int
	errs   int
}

type dashModel struct {
	root       string
	mods       []*workspace.Module
	multiTek   bool // true if projects span tek1 + tek2+
	docs       []string
	rows       []row
	cursor     int // index into rows; always on a project or collapsed module row
	offset     int // first visible screen line
	totalLines int // total rendered list lines (for scrollbar)
	w, h       int
	filter     textinput.Model
	editing    bool
	recent     bool            // sort flat by latest activity instead of by module
	collapsed  map[string]bool // module.Path -> collapsed
	picking    pickMode
	pickCursor int
	fetching   bool
	flash      string
	result     DashResult
}

func newDashModel(root string, mods []*workspace.Module) *dashModel {
	ti := textinput.New()
	ti.Prompt = sAccent.Render("/ ")
	ti.Placeholder = "type to filter projects or modules…"
	ti.PlaceholderStyle = sFaint
	ti.TextStyle = sText

	teks := map[string]bool{}
	for _, mod := range mods {
		teks[mod.Tek] = true
	}
	m := &dashModel{
		root:      root,
		mods:      mods,
		multiTek:  len(teks) > 1,
		docs:      workspace.ScanDocs(root),
		filter:    ti,
		collapsed: map[string]bool{},
	}
	m.rebuild()
	return m
}

func RunDashboard(root string) (DashResult, error) {
	mods, err := workspace.Scan(root)
	if err != nil {
		return DashResult{}, err
	}
	m := newDashModel(root, mods)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	if _, err := p.Run(); err != nil {
		return DashResult{}, err
	}
	return m.result, nil
}

func (r row) selectable() bool { return r.project != nil || r.collapsed }

// rebuild flattens modules into rows, applying the filter, view mode and collapse state.
func (m *dashModel) rebuild() {
	prevPath := ""
	if p := m.selected(); p != nil {
		prevPath = p.Path
	} else if mod := m.selectedModule(); mod != nil {
		prevPath = mod.Path
	}

	q := epitech.Squash(m.filter.Value())
	m.rows = m.rows[:0]

	if m.recent {
		var all []*workspace.Project
		for _, mod := range m.mods {
			for _, p := range mod.Projects {
				if q == "" || strings.Contains(epitech.Squash(p.Name+mod.Dir+mod.Tek), q) {
					all = append(all, p)
				}
			}
		}
		sort.SliceStable(all, func(i, j int) bool {
			return all[i].LatestActivity().After(all[j].LatestActivity())
		})
		if len(all) > 0 {
			m.rows = append(m.rows, row{}) // virtual "RECENT" header
			for _, p := range all {
				m.rows = append(m.rows, row{module: p.Module, project: p})
			}
		}
	} else {
		for _, mod := range m.mods {
			var ps []row
			for _, p := range mod.Projects {
				if q == "" || strings.Contains(epitech.Squash(p.Name+mod.Dir+mod.Tek), q) {
					ps = append(ps, row{module: mod, project: p})
				}
			}
			if len(ps) == 0 {
				continue
			}
			if q == "" && m.collapsed[mod.Path] {
				m.rows = append(m.rows, row{module: mod, collapsed: true})
				continue
			}
			m.rows = append(m.rows, row{module: mod})
			m.rows = append(m.rows, ps...)
		}
	}

	m.cursor = 0
	m.offset = 0
	if prevPath != "" {
		for i, r := range m.rows {
			if (r.project != nil && r.project.Path == prevPath) || (r.collapsed && r.module.Path == prevPath) {
				m.cursor = i
				return
			}
		}
	}
	m.move(0)
}

// move steps the cursor by d, skipping non-selectable header rows.
func (m *dashModel) move(d int) {
	if len(m.rows) == 0 {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}
	i := m.cursor
	if i < 0 {
		i = 0
	}
	if i >= len(m.rows) {
		i = len(m.rows) - 1
	}
	for n := 0; n < abs(d) || !m.rows[i].selectable(); {
		next := i + step
		if next < 0 || next >= len(m.rows) {
			break
		}
		i = next
		if m.rows[i].selectable() {
			n++
		}
	}
	if !m.rows[i].selectable() {
		for j := range m.rows {
			if m.rows[j].selectable() {
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
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].project
	}
	return nil
}

func (m *dashModel) selectedModule() *workspace.Module {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].module
	}
	return nil
}

func (m *dashModel) Init() tea.Cmd { return nil }

func fetchCmd(root string) tea.Cmd {
	return func() tea.Msg {
		mods, err := workspace.Scan(root)
		if err != nil {
			return fetchedMsg{errs: 1}
		}
		results := workspace.FetchAll(mods)
		errs, behind := 0, 0
		for _, r := range results {
			if r.Err != nil {
				errs++
			}
			if r.Project.Git.Behind > 0 {
				behind++
			}
		}
		return fetchedMsg{mods: mods, behind: behind, errs: errs}
	}
}

func (m *dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case fetchedMsg:
		m.fetching = false
		if msg.mods != nil {
			m.mods = msg.mods
			m.rebuild()
		}
		switch {
		case msg.behind > 0:
			m.flash = sCyan.Render(fmt.Sprintf("↓ fetched · %d project(s) behind origin", msg.behind))
		case msg.errs > 0:
			m.flash = sYellow.Render(fmt.Sprintf("fetched remotes (%d failed)", msg.errs))
		default:
			m.flash = sGreen.Render("✓ remotes up to date")
		}
	case tea.KeyMsg:
		if m.picking == pickHelp {
			switch msg.String() {
			case "?", "esc", "q", "enter":
				m.picking = pickNone
			}
			return m, nil
		}
		if m.picking != pickNone {
			return m.updatePicker(msg)
		}
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
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.filter.Value() != "" {
				m.filter.SetValue("")
				m.rebuild()
				return m, nil
			}
			return m, tea.Quit
		case "?":
			m.picking = pickHelp
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
		case "t":
			m.recent = !m.recent
			m.rebuild()
			if m.recent {
				m.flash = sMuted.Render("view: recent activity")
			} else {
				m.flash = sMuted.Render("view: grouped by module")
			}
		case " ", "tab":
			if !m.recent && m.filter.Value() == "" {
				if mod := m.selectedModule(); mod != nil {
					m.collapsed[mod.Path] = !m.collapsed[mod.Path]
					m.rebuild()
				}
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].collapsed {
				mod := m.rows[m.cursor].module
				m.collapsed[mod.Path] = false
				m.rebuild()
				return m, nil
			}
			if p := m.selected(); p != nil {
				m.result = DashResult{Action: ActCd, Path: p.Path}
				return m, tea.Quit
			}
		case "b":
			if p := m.selected(); p != nil {
				if p.Bootstrap.HasDir {
					m.result = DashResult{Action: ActCd, Path: p.Bootstrap.Dir}
					return m, tea.Quit
				}
				m.result = DashResult{Action: ActBootstrap, Path: p.Path, Project: p.Name}
				return m, tea.Quit
			}
		case "B":
			m.openBootstrapSubject()
		case "o":
			m.openSubject()
		case "s":
			if p := m.selected(); p != nil {
				m.result = DashResult{Action: ActSubject, Path: p.Path, Project: p.Name}
				return m, tea.Quit
			}
		case "w":
			m.openWeb()
		case "D":
			m.openDocs()
		case "e":
			if p := m.selected(); p != nil {
				m.result = DashResult{Action: ActEditor, Path: p.Path, Project: p.Name}
				return m, tea.Quit
			}
		case "L":
			if p := m.selected(); p != nil {
				m.result = DashResult{Action: ActLazygit, Path: p.Path, Project: p.Name}
				return m, tea.Quit
			}
		case "p":
			if p := m.selected(); p != nil {
				m.result = DashResult{Action: ActPredeliver, Path: p.Path, Project: p.Name}
				return m, tea.Quit
			}
		case "r":
			if !m.fetching {
				m.fetching = true
				m.flash = sMuted.Render("fetching remotes…")
				return m, fetchCmd(m.root)
			}
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

func (m *dashModel) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.pickerItems()
	k := msg.String()
	switch k {
	case "esc", "q":
		m.picking = pickNone
		return m, nil
	case "j", "down":
		if m.pickCursor < len(items)-1 {
			m.pickCursor++
		}
	case "k", "up":
		if m.pickCursor > 0 {
			m.pickCursor--
		}
	case "enter":
		if m.pickCursor >= 0 && m.pickCursor < len(items) {
			openFile(items[m.pickCursor])
			m.flash = sGreen.Render("✓ ") + sMuted.Render("opened ") + sText.Render(filepath.Base(items[m.pickCursor]))
		}
		m.picking = pickNone
	case "a":
		if m.picking == pickSubject {
			for _, p := range items {
				openFile(p)
			}
			m.flash = sGreen.Render("✓ ") + sMuted.Render(fmt.Sprintf("opened %d subjects", len(items)))
			m.picking = pickNone
		}
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			idx := int(k[0] - '1')
			if idx < len(items) {
				openFile(items[idx])
				m.flash = sGreen.Render("✓ ") + sMuted.Render("opened ") + sText.Render(filepath.Base(items[idx]))
				m.picking = pickNone
			}
		}
	}
	return m, nil
}

func (m *dashModel) pickerItems() []string {
	switch m.picking {
	case pickSubject:
		p := m.selected()
		if p == nil {
			return nil
		}
		out := make([]string, len(p.Subjects))
		for i, s := range p.Subjects {
			out[i] = s.Path
		}
		return out
	case pickDocs:
		return m.docs
	}
	return nil
}

func openFile(path string) {
	_ = exec.Command("xdg-open", path).Start()
}

func (m *dashModel) openSubject() {
	p := m.selected()
	if p == nil || len(p.Subjects) == 0 {
		m.flash = sYellow.Render("no subjects filed") + sFaint.Render(" · press ") + sKey.Render("s") + sFaint.Render(" to file from ~/Downloads")
		return
	}
	if len(p.Subjects) == 1 {
		openFile(p.Subjects[0].Path)
		m.flash = sGreen.Render("✓ ") + sMuted.Render("opened ") + sText.Render(p.Subjects[0].Name)
		return
	}
	m.picking = pickSubject
	m.pickCursor = 0
}

func (m *dashModel) openBootstrapSubject() {
	p := m.selected()
	if p == nil {
		return
	}
	s := p.BootstrapSubject()
	if s == nil {
		m.flash = sYellow.Render("no bootstrap PDF filed") + sFaint.Render(" · press ") + sKey.Render("s") + sFaint.Render(" to file one")
		return
	}
	openFile(s.Path)
	m.flash = sGreen.Render("✓ ") + sMuted.Render("opened ") + sText.Render(s.Name)
}

func (m *dashModel) openDocs() {
	if len(m.docs) == 0 {
		m.flash = sYellow.Render("no files in ~/Epitech/docs")
		return
	}
	if len(m.docs) == 1 {
		openFile(m.docs[0])
		m.flash = sGreen.Render("✓ ") + sMuted.Render("opened ") + sText.Render(filepath.Base(m.docs[0]))
		return
	}
	m.picking = pickDocs
	m.pickCursor = 0
}

func (m *dashModel) openWeb() {
	p := m.selected()
	if p == nil || p.Git.Remote == "" {
		m.flash = sYellow.Render("no remote configured")
		return
	}
	url := "https://github.com/" + workspace.RemoteName(p.Git.Remote)
	openFile(url)
	m.flash = sGreen.Render("✓ ") + sMuted.Render("opened GitHub")
}

// ---------------------------------------------------------------- view

// statusDot keeps clean repos visually quiet so only projects needing
// attention pop out in color.
func statusDot(g workspace.Git) string {
	switch {
	case !g.IsRepo:
		return sFaint.Render("○")
	case g.Ahead > 0:
		return sAccent.Render("↑")
	case g.Behind > 0:
		return sCyan.Render("↓")
	case g.Dirty > 0 || g.Untracked > 0:
		return sYellow.Render("●")
	}
	return sFaint.Render("·")
}

func projectBadge(p *workspace.Project) string {
	g := p.Git
	switch {
	case p.InBootstrapPhase():
		return Tag("bootstrap", cAccent)
	case !g.IsRepo:
		return Pill("local", cMuted)
	case g.Ahead > 0:
		return Pill(fmt.Sprintf("↑%d unpushed", g.Ahead), cAccent)
	case g.Behind > 0:
		return Pill(fmt.Sprintf("↓%d behind", g.Behind), cCyan)
	case g.Dirty > 0 || g.Untracked > 0:
		return Pill("dirty", cYellow)
	default:
		return Pill("clean", cGreen)
	}
}

func (m *dashModel) View() string {
	if m.w == 0 {
		return ""
	}
	if m.picking == pickHelp {
		return m.viewHelpModal()
	}
	if m.picking == pickDocs {
		return m.viewDocsModal()
	}

	// Top bar
	total, dirty, ahead, behind := 0, 0, 0, 0
	for _, mod := range m.mods {
		for _, p := range mod.Projects {
			total++
			if !p.Git.IsRepo {
				continue
			}
			if p.Git.Ahead > 0 {
				ahead++
			}
			if p.Git.Behind > 0 {
				behind++
			}
			if p.Git.Dirty > 0 || p.Git.Untracked > 0 {
				dirty++
			}
		}
	}
	var statParts []string
	statParts = append(statParts, sMuted.Render(fmt.Sprintf("%d projects", total)))
	if dirty > 0 {
		statParts = append(statParts, sYellow.Render(fmt.Sprintf("● %d dirty", dirty)))
	}
	if ahead > 0 {
		statParts = append(statParts, sAccent.Render(fmt.Sprintf("↑ %d unpushed", ahead)))
	}
	if behind > 0 {
		statParts = append(statParts, sCyan.Render(fmt.Sprintf("↓ %d behind", behind)))
	}
	if dirty == 0 && ahead == 0 && behind == 0 {
		statParts = append(statParts, sGreen.Render("✓ all clean"))
	}
	stats := strings.Join(statParts, sFaint.Render(" · "))

	home, _ := os.UserHomeDir()
	where := sFaint.Render(strings.Replace(m.root, home, "~", 1))
	viewSwitch := sAccent.Bold(true).Render("modules") + sFaint.Render(" │ ") + sFaint.Render("recent")
	if m.recent {
		viewSwitch = sFaint.Render("modules") + sFaint.Render(" │ ") + sAccent.Bold(true).Render("recent")
	}
	left := " " + sBrand.Render("epi") + " " + where + "   " + viewSwitch
	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(stats)-1, 1)
	header := left + strings.Repeat(" ", gap) + stats + "\n" + Rule(m.w)

	// Body (header = 2 lines, footer = 2 lines)
	bodyH := max(m.h-4, 6)
	listW := min(max(m.w*36/100, 32), 44)
	detailW := max(m.w-listW-4, 30)

	list := m.viewList(listW, bodyH)
	sep := m.viewScrollbar(bodyH)
	detail := m.viewDetail(detailW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, " ", sep, "  ", detail)

	// Footer
	footer := Rule(m.w) + "\n" + m.viewFooter()

	return header + "\n" + body + "\n" + footer
}

func (m *dashModel) viewScrollbar(h int) string {
	if m.totalLines <= h || h <= 2 {
		return sFaint.Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
	}
	thumbSize := max(h*h/m.totalLines, 1)
	maxOff := max(m.totalLines-h, 1)
	thumbStart := m.offset * (h - thumbSize) / maxOff
	var b strings.Builder
	for y := 0; y < h; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		if y >= thumbStart && y < thumbStart+thumbSize {
			b.WriteString(sMuted.Render("┃"))
		} else {
			b.WriteString(sFaint.Render("│"))
		}
	}
	return b.String()
}

func (m *dashModel) viewFooter() string {
	if m.picking == pickSubject {
		return " " + joinKeys([][2]string{{"↑↓ / 1-9", "choose subject"}, {"enter", "open"}, {"a", "open all"}, {"esc", "back"}})
	}
	if m.editing {
		matches := 0
		for _, r := range m.rows {
			if r.project != nil {
				matches++
			}
		}
		right := sFaint.Render(fmt.Sprintf("%d match(es)  ", matches)) + KeyHint("enter", "apply") + "  " + KeyHint("esc", "clear")
		left := " " + m.filter.View()
		gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
		return left + strings.Repeat(" ", gap) + right
	}
	if m.flash != "" {
		right := KeyHint("?", "help")
		left := " " + m.flash
		gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
		return left + strings.Repeat(" ", gap) + right
	}
	if m.filter.Value() != "" {
		left := " " + sAccent.Render("filter: ") + sText.Render(m.filter.Value())
		right := joinKeys([][2]string{{"/", "edit"}, {"esc", "clear"}, {"?", "help"}})
		gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
		return left + strings.Repeat(" ", gap) + right
	}

	leftKeys := [][2]string{
		{"enter", "cd"},
		{"o", "subjects"},
		{"b", "bootstrap"},
		{"/", "filter"},
		{"t", "recent"},
		{"n", "new"},
		{"d", "doctor"},
	}
	right := KeyHint("?", "all keys") + "   " + KeyHint("q", "quit")
	maxLeft := m.w - lipgloss.Width(right) - 3
	out := ""
	for _, k := range leftKeys {
		piece := KeyHint(k[0], k[1])
		cand := piece
		if out != "" {
			cand = out + "   " + piece
		}
		if lipgloss.Width(cand) > maxLeft {
			break
		}
		out = cand
	}
	left := " " + out
	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	return left + strings.Repeat(" ", gap) + right
}

func joinKeys(keys [][2]string) string {
	var kb []string
	for _, k := range keys {
		kb = append(kb, KeyHint(k[0], k[1]))
	}
	return strings.Join(kb, "   ")
}

func (m *dashModel) viewList(w, h int) string {
	if len(m.rows) == 0 {
		m.totalLines = 0
		return lipgloss.NewStyle().Width(w).Height(h).Render("\n  " + sMuted.Render("no projects match"))
	}
	var lines []string
	cursorLine := 0
	for i, r := range m.rows {
		if r.project == nil {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			if r.module == nil {
				lines = append(lines, "  "+sMuted.Bold(true).Render("RECENT ACTIVITY"))
				continue
			}
			title := strings.ToUpper(r.module.Code())
			label := ""
			if r.module.Label() != r.module.Dir {
				label = " " + sMuted.Render(r.module.Label())
			}
			tek := ""
			if m.multiTek {
				tek = " " + sFaint.Render(r.module.Tek)
			}
			count := fmt.Sprintf("%d", len(r.module.Projects))
			if r.collapsed {
				left := "▸ " + title + label + tek
				if i == m.cursor {
					cursorLine = len(lines)
					bar := sAccent.Render("▌")
					head := bar + " " + sAccent.Bold(true).Render("▸ "+title) + label + tek
					pad := strings.Repeat(" ", max(w-lipgloss.Width(head)-len(count)-1, 1))
					lines = append(lines, head+pad+sMuted.Render(count))
				} else {
					head := "  " + sMuted.Render("▸ "+title) + label + tek
					pad := strings.Repeat(" ", max(w-lipgloss.Width(head)-len(count)-1, 1))
					lines = append(lines, head+pad+sFaint.Render(count))
				}
				_ = left
			} else {
				head := "  " + sFaint.Render("▾ ") + sBold.Render(title) + label + tek
				pad := strings.Repeat(" ", max(w-lipgloss.Width(head)-len(count)-1, 1))
				lines = append(lines, head+pad+sFaint.Render(count))
			}
			continue
		}
		p := r.project
		ago := Ago(p.LatestActivity())
		right := ago
		if m.recent {
			right = p.Module.Code() + " · " + ago
		}
		bootMark := ""
		bootW := 0
		if p.InBootstrapPhase() {
			bootMark = " " + sAccent.Render("⚙")
			bootW = 2
		}
		name := p.Name
		nameW := w - 7 - len([]rune(right)) - bootW
		if nameW < 6 {
			nameW = 6
			bootMark = ""
		}
		if len([]rune(name)) > nameW {
			name = string([]rune(name)[:nameW-1]) + "…"
		}
		pad := strings.Repeat(" ", max(nameW-len([]rune(name)), 0))
		if i == m.cursor {
			cursorLine = len(lines)
			bar := sAccent.Render("▌")
			lines = append(lines, bar+" "+statusDot(p.Git)+" "+sBold.Foreground(cAccent).Render(name)+bootMark+pad+"  "+sMuted.Render(right))
		} else {
			lines = append(lines, "  "+statusDot(p.Git)+" "+sText.Render(name)+bootMark+pad+"  "+sFaint.Render(right))
		}
	}
	m.totalLines = len(lines)
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
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].collapsed {
		mod := m.rows[m.cursor].module
		var body strings.Builder
		body.WriteString(sMuted.Render(fmt.Sprintf("%d projects inside %s/%s", len(mod.Projects), mod.Tek, mod.Dir)) + "\n\n")
		body.WriteString(KeyHint("space", "expand") + "   " + KeyHint("enter", "expand"))
		return lipgloss.NewStyle().Width(w).MaxHeight(h).Render("\n" + SectionBox(strings.ToUpper(mod.Code())+" · "+mod.Label(), body.String(), w, false))
	}

	p := m.selected()
	if p == nil {
		return ""
	}
	g := p.Git
	home, _ := os.UserHomeDir()
	var sections []string

	// 1. Hero Header
	var hero strings.Builder
	hero.WriteString(sBold.Render(p.Name) + "  " + projectBadge(p) + "\n")
	crumb := p.Module.Tek + " / " + p.Module.Dir
	if r, err := epitech.Parse(g.Remote); err == nil {
		crumb += "  ·  " + r.ModuleID()
	}
	hero.WriteString(sMuted.Render(crumb) + "\n")
	hero.WriteString(sFaint.Render(strings.Replace(p.Path, home, "~", 1)))
	sections = append(sections, hero.String())

	// 2. Git Card
	var gitB strings.Builder
	field := func(b *strings.Builder, k, v string) {
		b.WriteString(sFaint.Render(fmt.Sprintf("%-8s", k)) + v + "\n")
	}
	if !g.IsRepo {
		field(&gitB, "repo", sFaint.Render("not a git repository"))
	} else {
		var st []string
		if g.Clean() && g.Behind == 0 {
			st = append(st, sGreen.Render("clean"))
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
			st = append(st, sCyan.Render(fmt.Sprintf("↓%d behind", g.Behind)))
		}
		branch := g.Branch
		if branch == "" {
			branch = "main"
		}
		field(&gitB, "branch", sText.Render(branch)+sFaint.Render("  ·  ")+strings.Join(st, sFaint.Render(" · ")))
		if !g.LastCommit.IsZero() {
			meta := Ago(g.LastCommit) + " ago"
			if g.LastAuthor != "" {
				meta += " · " + g.LastAuthor
			}
			msg := g.LastMsg
			maxMsg := max(w-15-len([]rune(meta)), 16)
			if len([]rune(msg)) > maxMsg {
				msg = string([]rune(msg)[:maxMsg-1]) + "…"
			}
			field(&gitB, "commit", sText.Render(msg)+"  "+sFaint.Render(meta))
		} else {
			field(&gitB, "commit", sFaint.Render("no commits yet"))
		}
		if g.Remote != "" {
			field(&gitB, "remote", sMuted.Render(truncate(workspace.RemoteName(g.Remote), max(w-16, 20))))
		}
	}
	sections = append(sections, SectionBox("Git", gitB.String(), w, false))

	// 3. Bootstrap Card (only when bootstrap dir or bootstrap PDF exists)
	if p.Bootstrap.HasDir || p.BootstrapSubject() != nil {
		var bootB strings.Builder
		if p.Bootstrap.HasDir {
			var bs []string
			bs = append(bs, sText.Render("bootstrap/"))
			if p.Bootstrap.Tracked && p.HasMain {
				bs = append(bs, sYellow.Render("tracked in delivery repo"))
			} else if p.Bootstrap.Tracked {
				bs = append(bs, sMuted.Render("tracked"))
			} else {
				bs = append(bs, sGreen.Render("untracked"))
			}
			if len(p.Bootstrap.Binaries) > 0 {
				bs = append(bs, sYellow.Render(fmt.Sprintf("%d binary", len(p.Bootstrap.Binaries))))
			}
			bootB.WriteString(strings.Join(bs, sFaint.Render(" · ")) + "\n")
			hints := []string{KeyHint("b", "cd bootstrap/")}
			if p.BootstrapSubject() != nil {
				hints = append(hints, KeyHint("B", "open PDF"))
			}
			bootB.WriteString(strings.Join(hints, sFaint.Render("   ")))
		} else {
			bootB.WriteString(sMuted.Render("bootstrap PDF filed, no bootstrap/ folder yet") + "\n")
			bootB.WriteString(KeyHint("b", "init bootstrap/") + "   " + KeyHint("B", "open PDF"))
		}
		sections = append(sections, SectionBox("Bootstrap", bootB.String(), w, false))
	}

	// 4. Subjects Card (accent-focused when choosing a subject!)
	var subjB strings.Builder
	subjTitle := "Subjects"
	if m.picking == pickSubject {
		subjTitle = "Subjects — pick one to open"
	} else if len(p.Subjects) > 1 {
		subjTitle = fmt.Sprintf("Subjects (%d)", len(p.Subjects))
	}
	if len(p.Subjects) == 0 {
		subjB.WriteString(sFaint.Render("None filed yet") + "  " + KeyHint("s", "file from ~/Downloads"))
	} else {
		for i, s := range p.Subjects {
			badge := kindBadge(s.Kind)
			name := truncate(s.Name, max(w-24, 16))
			if m.picking == pickSubject {
				num := sFaint.Render(fmt.Sprintf("%d ", i+1))
				if i == m.pickCursor {
					subjB.WriteString(sAccent.Render("› ") + num + badge + " " + sBold.Foreground(cAccent).Render(name) + "\n")
				} else {
					subjB.WriteString("  " + num + badge + " " + sText.Render(name) + "\n")
				}
			} else {
				subjB.WriteString(badge + " " + sText.Render(name) + "\n")
			}
		}
		if p.MainSubject() == nil && p.BootstrapSubject() != nil && m.picking != pickSubject {
			subjB.WriteString(sFaint.Render("Main subject not filed yet · ") + KeyHint("s", "file"))
		}
	}
	sections = append(sections, SectionBox(subjTitle, subjB.String(), w, m.picking == pickSubject))

	// 5. Contextual quick actions strip (compact, adapts to selected project)
	var actions []string
	actions = append(actions, KeyHint("enter", "cd"))
	if len(p.Subjects) > 1 {
		actions = append(actions, KeyHint("o", "pick subject"))
	} else if len(p.Subjects) == 1 {
		actions = append(actions, KeyHint("o", "open subject"))
	} else {
		actions = append(actions, KeyHint("s", "file subject"))
	}
	if p.Bootstrap.HasDir {
		actions = append(actions, KeyHint("b", "bootstrap/"))
	}
	actions = append(actions, KeyHint("e", "edit"))
	if g.IsRepo {
		actions = append(actions, KeyHint("p", "predeliver"))
		actions = append(actions, KeyHint("L", "lazygit"))
		if g.Remote != "" {
			actions = append(actions, KeyHint("w", "github"))
		}
	}
	actionLine := ""
	for _, a := range actions {
		cand := a
		if actionLine != "" {
			cand = actionLine + sFaint.Render("  ·  ") + a
		}
		if lipgloss.Width(cand) > w-2 {
			break
		}
		actionLine = cand
	}
	sections = append(sections, " "+actionLine)

	content := "\n" + strings.Join(sections, "\n")
	return lipgloss.NewStyle().Width(w).MaxHeight(h).Render(content)
}

func (m *dashModel) viewDocsModal() string {
	var b strings.Builder
	b.WriteString(sFaint.Render("~/Epitech/docs") + "\n\n")
	for i, doc := range m.docs {
		num := sFaint.Render(fmt.Sprintf("%d ", i+1))
		if i == m.pickCursor {
			b.WriteString(sAccent.Render("› ") + num + sBold.Foreground(cAccent).Render(filepath.Base(doc)) + "\n")
		} else {
			b.WriteString("  " + num + sText.Render(filepath.Base(doc)) + "\n")
		}
	}
	b.WriteString("\n" + joinKeys([][2]string{{"↑↓/1-9", "pick"}, {"enter", "open"}, {"esc", "close"}}))
	box := SectionBox("Epitech Reference Docs", b.String(), min(m.w-6, 56), true)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func (m *dashModel) viewHelpModal() string {
	group := func(title string, pairs [][2]string) string {
		var b strings.Builder
		b.WriteString(sBold.Render(title) + "\n")
		for _, p := range pairs {
			b.WriteString("  " + sKey.Render(fmt.Sprintf("%-9s", p[0])) + sMuted.Render(p[1]) + "\n")
		}
		return b.String()
	}

	left := group("Navigate", [][2]string{
		{"↑/↓ j/k", "move selection"},
		{"g / G", "top / bottom"},
		{"enter", "cd into project"},
		{"b", "bootstrap/ (cd or init)"},
		{"space", "collapse / expand module"},
		{"t", "modules / recent view"},
		{"/", "filter projects"},
	}) + "\n" + group("Subjects & Docs", [][2]string{
		{"o", "open or pick subject"},
		{"B", "open bootstrap PDF"},
		{"s", "file subjects"},
		{"D", "open ~/Epitech/docs"},
	})

	right := group("Project Tools", [][2]string{
		{"e", "open in $EDITOR"},
		{"L", "open lazygit"},
		{"p", "run epitech-predeliver"},
		{"w", "open repo on GitHub"},
	}) + "\n" + group("Workspace", [][2]string{
		{"n", "clone new project"},
		{"d", "run epi doctor"},
		{"r", "fetch all git remotes"},
		{"?", "toggle this help"},
		{"q", "quit"},
	})

	colW := 36
	cols := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(colW).Render(left),
		"   ",
		lipgloss.NewStyle().Width(colW).Render(right),
	)
	body := cols + "\n" + sFaint.Render("press esc or ? to close")
	box := SectionBox("epi — Keyboard Shortcuts", body, min(m.w-4, colW*2+9), true)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func kindBadge(kind string) string {
	var label string
	switch kind {
	case "":
		label = "MAIN"
	case "bootstrap":
		label = "BOOT"
	case "examples":
		label = "EXAMPLE"
	default:
		label = strings.ToUpper(kind)
		if len(label) > 7 {
			label = label[:7]
		}
	}
	text := fmt.Sprintf("%-7s", label)
	switch kind {
	case "":
		return sGreen.Bold(true).Render(text)
	case "bootstrap":
		return sAccent.Bold(true).Render(text)
	case "kickoff", "roadmap":
		return sYellow.Bold(true).Render(text)
	default:
		return sMuted.Bold(true).Render(text)
	}
}
