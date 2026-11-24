package ui

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/cempack/epi/internal/epitech"
	"github.com/cempack/epi/internal/remote"
	"github.com/cempack/epi/internal/subjects"
	"github.com/cempack/epi/internal/workspace"
)

type finding struct{ icon, text string }

type section struct {
	title string
	items []finding
}

var moduleDirRe = regexp.MustCompile(`^(pool|workshops|[a-z]{3}\d{3}(_[a-z0-9]+)*)$`)

const bigFile = 20 << 20 // 20 MB

type stray struct {
	src, dst string
	project  string
}

// RunDoctor checks the tree and offers to file stray subject PDFs.
func RunDoctor(root string) error {
	var (
		mods   []*workspace.Module
		strays []stray
		uncl   []remote.Repo
		junk   = map[*workspace.Project][]string{}
		netErr error
	)
	spin("Examining "+tilde(root), func() {
		mods, _ = workspace.Scan(root)
		for _, m := range mods {
			for _, p := range m.Projects {
				junk[p] = scanJunk(p.Path)
			}
		}
		strays = findStrays(mods)
	})
	idx := indexLocal(root)
	spin("Checking GitHub for repos you haven't cloned", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		all, err := remote.List(ctx, remote.Orgs(ctx, idx.owners))
		netErr = err
		for _, r := range all {
			if _, ok := idx.byRemote[strings.ToLower(r.Owner+"/"+r.Name)]; !ok {
				uncl = append(uncl, r)
			}
		}
	})

	naming := section{title: "Naming"}
	placement := section{title: "Placement"}
	work := section{title: "Unsaved work"}
	clutter := section{title: "Clutter"}
	missing := section{title: "Missing subjects"}
	github := section{title: "On GitHub, not cloned"}

	total, healthy := 0, 0
	for _, m := range mods {
		if !moduleDirRe.MatchString(m.Dir) {
			naming.items = append(naming.items, finding{"✎", rel(root, m.Path) + sFaint.Render("  module folders are <code>_<topic>, e.g. psu100_unix")})
		}
		for _, p := range m.Projects {
			total++
			ok := true
			if !epitech.IsSnake(p.Name) {
				naming.items = append(naming.items, finding{"✎", rel(root, p.Path) + sMuted.Render("  → "+epitech.Snake(p.Name))})
				ok = false
			}
			if r, err := epitech.Parse(p.Git.Remote); err == nil {
				wantTek := r.Tek()
				wrongMod := (r.IsPool() && m.Dir != "pool") || (!r.IsPool() && m.Code() != r.Code())
				if wrongMod || m.Tek != wantTek {
					placement.items = append(placement.items, finding{"⇄", rel(root, p.Path) + sFaint.Render("  remote is ") + sText.Render(r.ModuleID()) + sMuted.Render(", expected "+wantTek+"/"+r.Code()+"_…")})
					ok = false
				}
			}
			g := p.Git
			if g.Ahead > 0 {
				work.items = append(work.items, finding{sAccent.Render("↑"), rel(root, p.Path) + sMuted.Render(fmt.Sprintf("  %d commit(s) not pushed", g.Ahead))})
				ok = false
			}
			if g.Dirty > 0 || g.Untracked > 0 {
				var parts []string
				if g.Dirty > 0 {
					parts = append(parts, fmt.Sprintf("%d modified", g.Dirty))
				}
				if g.Untracked > 0 {
					parts = append(parts, fmt.Sprintf("%d untracked", g.Untracked))
				}
				work.items = append(work.items, finding{sYellow.Render("●"), rel(root, p.Path) + sMuted.Render("  "+strings.Join(parts, ", "))})
				ok = false
			}
			for _, j := range junk[p] {
				clutter.items = append(clutter.items, finding{"⌫", rel(root, j)})
			}
			if len(p.Subjects) == 0 && m.Dir != "workshops" {
				missing.items = append(missing.items, finding{"▤", rel(root, p.Path)})
			}
			if ok {
				healthy++
			}
		}
	}
	hints := subjectHints(mods)
	for _, r := range uncl {
		github.items = append(github.items, finding{"⎇", sText.Render(fmt.Sprintf("%-16s", epitech.SuggestName(r.Slug, hints))) + sMuted.Render(r.ModuleID()) + sFaint.Render("  "+r.Name)})
	}
	if netErr != nil && len(uncl) == 0 {
		github.items = append(github.items, finding{"?", sFaint.Render("couldn't reach GitHub: " + netErr.Error())})
	}

	// Report.
	say("")
	say(" " + Header("doctor"))
	say(" " + Rule(56))
	say(" " + healthBar(healthy, total, 40) + "  " + sBold.Render(fmt.Sprintf("%d/%d", healthy, total)) + sMuted.Render(" projects spotless"))
	say("")
	for _, s := range []section{naming, placement, work, clutter, github, missing} {
		if len(s.items) == 0 {
			continue
		}
		say(" " + sBold.Render(s.title) + sFaint.Render(fmt.Sprintf(" %d", len(s.items))))
		for i, f := range s.items {
			if i == 12 {
				say(sFaint.Render(fmt.Sprintf("   … and %d more", len(s.items)-12)))
				break
			}
			say("   " + sMuted.Render(f.icon) + " " + f.text)
		}
		say("")
	}
	if len(uncl) > 0 {
		say(sMuted.Render(" clone one with ") + sText.Render("epi new") + "\n")
	}

	// The one fixer: file stray subject PDFs.
	if len(strays) == 0 {
		return nil
	}
	say(" " + sBold.Render("Stray subjects in ~/Downloads") + sFaint.Render(fmt.Sprintf(" %d", len(strays))))
	var opts []huh.Option[int]
	var picked []int
	for i, s := range strays {
		label := sText.Render(filepath.Base(s.src)) + sMuted.Render("  →  "+rel(root, s.dst))
		opts = append(opts, huh.NewOption(label, i).Selected(true))
		picked = append(picked, i)
	}
	if err := form(huh.NewGroup(
		huh.NewMultiSelect[int]().Title("File these?").Description("They'll be moved out of ~/Downloads.").Options(opts...).Value(&picked),
	)).Run(); err != nil {
		return quiet(err)
	}
	for _, i := range picked {
		s := strays[i]
		if err := subjects.Move(s.src, s.dst); err != nil {
			say(sYellow.Render("   ● ") + sMuted.Render(filepath.Base(s.src)+": "+err.Error()))
			continue
		}
		say(sGreen.Render("   ✓ ") + sText.Render(rel(root, s.dst)))
	}
	say("")
	return nil
}

// healthBar is a gradient progress bar.
func healthBar(n, total, width int) string {
	if total == 0 {
		return sFaint.Render(strings.Repeat("━", width))
	}
	filled := n * width / total
	return sAccent.Render(strings.Repeat("━", filled)) + sFaint.Render(strings.Repeat("━", width-filled))
}

// scanJunk finds editor leftovers and huge files, skipping .git.
func scanJunk(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if (strings.HasPrefix(n, "#") && strings.HasSuffix(n, "#")) || strings.HasSuffix(n, "~") || n == "vgcore" || strings.HasPrefix(n, "vgcore.") {
			out = append(out, path)
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > bigFile {
			out = append(out, path+sFaint.Render(fmt.Sprintf("  %d MB", info.Size()>>20)))
		}
		return nil
	})
	return out
}

// findStrays matches recent PDFs in ~/Downloads to projects that don't have
// that subject filed yet. The longest matching project name wins, so
// "my_printf_roadmap.pdf" goes to my_printf, not to a hypothetical "printf".
func findStrays(mods []*workspace.Module) []stray {
	type key struct{ name, slug string }
	projects := map[key]*workspace.Project{}
	for _, m := range mods {
		for _, p := range m.Projects {
			slug := ""
			if r, err := epitech.Parse(p.Git.Remote); err == nil {
				slug = r.Slug
			}
			projects[key{p.Name, slug}] = p
		}
	}
	cands := subjects.Find(subjects.Downloads(), "", 120*24*time.Hour, 60)
	var out []stray
	for _, c := range cands {
		base := filepath.Base(c.Path)
		var best *workspace.Project
		bestLen := 0
		for k, p := range projects {
			for _, s := range []string{k.name, k.slug} {
				if l := len(epitech.Squash(s)); l > bestLen && subjects.Matches(s, base, c.Title) {
					best, bestLen = p, l
				}
			}
		}
		if best == nil {
			continue
		}
		dst := filepath.Join(best.Module.Path, "subjects", epitech.SubjectFile(best.Name, c.Kind, ".pdf"))
		if workspace.Exists(dst) {
			continue // already filed (or a same-kind subject exists)
		}
		out = append(out, stray{src: c.Path, dst: dst, project: best.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dst < out[j].dst })
	return out
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
