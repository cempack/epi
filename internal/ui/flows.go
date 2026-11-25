package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/cempack/epi/internal/epitech"
	"github.com/cempack/epi/internal/subjects"
	"github.com/cempack/epi/internal/workspace"
)

// pickProject resolves a query to a single project, falling back to an
// interactive selector when query is empty or ambiguous.
func pickProject(mods []*workspace.Module, query, title string) (*workspace.Project, error) {
	var pool []*workspace.Project
	if query != "" {
		hits := workspace.FindProjects(mods, query)
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			pool = hits
		} else {
			return nil, fmt.Errorf("no project matches %q", query)
		}
	} else {
		for _, m := range mods {
			pool = append(pool, m.Projects...)
		}
		sort.SliceStable(pool, func(i, j int) bool {
			return pool[i].LatestActivity().After(pool[j].LatestActivity())
		})
	}
	if len(pool) == 0 {
		return nil, errors.New("no projects found")
	}
	var opts []huh.Option[string]
	for _, p := range pool {
		label := sText.Render(fmt.Sprintf("%-18s", truncate(p.Name, 18))) +
			sMuted.Render(fmt.Sprintf("%-22s", truncate(p.Module.Dir, 22))) +
			sFaint.Render(Ago(p.LatestActivity()))
		opts = append(opts, huh.NewOption(label, p.Path))
	}
	var choice string
	if err := form(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Options(opts...).
			Value(&choice).
			Height(min(len(opts)+2, 14)),
	)).Run(); err != nil {
		return nil, quiet(err)
	}
	for _, p := range pool {
		if p.Path == choice {
			return p, nil
		}
	}
	return nil, errCancelled
}

// projectAtCwd returns the project containing the current working directory, if any.
func projectAtCwd(mods []*workspace.Module) *workspace.Project {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	cwd, _ = filepath.Abs(cwd)
	for _, m := range mods {
		for _, p := range m.Projects {
			if cwd == p.Path || strings.HasPrefix(cwd, p.Path+string(filepath.Separator)) {
				return p
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- subject

// RunSubject files one or more subjects (from explicit paths or ~/Downloads)
// into a project's module/subjects/ folder, letting you pick or customize the kind.
func RunSubject(root, projectQuery string, files []string) error {
	say("\n " + Header("file subjects") + "\n")
	mods, err := workspace.Scan(root)
	if err != nil {
		return err
	}
	var proj *workspace.Project
	if projectQuery == "" {
		proj = projectAtCwd(mods)
	}
	if proj == nil {
		proj, err = pickProject(mods, projectQuery, "Which project?")
		if err != nil {
			return err
		}
	}

	slug := proj.Name
	if r, err := epitech.Parse(proj.Git.Remote); err == nil {
		slug = r.Slug
	}

	var sources []subjects.Candidate
	if len(files) > 0 {
		for _, f := range files {
			abs, err := filepath.Abs(f)
			if err != nil || !workspace.Exists(abs) {
				return fmt.Errorf("file not found: %s", f)
			}
			title := subjects.Title(abs)
			sources = append(sources, subjects.Candidate{
				Path:  abs,
				Title: title,
				Kind:  epitech.SubjectKind(filepath.Base(abs), strings.Join(title, " ")),
				Match: true,
			})
		}
	} else {
		var cands []subjects.Candidate
		spin("Scanning "+tilde(subjects.Downloads()), func() {
			cands = subjects.Find(subjects.Downloads(), slug, 90*24*time.Hour, 50)
		})
		if len(cands) == 0 {
			say(sMuted.Render("  no subject files found in " + tilde(subjects.Downloads())))
			return nil
		}
		var opts []huh.Option[string]
		var picked []string
		for _, c := range cands {
			base := filepath.Base(c.Path)
			ext := strings.ToLower(filepath.Ext(base))
			label := sText.Render(base)
			if c.Match {
				label += sMuted.Render("  →  " + epitech.SubjectFile(proj.Name, c.Kind, ext))
			} else if len(c.Title) > 0 {
				label += sFaint.Render("  " + truncate(c.Title[0], 30))
			}
			opt := huh.NewOption(label, c.Path)
			if c.Match {
				opt = opt.Selected(true)
				picked = append(picked, c.Path)
			}
			opts = append(opts, opt)
		}
		if err := form(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Subjects for " + proj.Name).
				Description("Files will be moved into " + tilde(filepath.Join(proj.Module.Path, "subjects"))).
				Options(opts...).
				Value(&picked).
				Height(min(len(opts)+2, 14)),
		)).Run(); err != nil {
			return quiet(err)
		}
		if len(picked) == 0 {
			say(sFaint.Render("  nothing selected"))
			return nil
		}
		for _, p := range picked {
			for _, c := range cands {
				if c.Path == p {
					sources = append(sources, c)
					break
				}
			}
		}
	}

	subjDir := filepath.Join(proj.Module.Path, "subjects")
	for _, c := range sources {
		kind := c.Kind
		const customKind = "\x00custom"
		var kindOpts []huh.Option[string]
		for _, k := range epitech.KnownKinds {
			ext := strings.ToLower(filepath.Ext(c.Path))
			if ext == "" {
				ext = ".pdf"
			}
			lbl := fmt.Sprintf("%-10s %s", epitech.SubjectLabel(k), sMuted.Render(epitech.SubjectFile(proj.Name, k, ext)))
			kindOpts = append(kindOpts, huh.NewOption(lbl, k))
		}
		kindOpts = append(kindOpts, huh.NewOption("custom suffix…", customKind))

		if err := form(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Kind for " + filepath.Base(c.Path)).
				Description("Detected: " + epitech.SubjectLabel(c.Kind)).
				Options(kindOpts...).
				Value(&kind).
				Height(min(len(kindOpts)+2, 12)),
		)).Run(); err != nil {
			return quiet(err)
		}
		if kind == customKind {
			var custom string
			if err := form(huh.NewGroup(
				huh.NewInput().
					Title("Custom suffix").
					Placeholder("e.g. v2, protocol, guidelines").
					Value(&custom).
					Validate(func(s string) error {
						if epitech.Snake(s) == "" {
							return errors.New("enter at least one letter or digit")
						}
						return nil
					}),
			)).Run(); err != nil {
				return quiet(err)
			}
			kind = epitech.Snake(custom)
		}

		ext := strings.ToLower(filepath.Ext(c.Path))
		if ext == "" {
			ext = ".pdf"
		}
		dst := filepath.Join(subjDir, epitech.SubjectFile(proj.Name, kind, ext))
		if workspace.Exists(dst) {
			if subjects.SameFile(c.Path, dst) {
				rm := true
				if err := form(huh.NewGroup(
					huh.NewConfirm().
						Title(filepath.Base(dst) + " is already filed with identical content").
						Description("Remove the duplicate in " + tilde(filepath.Dir(c.Path)) + "?").
						Value(&rm),
				)).Run(); err != nil {
					return quiet(err)
				}
				if rm {
					_ = os.Remove(c.Path)
					say(sGreen.Render("  ✓ removed duplicate ") + sText.Render(filepath.Base(c.Path)))
				}
				continue
			}
			var clash string
			if err := form(huh.NewGroup(
				huh.NewSelect[string]().
					Title(filepath.Base(dst) + " already exists (different content)").
					Options(
						huh.NewOption("Replace existing "+filepath.Base(dst), "replace"),
						huh.NewOption("Keep both ("+filepath.Base(nextAvailablePath(dst))+")", "keep"),
						huh.NewOption("Skip", "skip"),
					).
					Value(&clash),
			)).Run(); err != nil {
				return quiet(err)
			}
			switch clash {
			case "replace":
				if err := subjects.Replace(c.Path, dst); err != nil {
					say(sRed.Render("  ✗ ") + err.Error())
				} else {
					say(sGreen.Render("  ✓ replaced ") + sText.Render(tilde(dst)))
				}
			case "keep":
				alt := nextAvailablePath(dst)
				if err := subjects.Move(c.Path, alt); err != nil {
					say(sRed.Render("  ✗ ") + err.Error())
				} else {
					say(sGreen.Render("  ✓ filed ") + sText.Render(tilde(alt)))
				}
			}
			continue
		}

		if err := subjects.Move(c.Path, dst); err != nil {
			say(sRed.Render("  ✗ ") + err.Error())
			continue
		}
		say(sGreen.Render("  ✓ filed ") + sText.Render(tilde(dst)))
	}
	say("")
	return nil
}

// --------------------------------------------------------------- bootstrap

// RunBootstrap initializes or tidies <project>/bootstrap/ (directory,
// .gitignore rules, and opening/filing the bootstrap PDF).
func RunBootstrap(root, projectQuery string) error {
	say("\n " + Header("bootstrap") + "\n")
	mods, err := workspace.Scan(root)
	if err != nil {
		return err
	}
	var proj *workspace.Project
	if projectQuery == "" {
		proj = projectAtCwd(mods)
	}
	if proj == nil {
		proj, err = pickProject(mods, projectQuery, "Bootstrap for which project?")
		if err != nil {
			return err
		}
	}

	bdir := filepath.Join(proj.Path, "bootstrap")
	if !proj.Bootstrap.HasDir {
		if err := os.MkdirAll(bdir, 0o755); err != nil {
			return err
		}
		say(sGreen.Render("  ✓ created ") + sText.Render(tilde(bdir)))
	} else {
		say(sMuted.Render("  ● exists ") + sText.Render(tilde(bdir)))
	}

	// Offer .gitignore protection so compiled binaries or bootstrap scratch code
	// don't trip Banana / moulinette.
	var ignoreChoice string
	if err := form(huh.NewGroup(
		huh.NewSelect[string]().
			Title(".gitignore for bootstrap/").
			Description("Keep compiled binaries (or the whole bootstrap/) out of deliveries.").
			Options(
				huh.NewOption("Ignore binaries only (/bootstrap/bootstrap, *.o, a.out)", "bins"),
				huh.NewOption("Ignore the whole /bootstrap/ folder", "all"),
				huh.NewOption("Leave .gitignore unchanged", "skip"),
			).
			Value(&ignoreChoice),
	)).Run(); err != nil {
		return quiet(err)
	}
	switch ignoreChoice {
	case "bins":
		if added, err := ensureGitignoreLines(proj.Path, []string{"/bootstrap/bootstrap", "/bootstrap/*.o", "/bootstrap/a.out"}); err == nil && added > 0 {
			say(sGreen.Render(fmt.Sprintf("  ✓ added %d rule(s) to .gitignore", added)))
		}
	case "all":
		if added, err := ensureGitignoreLines(proj.Path, []string{"/bootstrap/"}); err == nil && added > 0 {
			say(sGreen.Render("  ✓ added /bootstrap/ to .gitignore"))
		}
	}

	// Clean any stray binaries already sitting inside bootstrap/.
	if len(proj.Bootstrap.Binaries) > 0 {
		clean := true
		if err := form(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Remove %d compiled binary file(s) in bootstrap/?", len(proj.Bootstrap.Binaries))).
				Value(&clean),
		)).Run(); err == nil && clean {
			for _, bin := range proj.Bootstrap.Binaries {
				if os.Remove(bin) == nil {
					say(sGreen.Render("  ✓ removed ") + sText.Render(filepath.Base(bin)))
				}
			}
		}
	}

	// Open bootstrap PDF if present, or hint to file it.
	if s := proj.BootstrapSubject(); s != nil {
		openNow := true
		if err := form(huh.NewGroup(
			huh.NewConfirm().Title("Open " + s.Name + "?").Value(&openNow),
		)).Run(); err == nil && openNow {
			openFile(s.Path)
			say(sMuted.Render("  opened ") + sText.Render(s.Name))
		}
	} else {
		say(sMuted.Render("  no bootstrap PDF filed yet — run ") + sText.Render("epi subject "+proj.Name))
	}
	say("\n  " + sMuted.Render("jump in with ") + sText.Render("epi cd "+proj.Name+"/b") + "\n")
	return nil
}

func ensureGitignoreLines(repoPath string, want []string) (int, error) {
	giPath := filepath.Join(repoPath, ".gitignore")
	existing, _ := os.ReadFile(giPath)
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, w := range want {
		if !have[w] {
			add = append(add, w)
		}
	}
	if len(add) == 0 {
		return 0, nil
	}
	f, err := os.OpenFile(giPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		_, _ = f.WriteString("\n")
	}
	for _, line := range add {
		_, _ = f.WriteString(line + "\n")
	}
	return len(add), nil
}

// ------------------------------------------------------------------- open

// RunOpen opens a project's subject PDF from the CLI (`epi open sudo kickoff`).
func RunOpen(root, projectQuery, kindQuery string) error {
	mods, err := workspace.Scan(root)
	if err != nil {
		return err
	}
	var proj *workspace.Project
	if projectQuery == "" {
		proj = projectAtCwd(mods)
	}
	if proj == nil {
		proj, err = pickProject(mods, projectQuery, "Open subject for which project?")
		if err != nil {
			return err
		}
	}
	if len(proj.Subjects) == 0 {
		return fmt.Errorf("no subjects filed for %s (run: epi subject %s)", proj.Name, proj.Name)
	}
	if kindQuery == "all" {
		for _, s := range proj.Subjects {
			openFile(s.Path)
		}
		say(sGreen.Render(fmt.Sprintf("  ✓ opened %d subjects for %s", len(proj.Subjects), proj.Name)))
		return nil
	}
	if kindQuery != "" {
		s := proj.SubjectByKind(kindQuery)
		if s == nil {
			var have []string
			for _, x := range proj.Subjects {
				have = append(have, x.Label)
			}
			return fmt.Errorf("no %q subject for %s (available: %s)", kindQuery, proj.Name, strings.Join(have, ", "))
		}
		openFile(s.Path)
		say(sGreen.Render("  ✓ opened ") + sText.Render(s.Name))
		return nil
	}
	if len(proj.Subjects) == 1 {
		openFile(proj.Subjects[0].Path)
		say(sGreen.Render("  ✓ opened ") + sText.Render(proj.Subjects[0].Name))
		return nil
	}
	var opts []huh.Option[string]
	for _, s := range proj.Subjects {
		opts = append(opts, huh.NewOption(kindBadge(s.Kind)+" "+sText.Render(s.Name), s.Path))
	}
	var choice string
	if err := form(huh.NewGroup(
		huh.NewSelect[string]().Title("Subjects for " + proj.Name).Options(opts...).Value(&choice),
	)).Run(); err != nil {
		return quiet(err)
	}
	openFile(choice)
	say(sGreen.Render("  ✓ opened ") + sText.Render(filepath.Base(choice)))
	return nil
}

// ------------------------------------------------------------------- docs

// RunDocs opens a reference document from <root>/docs/.
func RunDocs(root, query string) error {
	docs := workspace.ScanDocs(root)
	if len(docs) == 0 {
		return fmt.Errorf("no documents in %s", tilde(filepath.Join(root, "docs")))
	}
	if query != "" {
		q := epitech.Squash(query)
		var hits []string
		for _, d := range docs {
			if strings.Contains(epitech.Squash(filepath.Base(d)), q) {
				hits = append(hits, d)
			}
		}
		if len(hits) == 1 {
			openFile(hits[0])
			say(sGreen.Render("  ✓ opened ") + sText.Render(filepath.Base(hits[0])))
			return nil
		}
		if len(hits) > 1 {
			docs = hits
		} else {
			return fmt.Errorf("no doc matches %q", query)
		}
	}
	if len(docs) == 1 {
		openFile(docs[0])
		say(sGreen.Render("  ✓ opened ") + sText.Render(filepath.Base(docs[0])))
		return nil
	}
	var opts []huh.Option[string]
	for _, d := range docs {
		opts = append(opts, huh.NewOption(filepath.Base(d), d))
	}
	var choice string
	if err := form(huh.NewGroup(
		huh.NewSelect[string]().Title("Epitech docs").Options(opts...).Value(&choice),
	)).Run(); err != nil {
		return quiet(err)
	}
	openFile(choice)
	say(sGreen.Render("  ✓ opened ") + sText.Render(filepath.Base(choice)))
	return nil
}

// ----------------------------------------------------------------- search

// RunSearch runs full-text search across every filed subject and reference doc.
func RunSearch(root, query string) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("usage: epi search <query>")
	}
	mods, err := workspace.Scan(root)
	if err != nil {
		return err
	}
	var targets []subjects.SearchTarget
	seen := map[string]bool{}
	for _, m := range mods {
		for _, s := range m.Subjects {
			if seen[s] {
				continue
			}
			seen[s] = true
			targets = append(targets, subjects.SearchTarget{
				Owner: m.Tek + "/" + m.Dir,
				Label: filepath.Base(s),
				Path:  s,
			})
		}
	}
	for _, d := range workspace.ScanDocs(root) {
		targets = append(targets, subjects.SearchTarget{
			Owner: "docs",
			Label: filepath.Base(d),
			Path:  d,
		})
	}
	var hits []subjects.SearchHit
	spin(fmt.Sprintf("Searching %d documents for %q", len(targets), query), func() {
		hits = subjects.Search(targets, query)
	})
	say("")
	say(" " + Header("search") + "  " + sMuted.Render(fmt.Sprintf("%q · %d file(s)", query, len(hits))))
	say(" " + Rule(min(TermWidth()-2, 64)))
	if len(hits) == 0 {
		say("  " + sMuted.Render("no matches found") + "\n")
		return nil
	}
	for _, h := range hits {
		say("  " + sBold.Render(h.Target.Label) + "  " + Pill(fmt.Sprintf("%d hit", h.Count), cAccent) + "  " + sFaint.Render(h.Target.Owner))
		for _, sn := range h.Snippets {
			say("    " + sMuted.Render("│ "+truncate(sn, min(TermWidth()-10, 74))))
		}
		say("")
	}
	return nil
}

// ------------------------------------------------------------------- sync

// RunSync fetches all git repositories concurrently and reports status.
func RunSync(root string) error {
	mods, err := workspace.Scan(root)
	if err != nil {
		return err
	}
	var results []workspace.FetchResult
	spin("Fetching all Epitech repositories", func() {
		results = workspace.FetchAll(mods)
	})
	say("")
	say(" " + Header("sync"))
	say(" " + Rule(min(TermWidth()-2, 64)))
	behind, ahead, errs := 0, 0, 0
	for _, r := range results {
		p := r.Project
		if r.Err != nil {
			errs++
			say("  " + sRed.Render("✗ ") + sText.Render(rel(root, p.Path)) + sFaint.Render("  fetch failed"))
			continue
		}
		if p.Git.Behind > 0 {
			behind++
			say("  " + sCyan.Render("↓ ") + sText.Render(rel(root, p.Path)) + sMuted.Render(fmt.Sprintf("  %d behind origin", p.Git.Behind)))
		}
		if p.Git.Ahead > 0 {
			ahead++
			say("  " + sAccent.Render("↑ ") + sText.Render(rel(root, p.Path)) + sMuted.Render(fmt.Sprintf("  %d unpushed", p.Git.Ahead)))
		}
	}
	if behind == 0 && ahead == 0 && errs == 0 {
		say("  " + sGreen.Render(fmt.Sprintf("✓ all %d repositories up to date with origin", len(results))))
	}
	say("")
	return nil
}

// ---------------------------------------------------------------- allowed

// RunAllowed extracts authorized/forbidden functions from a project's main
// subject PDF and optionally saves them for epitech-predeliver.
func RunAllowed(root, projectQuery string, save bool) error {
	mods, err := workspace.Scan(root)
	if err != nil {
		return err
	}
	var proj *workspace.Project
	if projectQuery == "" {
		proj = projectAtCwd(mods)
	}
	if proj == nil {
		proj, err = pickProject(mods, projectQuery, "Authorized functions for which project?")
		if err != nil {
			return err
		}
	}
	subj := proj.MainSubject()
	if subj == nil {
		return fmt.Errorf("no main subject PDF filed for %s", proj.Name)
	}
	text, err := subjects.FullText(subj.Path)
	if err != nil {
		return fmt.Errorf("could not read %s (is pdftotext installed?): %w", subj.Name, err)
	}
	info := epitech.ParseAuthorized(text)
	say("")
	say(" " + Header("allowed"))
	var body strings.Builder
	body.WriteString(sFaint.Render("source   ") + sText.Render(subj.Name) + "\n")
	if info.Note != "" {
		body.WriteString(sFaint.Render("policy   ") + sText.Render(info.Note) + "\n")
	}
	if len(info.Allowed) > 0 {
		body.WriteString(sFaint.Render("allowed  ") + sGreen.Render(strings.Join(info.Allowed, ", ")) + "\n")
	}
	if len(info.Forbidden) > 0 {
		body.WriteString(sFaint.Render("banned   ") + sRed.Render(strings.Join(info.Forbidden, ", ")) + "\n")
	}
	if len(info.Allowed) == 0 && len(info.Forbidden) == 0 && info.Note == "" {
		body.WriteString(sYellow.Render("couldn't find an Authorized Functions section in this PDF"))
	}
	say(IndentBox(proj.Name, body.String(), min(max(TermWidth()-4, 48), 76), false))

	if len(info.Allowed) > 0 {
		dst := workspace.AllowedFilePath(proj.Path)
		if !save {
			say("  " + sMuted.Render("save for epitech-predeliver: ") + sKey.Render("epi allowed "+proj.Name+" --save"))
		} else {
			sorted := append([]string(nil), info.Allowed...)
			sort.Strings(sorted)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, []byte(strings.Join(sorted, "\n")+"\n"), 0o644); err != nil {
				return err
			}
			say("  " + sGreen.Render("✓ saved to ") + sText.Render(tilde(dst)))
			say("  " + sMuted.Render("epitech-predeliver will now use this list automatically"))
		}
	}
	say("")
	return nil
}

// --------------------------------------------------------------------- cd

// ResolveCd resolves a fuzzy project query (including "<project>/b" or
// "<project>/bootstrap" for the bootstrap subdirectory) to a directory path.
func ResolveCd(root, query string) (string, error) {
	mods, err := workspace.Scan(root)
	if err != nil {
		return "", err
	}
	wantBoot := false
	q := strings.TrimSpace(query)
	for _, suf := range []string{"/bootstrap", "/boot", "/b"} {
		if strings.HasSuffix(strings.ToLower(q), suf) {
			wantBoot = true
			q = q[:len(q)-len(suf)]
			break
		}
	}
	if q == "" {
		if p := projectAtCwd(mods); p != nil && wantBoot {
			if p.Bootstrap.HasDir {
				return p.Bootstrap.Dir, nil
			}
			return "", fmt.Errorf("%s has no bootstrap/ folder", p.Name)
		}
		return root, nil
	}
	// Module-level match (e.g. `epi cd pool` or `epi cd psu100`).
	for _, m := range mods {
		if strings.EqualFold(m.Dir, q) || strings.EqualFold(m.Code(), q) {
			return m.Path, nil
		}
	}
	proj, err := pickProject(mods, q, "Jump to which project?")
	if err != nil {
		return "", err
	}
	if wantBoot {
		if proj.Bootstrap.HasDir {
			return proj.Bootstrap.Dir, nil
		}
		return "", fmt.Errorf("%s has no bootstrap/ folder (create one with: epi bootstrap %s)", proj.Name, proj.Name)
	}
	return proj.Path, nil
}
