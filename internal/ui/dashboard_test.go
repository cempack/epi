package ui

import (
	"strings"
	"testing"

	"github.com/cempack/epi/internal/workspace"
)

func TestDashboardViewRendersCleanly(t *testing.T) {
	mod := &workspace.Module{Tek: "tek1", Dir: "psu100_unix", Path: "/tmp/tek1/psu100_unix"}
	p := &workspace.Project{
		Name:   "my_sudo",
		Path:   "/tmp/tek1/psu100_unix/my_sudo",
		Module: mod,
		Git:    workspace.Git{IsRepo: true, Branch: "main", LastAuthor: "Elliot", LastMsg: "feat: init"},
		Bootstrap: workspace.Bootstrap{
			HasDir: true,
			Dir:    "/tmp/tek1/psu100_unix/my_sudo/bootstrap",
		},
		Subjects: []workspace.Subject{
			{Name: "my_sudo.pdf", Kind: "", Label: "main"},
			{Name: "my_sudo-bootstrap.pdf", Kind: "bootstrap", Label: "bootstrap"},
			{Name: "my_sudo-kickoff.pdf", Kind: "kickoff", Label: "kickoff"},
		},
	}
	mod.Projects = []*workspace.Project{p}

	m := newDashModel("/tmp", []*workspace.Module{mod})
	m.w, m.h = 110, 32

	out := m.View()
	for _, want := range []string{"my_sudo", "Git", "Bootstrap", "Subjects", "MAIN", "BOOT", "KICKOFF", "all keys"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard view missing %q", want)
		}
	}

	m.picking = pickHelp
	if !strings.Contains(m.View(), "Keyboard Shortcuts") {
		t.Errorf("help overlay missing title")
	}
}
