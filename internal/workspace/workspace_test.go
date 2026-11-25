package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSortsSubjectsAndDetectsBootstrap(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "tek1", "psu100_unix")
	proj := filepath.Join(mod, "my_sudo")
	boot := filepath.Join(proj, "bootstrap")
	subj := filepath.Join(mod, "subjects")
	for _, d := range []string{boot, subj} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"my_sudo-kickoff.pdf", "my_sudo-bootstrap.pdf", "my_sudo.pdf"} {
		if err := os.WriteFile(filepath.Join(subj, f), []byte("pdf"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Put a fake ELF binary inside bootstrap/.
	bin := filepath.Join(boot, "bootstrap")
	if err := os.WriteFile(bin, []byte{0x7f, 'E', 'L', 'F', 0, 0}, 0o755); err != nil {
		t.Fatal(err)
	}

	mods, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || len(mods[0].Projects) != 1 {
		t.Fatalf("unexpected mods: %+v", mods)
	}
	p := mods[0].Projects[0]
	if len(p.Subjects) != 3 {
		t.Fatalf("expected 3 subjects, got %d", len(p.Subjects))
	}
	if p.Subjects[0].Kind != "" || p.Subjects[1].Kind != "bootstrap" || p.Subjects[2].Kind != "kickoff" {
		t.Fatalf("bad subject order: %+v", p.Subjects)
	}
	if !p.Bootstrap.HasDir || len(p.Bootstrap.Binaries) != 1 {
		t.Fatalf("expected bootstrap dir with 1 binary, got %+v", p.Bootstrap)
	}
	if !p.InBootstrapPhase() {
		t.Fatalf("expected project without Makefile/src to be in bootstrap phase")
	}
	if hits := FindProjects(mods, "sudo"); len(hits) != 1 || hits[0].Name != "my_sudo" {
		t.Fatalf("FindProjects failed: %+v", hits)
	}
}
