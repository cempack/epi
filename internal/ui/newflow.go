package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/cempack/epi/internal/epitech"
	"github.com/cempack/epi/internal/remote"
	"github.com/cempack/epi/internal/subjects"
	"github.com/cempack/epi/internal/workspace"
)

var errCancelled = errors.New("cancelled")

func form(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(Theme()).WithOutput(os.Stderr)
}

func spin(title string, fn func()) {
	_ = spinner.New().Title(" " + title).Style(sAccent).TitleStyle(sMuted).Output(os.Stderr).Action(fn).Run()
}

func say(s string) { fmt.Fprintln(os.Stderr, s) }

// card draws a bordered box that never exceeds the terminal width: content
// that is too wide wraps inside the border instead of breaking it.
func card(st lipgloss.Style, s string) string {
	w, _, err := term.GetSize(os.Stderr.Fd())
	if err != nil || w <= 0 {
		w = 80
	}
	frame := st.GetHorizontalFrameSize()
	if lipgloss.Width(s)+frame > w-1 {
		st = st.Width(w - 1 - st.GetHorizontalBorderSize())
	}
	return st.Render(s)
}

// localIndex summarizes what is already on disk.
type localIndex struct {
	byRemote  map[string]*workspace.Project // "owner/name" -> project
	owners    []string
	preferSSH bool
	mods      []*workspace.Module
}

// subjectHints returns the names of every filed subject ("count_island",
// "my_ls-bootstrap"). They're used as naming evidence when no PDF is at hand.
func subjectHints(mods []*workspace.Module) []string {
	var hints []string
	for _, m := range mods {
		for _, s := range m.Subjects {
			hints = append(hints, strings.TrimSuffix(filepath.Base(s), filepath.Ext(s)))
		}
	}
	return hints
}

func indexLocal(root string) localIndex {
	mods, _ := workspace.Scan(root)
	idx := localIndex{byRemote: map[string]*workspace.Project{}, mods: mods}
	seen := map[string]bool{}
	ssh, https := 0, 0
	for _, m := range mods {
		for _, p := range m.Projects {
			if p.Git.Remote == "" {
				continue
			}
			key := workspace.RemoteKey(p.Git.Remote)
			idx.byRemote[key] = p
			if strings.HasPrefix(p.Git.Remote, "git@") {
				ssh++
			} else {
				https++
			}
			if r, err := epitech.Parse(p.Git.Remote); err == nil && !seen[r.Owner] {
				seen[r.Owner] = true
				idx.owners = append(idx.owners, r.Owner)
			}
		}
	}
	idx.preferSSH = ssh >= https
	return idx
}

// RunNew is the full "add a project" flow. arg may be empty, a repo name, or a URL.
func RunNew(root, arg string) error {
	say("\n " + Header("new project") + "\n")
	idx := indexLocal(root)

	repo, err := chooseRepo(idx, arg)
	if err != nil {
		return quiet(err)
	}
	if p, ok := idx.byRemote[strings.ToLower(repo.Owner+"/"+repo.Name)]; ok {
		say(sYellow.Render("  ● already cloned at ") + sText.Render(p.Path))
		return nil
	}

	// Subjects: read titles in ~/Downloads and keep the ones mentioning the slug.
	var cands []subjects.Candidate
	spin("Reading subjects in "+tilde(subjects.Downloads()), func() {
		cands = subjects.Find(subjects.Downloads(), repo.Slug, 45*24*time.Hour, 40)
	})
	var titles []string // from matching PDFs: the strongest naming evidence
	for _, c := range cands {
		if c.Match {
			titles = append(titles, c.Title...)
		}
	}

	modDir, err := workspace.ModuleDir(root, repo.Repo, askTopic)
	if err != nil {
		return quiet(err)
	}
	modPath := filepath.Join(root, repo.Tek(), modDir)

	// Name.
	name := epitech.SuggestName(repo.Slug, append(titles, subjectHints(idx.mods)...))
	hint := "from repo slug " + sMuted.Render(repo.Slug)
	if len(titles) > 0 {
		hint += sMuted.Render(" + subject title ") + sMuted.Render(strings.Trim(titles[0], "_ "))
	}
	err = form(huh.NewGroup(
		huh.NewInput().
			Title("Project name").
			Description(hint).
			Value(&name).
			Validate(func(s string) error {
				if !epitech.IsSnake(s) {
					return errors.New("use lowercase snake_case, like my_ls")
				}
				if workspace.Exists(filepath.Join(modPath, s)) {
					return fmt.Errorf("%s already exists in %s", s, modDir)
				}
				return nil
			}),
	)).Run()
	if err != nil {
		return quiet(err)
	}

	// Subject PDFs to file.
	plan := map[string]string{} // src -> dst
	if len(cands) > 0 {
		var picked []string
		var opts []huh.Option[string]
		for _, c := range cands {
			base := filepath.Base(c.Path)
			label := sText.Render(base)
			if c.Match {
				label += sMuted.Render("  →  " + epitech.SubjectFile(name, c.Kind, ".pdf"))
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
		desc := "Matched by filename and title. They'll be moved, not copied."
		if len(picked) == 0 {
			desc = "These look like Epitech subjects but don't mention " + repo.Slug + ". Pick any that belong, or skip."
		}
		err = form(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Subjects from ~/Downloads").
				Description(desc).
				Options(opts...).
				Value(&picked).
				Height(min(len(opts)+2, 14)),
		)).Run()
		if err != nil {
			return quiet(err)
		}
		used := map[string]bool{}
		for _, m := range idx.mods {
			if m.Path == modPath {
				for _, s := range m.Subjects {
					used[filepath.Base(s)] = true
				}
			}
		}
		for _, src := range picked {
			var c subjects.Candidate
			for _, x := range cands {
				if x.Path == src {
					c = x
				}
			}
			dst := epitech.SubjectFile(name, c.Kind, ".pdf")
			for n := 2; used[dst]; n++ { // two "bootstrap"s? -> my_x-bootstrap-2.pdf
				dst = epitech.SubjectFile(name, strings.TrimPrefix(c.Kind+fmt.Sprintf("-%d", n), "-"), ".pdf")
			}
			used[dst] = true
			plan[src] = filepath.Join(modPath, "subjects", dst)
		}
	}

	// Plan + confirm.
	dest := filepath.Join(modPath, name)
	cloneURL := repo.CloneURL(idx.preferSSH)
	var b strings.Builder
	row := func(icon, k, v string) { b.WriteString(icon + " " + sFaint.Render(fmt.Sprintf("%-8s", k)) + v + "\n") }
	row(sMuted.Render("⎇"), "clone", sText.Render(repo.Owner+"/"+repo.Name))
	row(sMuted.Render("→"), "into", sText.Render(tilde(dest)))
	if !workspace.Exists(modPath) {
		row(sMuted.Render("✦"), "module", sMuted.Render("new folder ")+sText.Render(modDir))
	}
	for src, dst := range plan {
		row(sMuted.Render("▤"), "subject", sText.Render(filepath.Base(src))+sMuted.Render(" → ")+sText.Render(filepath.Base(dst)))
	}
	if len(plan) == 0 {
		row(sFaint.Render("▤"), "subject", sFaint.Render("none — run epi doctor later to file it"))
	}
	say(card(sCard, strings.TrimRight(b.String(), "\n")))

	ok := true
	if err := form(huh.NewGroup(huh.NewConfirm().Title("Go?").Affirmative("Do it").Negative("Cancel").Value(&ok))).Run(); err != nil || !ok {
		return quiet(errCancelled)
	}

	// Execute.
	var cloneErr error
	var cloneOut []byte
	newModule := !workspace.Exists(modPath)
	spin("Cloning "+repo.Name, func() {
		_ = os.MkdirAll(modPath, 0o755)
		cmd := exec.Command("git", "clone", "--quiet", cloneURL, dest)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		cloneOut, cloneErr = cmd.CombinedOutput()
	})
	if cloneErr != nil {
		if newModule {
			_ = os.Remove(modPath) // only removes it if it's still empty
		}
		say(sRed.Render("  ✗ clone failed") + "\n" + sMuted.Render(indent(strings.TrimSpace(string(cloneOut)))))
		if strings.Contains(string(cloneOut), "Permission denied") || strings.Contains(string(cloneOut), "passphrase") {
			say(sMuted.Render("  hint: unlock your SSH key with ") + sText.Render("ssh-add") + sMuted.Render(" and retry"))
		}
		return errCancelled
	}
	var filed []string
	for src, dst := range plan {
		if err := subjects.Move(src, dst); err != nil {
			say(sYellow.Render("  ● couldn't move "+filepath.Base(src)+": ") + sMuted.Render(err.Error()))
			continue
		}
		filed = append(filed, filepath.Base(dst))
	}

	// Done.
	var d strings.Builder
	d.WriteString(sGreen.Render("✓ ") + sBold.Render(name) + " is ready\n\n")
	d.WriteString(sMuted.Render("path     ") + sText.Render(tilde(dest)) + "\n")
	if len(filed) > 0 {
		d.WriteString(sFaint.Render("subjects ") + sText.Render(strings.Join(filed, ", ")) + "\n")
	}
	d.WriteString("\n" + sMuted.Render("jump in with ") + sText.Render("cd "+tilde(dest)))
	say("\n" + card(sCard, d.String()) + "\n")
	return nil
}

// chooseRepo resolves the argument, or shows a picker of repos you haven't cloned.
func chooseRepo(idx localIndex, arg string) (remote.Repo, error) {
	var all []remote.Repo
	var listErr error
	spin("Asking GitHub for your Epitech repos", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		all, listErr = remote.List(ctx, remote.Orgs(ctx, idx.owners))
	})

	var fresh []remote.Repo
	for _, r := range all {
		if _, ok := idx.byRemote[strings.ToLower(r.Owner+"/"+r.Name)]; !ok {
			fresh = append(fresh, r)
		}
	}

	if arg != "" {
		r, err := epitech.Parse(arg)
		if err != nil {
			// Not a full repo name: treat it as a project name ("my_radar",
			// "countisland") and look it up among your GitHub repos.
			want := epitech.Squash(arg)
			var hits, freshHits []remote.Repo
			for _, x := range all {
				if epitech.Squash(x.Slug) == want || epitech.Squash(epitech.SuggestName(x.Slug, nil)) == want {
					hits = append(hits, x)
					if _, ok := idx.byRemote[strings.ToLower(x.Owner+"/"+x.Name)]; !ok {
						freshHits = append(freshHits, x)
					}
				}
			}
			switch {
			case len(hits) == 0 && listErr != nil:
				return remote.Repo{}, fmt.Errorf("can't search GitHub (%v) — pass the full repo URL", listErr)
			case len(hits) == 0:
				return remote.Repo{}, fmt.Errorf("no repo on GitHub matches %q", arg)
			case len(freshHits) == 1:
				return freshHits[0], nil
			case len(freshHits) == 0:
				return hits[0], nil // already cloned: caller reports where
			}
			fresh = freshHits // several candidates: let the picker decide
		} else {
			for _, x := range all { // fill owner/URLs from GitHub when we can
				if strings.EqualFold(x.Name, r.Name) && (r.Owner == "" || strings.EqualFold(x.Owner, r.Owner)) {
					return x, nil
				}
			}
			if r.Owner == "" {
				return remote.Repo{}, fmt.Errorf("couldn't find %s on GitHub — pass the full URL", r.Name)
			}
			return remote.Repo{Repo: r,
				SSHURL: "git@github.com:" + r.Owner + "/" + r.Name + ".git",
				URL:    "https://github.com/" + r.Owner + "/" + r.Name}, nil
		}
	}
	if listErr != nil && len(all) == 0 {
		say(sYellow.Render("  ● GitHub unavailable: ") + sMuted.Render(listErr.Error()))
	}

	// Names already on disk, to flag duplicates like a second my_printf repo.
	have := map[string]string{}
	for _, m := range idx.mods {
		for _, p := range m.Projects {
			have[epitech.Squash(p.Name)] = p.Name
		}
	}

	hints := subjectHints(idx.mods)
	const manual = "\x00manual"
	var opts []huh.Option[string]
	for _, r := range fresh {
		guess := epitech.SuggestName(r.Slug, hints)
		label := sText.Render(fmt.Sprintf("%-16s", truncate(guess, 16))) +
			sMuted.Render(fmt.Sprintf("%-10s", r.Area+"-"+r.Number)) +
			sFaint.Render(Ago(r.CreatedAt)+" ago")
		if dup, ok := have[epitech.Squash(guess)]; ok {
			label += sYellow.Render("  · you already have " + dup)
		}
		opts = append(opts, huh.NewOption(label, r.Owner+"/"+r.Name))
	}
	opts = append(opts, huh.NewOption(sMuted.Render("✎  paste a repo URL…"), manual))

	title := "Which project?"
	desc := fmt.Sprintf("%d repos on GitHub you haven't cloned yet · newest first", len(fresh))
	if len(fresh) == 0 {
		desc = "Everything on GitHub is already cloned ✓"
	}
	var choice string
	if err := form(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Description(desc).Options(opts...).Value(&choice).Height(min(len(opts)+2, 14)),
	)).Run(); err != nil {
		return remote.Repo{}, err
	}
	if choice == manual {
		var url string
		if err := form(huh.NewGroup(
			huh.NewInput().Title("Repo URL").Placeholder("git@github.com:Org/G-XXX-000-TLS-1-1-name-1.git").Value(&url).
				Validate(func(s string) error { _, err := epitech.Parse(s); return err }),
		)).Run(); err != nil {
			return remote.Repo{}, err
		}
		return chooseRepo(localIndex{byRemote: idx.byRemote, owners: idx.owners, preferSSH: idx.preferSSH, mods: idx.mods}, url)
	}
	for _, r := range fresh {
		if r.Owner+"/"+r.Name == choice {
			return r, nil
		}
	}
	return remote.Repo{}, errCancelled
}

// askTopic is called once for a module area epi has never seen (e.g. "AIA").
func askTopic(r epitech.Repo) (string, error) {
	var topic string
	err := form(huh.NewGroup(
		huh.NewInput().
			Title("New module " + r.ModuleID()).
			Description("A short topic for the folder name: " + r.Code() + "_<topic>. Leave empty for just " + r.Code() + ".").
			Placeholder("e.g. networking").
			Value(&topic),
	)).Run()
	return topic, err
}

func quiet(err error) error {
	if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, errCancelled) {
		say(sFaint.Render("  cancelled — nothing changed"))
		return errCancelled
	}
	return err
}

func tilde(p string) string {
	home, _ := os.UserHomeDir()
	return strings.Replace(p, home, "~", 1)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func indent(s string) string {
	return lipgloss.NewStyle().PaddingLeft(4).Render(s)
}
