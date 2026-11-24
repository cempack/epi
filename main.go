// epi keeps an Epitech folder organized: browse projects, add new ones from
// GitHub with their subjects, and check the whole tree for problems.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cempack/epi/internal/ui"
	"github.com/cempack/epi/internal/workspace"
)

// version is set at release time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// The shell wrapper: plain `epi` cds into the project you pick.
const shellInit = `epi() {
  if [ $# -eq 0 ]; then
    local dir
    dir="$(command epi)" && [ -n "$dir" ] && cd "$dir"
  else
    command epi "$@"
  fi
}`

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
		arg := ""
		if len(args) > 1 {
			arg = args[1]
		}
		err = ui.RunNew(root, arg)
	case "doctor", "check":
		err = ui.RunDoctor(root)
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
		default:
			return nil
		}
	}
}

func waitKey() {
	fmt.Fprint(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  press enter to go back "))
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
}

func help() {
	bold := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	faint := muted
	cmds := [][2]string{
		{"epi", "dashboard: browse projects, enter to cd, o to open the subject"},
		{"epi new [repo]", "clone a project from GitHub and file its subject PDFs"},
		{"epi doctor", "check naming, placement, unpushed work, stray PDFs"},
		{"epi shell", "print the shell function that makes `epi` cd for you"},
		{"epi root", "print the Epitech folder path"},
	}
	var b strings.Builder
	b.WriteString("\n  " + ui.Header("keeps your Epitech folder tidy") + "\n\n")
	for _, c := range cmds {
		b.WriteString("  " + bold.Render(fmt.Sprintf("%-16s", c[0])) + muted.Render(c[1]) + "\n")
	}
	b.WriteString("\n  " + faint.Render("setup: add ") + muted.Render(`eval "$(command epi shell)"`) + faint.Render(" to ~/.bashrc") + "\n")
	b.WriteString("  " + faint.Render("env:   EPI_ROOT (default ~/Epitech) · EPI_DOWNLOADS (default ~/Downloads)") + "\n\n")
	fmt.Fprint(os.Stderr, b.String())
}
