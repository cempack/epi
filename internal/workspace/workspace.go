// Package workspace reads the on-disk layout:
//
//	<root>/tekN/<module>/<project>   git repos
//	<root>/tekN/<module>/subjects/   subject PDFs, kept outside the repos
package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cempack/epi/internal/epitech"
)

// Root is $EPI_ROOT, or ~/Epitech.
func Root() string {
	if r := os.Getenv("EPI_ROOT"); r != "" {
		return r
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Epitech")
}

type Git struct {
	IsRepo     bool
	Branch     string
	Dirty      int // modified/staged files
	Untracked  int
	Ahead      int // commits not pushed
	Behind     int
	LastCommit time.Time
	LastMsg    string
	Remote     string
}

func (g Git) Clean() bool { return g.Dirty == 0 && g.Untracked == 0 && g.Ahead == 0 }

type Project struct {
	Name     string
	Path     string
	Module   *Module
	Git      Git
	Subjects []string // absolute paths of this project's subject files
}

type Module struct {
	Tek      string // tek1
	Dir      string // psu100_unix
	Path     string
	Projects []*Project
	Subjects []string
}

// Code is the module code prefix of the folder: "psu100", "pool", "workshops".
func (m *Module) Code() string { return strings.SplitN(m.Dir, "_", 2)[0] }

// Label is the human part: "unix" for psu100_unix.
func (m *Module) Label() string {
	if parts := strings.SplitN(m.Dir, "_", 2); len(parts) == 2 {
		return strings.ReplaceAll(parts[1], "_", " ")
	}
	return m.Dir
}

var tekRe = regexp.MustCompile(`^tek\d+$`)

// Scan walks the tree and loads git state for every project concurrently.
func Scan(root string) ([]*Module, error) {
	teks, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var mods []*Module
	var projects []*Project
	for _, t := range teks {
		if !t.IsDir() || !tekRe.MatchString(t.Name()) {
			continue
		}
		tekPath := filepath.Join(root, t.Name())
		entries, _ := os.ReadDir(tekPath)
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			m := &Module{Tek: t.Name(), Dir: e.Name(), Path: filepath.Join(tekPath, e.Name())}
			inner, _ := os.ReadDir(m.Path)
			for _, p := range inner {
				switch {
				case p.Name() == "subjects" && p.IsDir():
					files, _ := os.ReadDir(filepath.Join(m.Path, "subjects"))
					for _, f := range files {
						if !f.IsDir() {
							m.Subjects = append(m.Subjects, filepath.Join(m.Path, "subjects", f.Name()))
						}
					}
				case p.IsDir() && !strings.HasPrefix(p.Name(), "."):
					pr := &Project{Name: p.Name(), Path: filepath.Join(m.Path, p.Name()), Module: m}
					m.Projects = append(m.Projects, pr)
					projects = append(projects, pr)
				}
			}
			mods = append(mods, m)
		}
	}
	// Attach subjects to projects: "my_ls.pdf" and "my_ls-*.pdf" belong to my_ls.
	for _, m := range mods {
		for _, p := range m.Projects {
			for _, s := range m.Subjects {
				base := strings.TrimSuffix(filepath.Base(s), filepath.Ext(s))
				if base == p.Name || strings.HasPrefix(base, p.Name+"-") {
					p.Subjects = append(p.Subjects, s)
				}
			}
		}
	}
	// Fan out: one goroutine per repo, joined by a WaitGroup.
	var wg sync.WaitGroup
	for _, p := range projects {
		wg.Add(1)
		go func(p *Project) { defer wg.Done(); p.Git = ReadGit(p.Path) }(p)
	}
	wg.Wait()
	sort.SliceStable(mods, func(i, j int) bool {
		if mods[i].Tek != mods[j].Tek {
			return mods[i].Tek < mods[j].Tek
		}
		return moduleRank(mods[i]) < moduleRank(mods[j])
	})
	return mods, nil
}

// Pool first, workshops last, everything else alphabetical in between.
func moduleRank(m *Module) string {
	switch m.Dir {
	case "pool":
		return "0"
	case "workshops":
		return "9"
	}
	return "5" + m.Dir
}

func git(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ReadGit uses porcelain v2: a stable, machine-readable status format.
func ReadGit(dir string) Git {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return Git{}
	}
	g := Git{IsRepo: true, Remote: git(dir, "remote", "get-url", "origin")}
	for _, line := range strings.Split(git(dir, "status", "--porcelain=v2", "--branch"), "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			g.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			f := strings.Fields(line)
			g.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
			g.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
		case strings.HasPrefix(line, "? "):
			g.Untracked++
		case len(line) > 1 && (line[0] == '1' || line[0] == '2' || line[0] == 'u'):
			g.Dirty++
		}
	}
	if log := git(dir, "log", "-1", "--format=%ct%x00%s"); log != "" {
		parts := strings.SplitN(log, "\x00", 2)
		if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			g.LastCommit = time.Unix(ts, 0)
		}
		if len(parts) == 2 {
			g.LastMsg = parts[1]
		}
	}
	return g
}

// RemoteKey normalizes any GitHub URL to "owner/name" (lowercase) so SSH and
// HTTPS remotes of the same repo compare equal.
func RemoteKey(url string) string { return strings.ToLower(RemoteName(url)) }

// RemoteName is "Owner/Name" with original casing, for display.
func RemoteName(url string) string {
	s := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	s = strings.TrimPrefix(s, "git@github.com:")
	if i := strings.Index(s, "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
	}
	return s
}

// ModuleDir decides which folder a repo goes in. It reuses an existing folder
// for the same code (so "psu100_unix" sticks even if the topic map changes),
// and otherwise builds "<code>_<topic>". askTopic is only called for module
// areas epi has never seen.
func ModuleDir(root string, r epitech.Repo, askTopic func(r epitech.Repo) (string, error)) (string, error) {
	if r.IsPool() {
		return "pool", nil
	}
	entries, _ := os.ReadDir(filepath.Join(root, r.Tek()))
	for _, e := range entries {
		if e.IsDir() && (e.Name() == r.Code() || strings.HasPrefix(e.Name(), r.Code()+"_")) {
			return e.Name(), nil
		}
	}
	topic, ok := epitech.Topics[r.Area]
	if !ok {
		t, err := askTopic(r)
		if err != nil {
			return "", err
		}
		topic = epitech.Snake(t)
	}
	if topic == "" {
		return r.Code(), nil
	}
	return r.Code() + "_" + topic, nil
}

// Exists reports whether a path exists.
func Exists(p string) bool { _, err := os.Stat(p); return err == nil }
