// epi keeps an Epitech folder organized: browse projects, manage bootstraps
// and multiple subjects, add new projects from GitHub, and check the whole
// tree for problems.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cempack/epi/internal/ui"
	"github.com/cempack/epi/internal/workspace"
)

// version is set at release time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// The shell wrapper: plain `epi` and `epi cd <query>` cd into the chosen dir.
const shellInit = `epi() {
  if [ $# -eq 0 ] || [ "$1" = "cd" ]; then
    local dir
    dir="$(command epi "$@")" && [ -n "$dir" ] && cd "$dir"
  else
    command epi "$@"
  fi
}`

const bashCompletion = `_epi_complete() {
  local cur prev cmds
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  cmds="new doctor subject bootstrap open docs search sync allowed cd shell root completion version help"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "$cmds" -- "$cur") )
    return
  fi
  case "${COMP_WORDS[1]}" in
    cd|open|subject|bootstrap|allowed)
      if [ "$COMP_CWORD" -eq 2 ]; then
        local projs
        projs="$(command epi __complete projects 2>/dev/null)"
        COMPREPLY=( $(compgen -W "$projs" -- "$cur") )
      elif [ "${COMP_WORDS[1]}" = "open" ] && [ "$COMP_CWORD" -eq 3 ]; then
        COMPREPLY=( $(compgen -W "main bootstrap kickoff roadmap annex bonus slides examples all" -- "$cur") )
      fi
      ;;
    completion)
      COMPREPLY=( $(compgen -W "bash zsh" -- "$cur") )
      ;;
  esac
}
complete -F _epi_complete epi`

const zshCompletion = `#compdef epi
_epi() {
  local -a cmds
  cmds=(
    'new:Clone a project from GitHub and file its subjects'
    'doctor:Check naming, placement, bootstrap hygiene, unpushed work, stray PDFs'
    'subject:File subjects for a project from ~/Downloads or paths'
    'bootstrap:Initialize or tidy bootstrap/ for a project'
    'open:Open a project subject PDF'
    'docs:Open a reference doc from ~/Epitech/docs'
    'search:Full-text search across all filed subjects and docs'
    'sync:Fetch all repositories concurrently'
    'allowed:Extract authorized functions from a project subject PDF'
    'cd:Fuzzy-jump into a project or its bootstrap/ folder'
    'shell:Print the shell wrapper function'
    'root:Print the Epitech folder path'
    'completion:Print shell completion script (bash|zsh)'
    'version:Print epi version'
  )
  if (( CURRENT == 2 )); then
    _describe 'command' cmds
    return
  fi
  case "$words[2]" in
    cd|open|subject|bootstrap|allowed)
      if (( CURRENT == 3 )); then
        local -a projs
        projs=(${(f)"$(command epi __complete projects 2>/dev/null)"})
        _describe 'project' projs
      elif [[ "$words[2]" == "open" && CURRENT == 4 ]]; then
        local -a kinds
        kinds=(main bootstrap kickoff roadmap annex bonus slides examples all)
        _describe 'kind' kinds
      fi
      ;;
    completion)
      _values 'shell' bash zsh
      ;;
  esac
}
compdef _epi epi`

func main() {
	// All UI goes to stderr so stdout can carry a path for `cd "$(epi)"`.
	// Colors are detected on stderr too, so piping stdout keeps them.
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stderr))

	root := workspace.Root()
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}

	var err error
	switch cmd {
	case "":
		err = dashboard(root)
	case "new", "add", "clone":
		err = ui.RunNew(root, argAt(args, 1))
	case "doctor", "check":
		err = ui.RunDoctor(root)
	case "subject", "subjects", "file":
		var files []string
		if len(args) > 2 {
			files = args[2:]
		}
		err = ui.RunSubject(root, argAt(args, 1), files)
	case "bootstrap", "boot":
		err = ui.RunBootstrap(root, argAt(args, 1))
	case "open", "pdf":
		err = ui.RunOpen(root, argAt(args, 1), argAt(args, 2))
	case "docs", "doc":
		err = ui.RunDocs(root, argAt(args, 1))
	case "search", "grep":
		err = ui.RunSearch(root, strings.Join(args[1:], " "))
	case "sync", "fetch":
		err = ui.RunSync(root)
	case "allowed", "funcs":
		proj, save := "", false
		for _, a := range args[1:] {
			if a == "--save" || a == "-s" {
				save = true
			} else if proj == "" {
				proj = a
			}
		}
		err = ui.RunAllowed(root, proj, save)
	case "cd":
		var dir string
		dir, err = ui.ResolveCd(root, argAt(args, 1))
		if err == nil && dir != "" {
			fmt.Println(dir)
		}
	case "completion":
		switch argAt(args, 1) {
		case "zsh":
			fmt.Println(zshCompletion)
		default:
			fmt.Println(bashCompletion)
		}
	case "__complete":
		complete(root, argAt(args, 1))
	case "shell":
		fmt.Println(shellInit)
	case "root":
		fmt.Println(root)
	case "version", "--version", "-v":
		fmt.Println("epi " + version)
	case "help", "--help", "-h":
		help()
	default:
		fmt.Fprintln(os.Stderr, "epi: unknown command "+cmd+"\n")
		help()
		os.Exit(2)
	}
	if err != nil {
		if err.Error() != "cancelled" {
			fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("  ✗ "+err.Error()))
		}
		os.Exit(1)
	}
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func complete(root, what string) {
	if what != "projects" {
		return
	}
	mods, err := workspace.Scan(root)
	if err != nil {
		return
	}
	for _, m := range mods {
		for _, p := range m.Projects {
			fmt.Println(p.Name)
		}
	}
}

func dashboard(root string) error {
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("no Epitech folder at %s (set EPI_ROOT)", root)
	}
	for {
		res, err := ui.RunDashboard(root)
		if err != nil {
			return err
		}
		switch res.Action {
		case ui.ActCd:
			fmt.Println(res.Path)
			return nil
		case ui.ActNew:
			if err := ui.RunNew(root, ""); err != nil && err.Error() != "cancelled" {
				return err
			}
			waitKey()
		case ui.ActDoctor:
			_ = ui.RunDoctor(root) // report is already printed
			waitKey()
		case ui.ActSubject:
			if err := ui.RunSubject(root, res.Project, nil); err != nil && err.Error() != "cancelled" {
				fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("  ✗ "+err.Error()))
			}
			waitKey()
		case ui.ActBootstrap:
			if err := ui.RunBootstrap(root, res.Project); err != nil && err.Error() != "cancelled" {
				fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("  ✗ "+err.Error()))
			}
			waitKey()
		case ui.ActPredeliver:
			runInteractive(res.Path, "epitech-predeliver")
			waitKey()
		case ui.ActEditor:
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "nvim"
			}
			runInteractive(res.Path, editor, ".")
		case ui.ActLazygit:
			runInteractive(res.Path, "lazygit")
		default:
			return nil
		}
	}
}

func runInteractive(dir, bin string, args ...string) {
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("  ✗ "+bin+" not found on PATH"))
		return
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr // keep stdout reserved for `cd "$(epi)"`
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func waitKey() {
	fmt.Fprint(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  press enter to go back "))
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
}

func help() {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	bold := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	group := func(b *strings.Builder, title string, cmds [][2]string) {
		b.WriteString("  " + bold.Render(title) + "\n")
		for _, c := range cmds {
			b.WriteString("    " + accent.Render(fmt.Sprintf("%-22s", c[0])) + muted.Render(c[1]) + "\n")
		}
		b.WriteString("\n")
	}

	var b strings.Builder
	b.WriteString("\n  " + ui.Header("keeps your Epitech folder tidy") + "\n\n")
	group(&b, "Browse & Jump", [][2]string{
		{"epi", "interactive dashboard (press ? inside for all shortcuts)"},
		{"epi cd [query]", "fuzzy-jump into a project or <project>/b for bootstrap/"},
		{"epi new [repo]", "clone from GitHub, file subjects, optional bootstrap/"},
	})
	group(&b, "Subjects & Bootstrap", [][2]string{
		{"epi open [proj] [k]", "open a subject (main, bootstrap, kickoff, roadmap, all)"},
		{"epi subject [proj]", "file subject PDFs/docs from ~/Downloads or paths"},
		{"epi bootstrap [proj]", "create/tidy bootstrap/ + .gitignore and open PDF"},
		{"epi docs [query]", "open a reference PDF from ~/Epitech/docs"},
		{"epi search <query>", "full-text search across all filed subjects and docs"},
	})
	group(&b, "Check & Sync", [][2]string{
		{"epi doctor", "audit layout, bootstrap hygiene, git state, stray/dup PDFs"},
		{"epi allowed [proj]", "parse authorized functions from subject (--save for predeliver)"},
		{"epi sync", "fetch all git repositories concurrently"},
	})
	group(&b, "Shell", [][2]string{
		{"epi shell", "shell wrapper so `epi` and `epi cd` change directories"},
		{"epi completion", "bash or zsh tab-completion script"},
		{"epi root", "print the Epitech folder path"},
	})
	b.WriteString("  " + muted.Render("setup: ") + `eval "$(command epi shell)"` + "\n")
	b.WriteString("  " + muted.Render("env:   EPI_ROOT (~/Epitech) · EPI_DOWNLOADS (~/Downloads)") + "\n\n")
	fmt.Fprint(os.Stderr, b.String())
}
