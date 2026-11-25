// Package workspace reads the on-disk layout:
//
//	<root>/tekN/<module>/<project>   git repos
//	<root>/tekN/<module>/subjects/   subject PDFs, kept outside the repos
//	<root>/docs/                     shared reference docs (coding style, etc.)
package workspace

import (
	"bytes"
	"context"
	"io/fs"
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

// Subject is a filed document belonging to a project.
type Subject struct {
	Path  string
	Name  string // base filename, e.g. my_sudo-bootstrap.pdf
	Kind  string // "", "bootstrap", "kickoff", "roadmap", ...
	Label string // "main", "bootstrap", ...
}

// Bootstrap describes the optional <project>/bootstrap/ workspace inside a repo.
type Bootstrap struct {
	HasDir   bool
	Dir      string
	Tracked  bool     // any file under bootstrap/ is tracked by git
	Binaries []string // compiled ELF binaries or .o files inside bootstrap/
}

type Git struct {
	IsRepo     bool
	Branch     string
	Dirty      int // modified/staged files
	Untracked  int
	Ahead      int // commits not pushed
	Behind     int
	LastCommit time.Time
	LastAuthor string
	LastMsg    string
	Remote     string
}

func (g Git) Clean() bool { return g.Dirty == 0 && g.Untracked == 0 && g.Ahead == 0 }

type Project struct {
	Name      string
	Path      string
	Module    *Module
	Git       Git
	Subjects  []Subject // semantically sorted: main, bootstrap, kickoff, roadmap, ...
	Bootstrap Bootstrap
	HasMain   bool      // root Makefile, src/ or include/ exists
	ModTime   time.Time // fallback activity timestamp when there are no commits yet
}

// MainSubject returns the project's primary subject (kind == ""), or nil.
func (p *Project) MainSubject() *Subject { return p.SubjectByKind("") }

// BootstrapSubject returns the project's bootstrap subject, or nil.
func (p *Project) BootstrapSubject() *Subject { return p.SubjectByKind("bootstrap") }

// SubjectByKind finds the first subject whose kind matches (exact or prefix).
func (p *Project) SubjectByKind(kind string) *Subject {
	q := strings.ToLower(strings.TrimSpace(kind))
	if q == "main" {
		q = ""
	}
	for i := range p.Subjects {
		if p.Subjects[i].Kind == q {
			return &p.Subjects[i]
		}
	}
	if q != "" {
		for i := range p.Subjects {
			if strings.HasPrefix(p.Subjects[i].Kind, q) {
				return &p.Subjects[i]
			}
		}
	}
	return nil
}

// InBootstrapPhase reports whether the project appears to still be in its
// bootstrap stage (has a bootstrap folder or bootstrap PDF, but no root
// Makefile/src yet).
func (p *Project) InBootstrapPhase() bool {
	return (p.Bootstrap.HasDir || p.BootstrapSubject() != nil) && !p.HasMain
}

// LatestActivity returns the latest commit time, falling back to filesystem mtime.
func (p *Project) LatestActivity() time.Time {
	if !p.Git.LastCommit.IsZero() {
		return p.Git.LastCommit
	}
	return p.ModTime
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

// Scan walks the tree and loads git and bootstrap state concurrently.
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
					if info, err := p.Info(); err == nil {
						pr.ModTime = info.ModTime()
					}
					m.Projects = append(m.Projects, pr)
					projects = append(projects, pr)
				}
			}
			mods = append(mods, m)
		}
	}
	// Attach subjects to projects and sort them semantically (main first,
	// then bootstrap, kickoff, roadmap, extras).
	for _, m := range mods {
		for _, p := range m.Projects {
			for _, s := range m.Subjects {
				name := filepath.Base(s)
				base := strings.TrimSuffix(name, filepath.Ext(name))
				if base == p.Name || strings.HasPrefix(base, p.Name+"-") {
					kind := epitech.SubjectKindFromFile(p.Name, name)
					p.Subjects = append(p.Subjects, Subject{
						Path:  s,
						Name:  name,
						Kind:  kind,
						Label: epitech.SubjectLabel(kind),
					})
				}
			}
			sort.Slice(p.Subjects, func(i, j int) bool {
				oi, oj := epitech.SubjectOrder(p.Subjects[i].Kind), epitech.SubjectOrder(p.Subjects[j].Kind)
				if oi != oj {
					return oi < oj
				}
				return p.Subjects[i].Name < p.Subjects[j].Name
			})
		}
	}
	// Fan out: one goroutine per project, joined by a WaitGroup.
	var wg sync.WaitGroup
	for _, p := range projects {
		wg.Add(1)
		go func(p *Project) {
			defer wg.Done()
			p.Git = ReadGit(p.Path)
			p.HasMain = hasMainFiles(p.Path)
			p.Bootstrap = readBootstrap(p.Path, p.Git.IsRepo)
		}(p)
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

// ScanDocs returns sorted file paths inside <root>/docs/.
func ScanDocs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(root, "docs", e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func hasMainFiles(dir string) bool {
	for _, name := range []string{"Makefile", "src", "include"} {
		if Exists(filepath.Join(dir, name)) {
			return true
		}
	}
	return false
}

func readBootstrap(dir string, isRepo bool) Bootstrap {
	bdir := filepath.Join(dir, "bootstrap")
	info, err := os.Stat(bdir)
	if err != nil || !info.IsDir() {
		return Bootstrap{}
	}
	b := Bootstrap{HasDir: true, Dir: bdir}
	if isRepo {
		if out := git(dir, "ls-files", "bootstrap"); out != "" {
			b.Tracked = true
		}
	}
	_ = filepath.WalkDir(bdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".o") || strings.HasSuffix(name, ".a") || IsELF(path) {
			b.Binaries = append(b.Binaries, path)
		}
		return nil
	})
	return b
}

var elfMagic = []byte{0x7f, 'E', 'L', 'F'}

// IsELF reports whether path starts with the ELF executable/object header.
func IsELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [4]byte
	n, err := f.Read(buf[:])
	return err == nil && n == 4 && bytes.Equal(buf[:], elfMagic)
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
			if len(f) >= 4 {
				g.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
				g.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
			}
		case strings.HasPrefix(line, "? "):
			g.Untracked++
		case len(line) > 1 && (line[0] == '1' || line[0] == '2' || line[0] == 'u'):
			g.Dirty++
		}
	}
	if log := git(dir, "log", "-1", "--format=%ct%x00%an%x00%s"); log != "" {
		parts := strings.SplitN(log, "\x00", 3)
		if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			g.LastCommit = time.Unix(ts, 0)
		}
		if len(parts) >= 2 {
			g.LastAuthor = parts[1]
		}
		if len(parts) == 3 {
			g.LastMsg = parts[2]
		}
	}
	return g
}

// FetchResult holds the outcome of fetching one repository.
type FetchResult struct {
	Project *Project
	Err     error
}

// FetchAll runs `git fetch --all --prune --quiet` across every repo
// concurrently and refreshes their Git state.
func FetchAll(mods []*Module) []FetchResult {
	var repos []*Project
	for _, m := range mods {
		for _, p := range m.Projects {
			if p.Git.IsRepo && p.Git.Remote != "" {
				repos = append(repos, p)
			}
		}
	}
	results := make([]FetchResult, len(repos))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, p := range repos {
		wg.Add(1)
		go func(i int, p *Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "git", "-C", p.Path, "fetch", "--all", "--prune", "--quiet")
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
			err := cmd.Run()
			if err == nil {
				p.Git = ReadGit(p.Path)
			}
			results[i] = FetchResult{Project: p, Err: err}
		}(i, p)
	}
	wg.Wait()
	return results
}

// FindProjects matches a query against project names (exact, squashed, prefix,
// or substring) and returns hits ordered from strongest to weakest.
func FindProjects(mods []*Module, query string) []*Project {
	q := strings.ToLower(strings.TrimSpace(query))
	sq := epitech.Squash(q)
	if sq == "" {
		return nil
	}
	var exact, prefix, sub []*Project
	for _, m := range mods {
		for _, p := range m.Projects {
			low := strings.ToLower(p.Name)
			psq := epitech.Squash(p.Name)
			full := strings.ToLower(m.Dir + "/" + p.Name)
			switch {
			case low == q || full == q || psq == sq:
				exact = append(exact, p)
			case strings.HasPrefix(low, q) || strings.HasPrefix(psq, sq):
				prefix = append(prefix, p)
			case strings.Contains(low, q) || strings.Contains(psq, sq) || strings.Contains(epitech.Squash(m.Dir+p.Name), sq):
				sub = append(sub, p)
			}
		}
	}
	if len(exact) > 0 {
		return exact
	}
	if len(prefix) > 0 {
		return prefix
	}
	return sub
}

// AllowedFilePath returns the file path where epitech-predeliver stores a
// project's remembered authorized-functions list.
func AllowedFilePath(projectPath string) string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	encoded := strings.ReplaceAll(projectPath, "/", "%")
	return filepath.Join(dataHome, "epitech-dump-arch", "allowed", encoded)
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
