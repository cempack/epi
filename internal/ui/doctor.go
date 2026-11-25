package ui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
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

type strayKind int

const (
	strayNew       strayKind = iota // target doesn't exist yet -> move
	strayDuplicate                  // target exists and SHA256 matches -> delete download copy
	strayUpdated                    // target exists and content differs -> replace or save as -2
)

type stray struct {
	src, dst string
	project  string
	kind     strayKind
}

type cleanableFile struct {
	path   string
	reason string
}

// RunDoctor checks the tree and offers safe interactive fixes (filing stray
// subjects, cleaning duplicate downloads, and removing junk/bootstrap binaries).
func RunDoctor(root string) error {
	var (
		mods      []*workspace.Module
		strays    []stray
		uncl      []remote.Repo
		junk      = map[*workspace.Project][]string{}
		cleanable []cleanableFile
		netErr    error
	)
	spin("Examining "+tilde(root), func() {
		mods, _ = workspace.Scan(root)
		for _, m := range mods {
			for _, p := range m.Projects {
				items, rm := scanJunk(p)
				junk[p] = items
				cleanable = append(cleanable, rm...)
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
	bootSec := section{title: "Bootstrap hygiene"}
	clutter := section{title: "Clutter"}
	bootOnly := section{title: "Bootstrap only (missing main subject)"}
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
			if g.Behind > 0 {
				work.items = append(work.items, finding{sAccent.Render("↓"), rel(root, p.Path) + sMuted.Render(fmt.Sprintf("  %d commit(s) behind origin", g.Behind))})
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
			if p.Bootstrap.HasDir {
				for _, bin := range p.Bootstrap.Binaries {
					bootSec.items = append(bootSec.items, finding{sYellow.Render("⚙"), rel(root, bin) + sFaint.Render("  compiled binary in bootstrap/")})
					ok = false
				}
				if p.Bootstrap.Tracked && p.HasMain {
					bootSec.items = append(bootSec.items, finding{sYellow.Render("⚙"), rel(root, p.Bootstrap.Dir) + sFaint.Render("  tracked in a repo with a Makefile (Banana/C-O1 risk)")})
				}
			}
			for _, j := range junk[p] {
				clutter.items = append(clutter.items, finding{"⌫", rel(root, j)})
			}
			if m.Dir != "workshops" {
				if len(p.Subjects) == 0 {
					missing.items = append(missing.items, finding{"▤", rel(root, p.Path)})
				} else if p.MainSubject() == nil && p.BootstrapSubject() != nil {
					bootOnly.items = append(bootOnly.items, finding{"▤", rel(root, p.Path) + sFaint.Render("  has bootstrap only")})
				}
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
	summaryW := min(max(TermWidth()-4, 48), 72)
	barW := min(max(summaryW-28, 18), 36)
	var sumB strings.Builder
	sumB.WriteString(healthBar(healthy, total, barW) + "  " + sBold.Render(fmt.Sprintf("%d/%d", healthy, total)) + sMuted.Render(" spotless"))
	var pills []string
	if len(work.items) > 0 {
		pills = append(pills, Pill(fmt.Sprintf("%d unsaved", len(work.items)), cYellow))
	}
	if len(bootSec.items) > 0 {
		pills = append(pills, Pill(fmt.Sprintf("%d bootstrap", len(bootSec.items)), cYellow))
	}
	if len(clutter.items) > 0 {
		pills = append(pills, Pill(fmt.Sprintf("%d clutter", len(clutter.items)), cYellow))
	}
	if len(uncl) > 0 {
		pills = append(pills, Pill(fmt.Sprintf("%d uncloned", len(uncl)), cAccent))
	}
	if len(pills) > 0 {
		sumB.WriteString("\n" + strings.Join(pills, " "))
	} else {
		sumB.WriteString("\n" + sGreen.Render("✓ everything looks spotless"))
	}
	say(IndentBox("Workspace Health", sumB.String(), summaryW, false))
	say("")
	for _, s := range []section{naming, placement, work, bootSec, clutter, github, bootOnly, missing} {
		if len(s.items) == 0 {
			continue
		}
		say(SectionHeading(s.title, len(s.items)))
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
		say(" " + sMuted.Render("clone one with ") + sKey.Render("epi new") + "\n")
	}

	// Fixer 1: file stray / updated / duplicate subjects in ~/Downloads.
	var newStrays, dupStrays, updStrays []stray
	for _, s := range strays {
		switch s.kind {
		case strayNew:
			newStrays = append(newStrays, s)
		case strayDuplicate:
			dupStrays = append(dupStrays, s)
		case strayUpdated:
			updStrays = append(updStrays, s)
		}
	}

	if len(newStrays) > 0 {
		say(" " + sBold.Render("Stray subjects in ~/Downloads") + sFaint.Render(fmt.Sprintf(" %d", len(newStrays))))
		var opts []huh.Option[int]
		var picked []int
		for i, s := range newStrays {
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
			s := newStrays[i]
			if err := subjects.Move(s.src, s.dst); err != nil {
				say(sYellow.Render("   ● ") + sMuted.Render(filepath.Base(s.src)+": "+err.Error()))
				continue
			}
			say(sGreen.Render("   ✓ ") + sText.Render(rel(root, s.dst)))
		}
		say("")
	}

	if len(updStrays) > 0 {
		say(" " + sBold.Render("Updated or extra subjects in ~/Downloads") + sFaint.Render(fmt.Sprintf(" %d", len(updStrays))))
		for _, s := range updStrays {
			var action string
			err := form(huh.NewGroup(
				huh.NewSelect[string]().
					Title(filepath.Base(s.src) + " differs from " + filepath.Base(s.dst)).
					Description("Same project and kind, different file contents.").
					Options(
						huh.NewOption("Replace "+rel(root, s.dst), "replace"),
						huh.NewOption("Keep both (save with -2 suffix)", "keep"),
						huh.NewOption("Skip", "skip"),
					).
					Value(&action),
			)).Run()
			if err != nil {
				return quiet(err)
			}
			switch action {
			case "replace":
				if err := subjects.Replace(s.src, s.dst); err != nil {
					say(sYellow.Render("   ● ") + sMuted.Render(err.Error()))
				} else {
					say(sGreen.Render("   ✓ replaced ") + sText.Render(rel(root, s.dst)))
				}
			case "keep":
				alt := nextAvailablePath(s.dst)
				if err := subjects.Move(s.src, alt); err != nil {
					say(sYellow.Render("   ● ") + sMuted.Render(err.Error()))
				} else {
					say(sGreen.Render("   ✓ ") + sText.Render(rel(root, alt)))
				}
			}
		}
		say("")
	}

	if len(dupStrays) > 0 {
		say(" " + sBold.Render("Already-filed duplicates in ~/Downloads") + sFaint.Render(fmt.Sprintf(" %d", len(dupStrays))))
		var opts []huh.Option[int]
		var picked []int
		for i, s := range dupStrays {
			label := sText.Render(filepath.Base(s.src)) + sFaint.Render("  (identical to "+rel(root, s.dst)+")")
			opts = append(opts, huh.NewOption(label, i))
		}
		if err := form(huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Delete duplicate downloads?").
				Description("Exact SHA256 match with files already inside ~/Epitech.").
				Options(opts...).
				Value(&picked),
		)).Run(); err != nil {
			return quiet(err)
		}
		for _, i := range picked {
			s := dupStrays[i]
			if err := os.Remove(s.src); err != nil {
				say(sYellow.Render("   ● ") + sMuted.Render(err.Error()))
			} else {
				say(sGreen.Render("   ✓ removed ") + sText.Render(filepath.Base(s.src)))
			}
		}
		say("")
	}

	// Fixer 2: remove editor leftovers and compiled binaries in bootstrap/.
	if len(cleanable) > 0 {
		var opts []huh.Option[int]
		var picked []int
		for i, c := range cleanable {
			label := sText.Render(rel(root, c.path)) + sFaint.Render("  "+c.reason)
			opts = append(opts, huh.NewOption(label, i))
		}
		if err := form(huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Clean leftover junk / bootstrap binaries?").
				Description("Select files to delete, or leave empty to keep everything.").
				Options(opts...).
				Value(&picked).
				Height(min(len(opts)+2, 12)),
		)).Run(); err != nil {
			return quiet(err)
		}
		for _, i := range picked {
			c := cleanable[i]
			if err := os.Remove(c.path); err != nil {
				say(sYellow.Render("   ● ") + sMuted.Render(err.Error()))
			} else {
				say(sGreen.Render("   ✓ removed ") + sText.Render(rel(root, c.path)))
			}
		}
		if len(picked) > 0 {
			say("")
		}
	}

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

// scanJunk finds editor leftovers, huge files, and compiled bootstrap binaries.
func scanJunk(p *workspace.Project) ([]string, []cleanableFile) {
	var out []string
	var clean []cleanableFile
	seen := map[string]bool{}
	for _, bin := range p.Bootstrap.Binaries {
		seen[bin] = true
		clean = append(clean, cleanableFile{path: bin, reason: "bootstrap binary"})
	}
	_ = filepath.WalkDir(p.Path, func(path string, d fs.DirEntry, err error) error {
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
			if !seen[path] {
				seen[path] = true
				clean = append(clean, cleanableFile{path: path, reason: "editor/valgrind leftover"})
			}
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > bigFile {
			out = append(out, path+sFaint.Render(fmt.Sprintf("  %d MB", info.Size()>>20)))
		}
		return nil
	})
	return out, clean
}

// findStrays matches recent PDFs in ~/Downloads to projects and classifies each
// as new, identical duplicate, or updated version.
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
		ext := strings.ToLower(filepath.Ext(c.Path))
		if ext == "" {
			ext = ".pdf"
		}
		dst := filepath.Join(best.Module.Path, "subjects", epitech.SubjectFile(best.Name, c.Kind, ext))
		kind := strayNew
		if workspace.Exists(dst) {
			if subjects.SameFile(c.Path, dst) {
				kind = strayDuplicate
			} else {
				// Also check whether it's an exact match of another already-filed subject of this project.
				dup := false
				for _, s := range best.Subjects {
					if subjects.SameFile(c.Path, s.Path) {
						dst = s.Path
						dup = true
						break
					}
				}
				if dup {
					kind = strayDuplicate
				} else {
					kind = strayUpdated
				}
			}
		}
		out = append(out, stray{src: c.Path, dst: dst, project: best.Name, kind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dst < out[j].dst })
	return out
}

func nextAvailablePath(dst string) string {
	ext := filepath.Ext(dst)
	stem := strings.TrimSuffix(dst, ext)
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s-%d%s", stem, n, ext)
		if !workspace.Exists(cand) {
			return cand
		}
	}
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
