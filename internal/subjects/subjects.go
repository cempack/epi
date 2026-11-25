// Package subjects finds subject PDFs (usually in ~/Downloads), reads their
// title, searches across filed subjects, and moves them under <module>/subjects
// with canonical names.
package subjects

import (
	"crypto/sha256"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cempack/epi/internal/epitech"
)

type Candidate struct {
	Path    string
	ModTime time.Time
	Title   []string // first non-empty lines of page 1
	Kind    string   // "", bootstrap, kickoff, roadmap, annex, bonus, slides, examples
	Match   bool     // looks like it belongs to the project
	Subject bool     // looks like an Epitech subject at all
}

// Every Epitech subject's first page has a "< TAGLINE />" line under the title.
var taglineRe = regexp.MustCompile(`^<.*>$`)

// LooksLikeSubject tells Epitech subjects apart from the rest of ~/Downloads
// (invoices, IDs...), which epi should never offer to file.
func LooksLikeSubject(title []string) bool {
	for _, l := range title {
		if taglineRe.MatchString(l) {
			return true
		}
	}
	return false
}

// Downloads is $EPI_DOWNLOADS, or ~/Downloads.
func Downloads() string {
	if d := os.Getenv("EPI_DOWNLOADS"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// Title extracts the first few lines of page 1. Missing pdftotext is not fatal:
// we just fall back to filename matching.
func Title(path string) []string {
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		return nil
	}
	out, err := exec.Command("pdftotext", "-l", "1", path, "-").Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
			if len(lines) == 3 {
				break
			}
		}
	}
	return lines
}

// FullText returns the complete extracted text of a PDF (via pdftotext) or
// plain-text file.
func FullText(path string) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		out, err := exec.Command("pdftotext", path, "-").Output()
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Find scans dir for recent subject PDFs (and companion .txt/.md files that
// explicitly match slug), reads their titles in parallel, and marks matches.
func Find(dir, slug string, maxAge time.Duration, limit int) []Candidate {
	entries, _ := os.ReadDir(dir)
	var cands []Candidate
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".pdf" && ext != ".txt" && ext != ".md" {
			continue
		}
		// Non-PDF files are only candidates when we have a specific slug and
		// the filename mentions it (avoids showing random .txt files).
		if ext != ".pdf" && !Matches(slug, e.Name(), nil) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) > maxAge {
			continue
		}
		cands = append(cands, Candidate{Path: filepath.Join(dir, e.Name()), ModTime: info.ModTime()})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].ModTime.After(cands[j].ModTime) })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	var wg sync.WaitGroup
	for i := range cands {
		wg.Add(1)
		go func(c *Candidate) {
			defer wg.Done()
			c.Title = Title(c.Path)
			base := filepath.Base(c.Path)
			c.Kind = epitech.SubjectKind(base, strings.Join(c.Title, " "))
			c.Match = Matches(slug, base, c.Title)
			c.Subject = c.Match || LooksLikeSubject(c.Title)
		}(&cands[i])
	}
	wg.Wait()
	// Drop anything that isn't a subject, then matches first, most recent next.
	kept := cands[:0]
	for _, c := range cands {
		if c.Subject {
			kept = append(kept, c)
		}
	}
	cands = kept
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Match && !cands[j].Match })
	return cands
}

// Matches is true when the squashed slug appears in the filename or title:
// "Bootstrap_my_radar.pdf" -> "bootstrapmyradarpdf" contains "myradar".
func Matches(slug, filename string, title []string) bool {
	want := epitech.Squash(slug)
	if len(want) < 3 {
		return false
	}
	return strings.Contains(epitech.Squash(filename), want) ||
		strings.Contains(epitech.Squash(strings.Join(title, " ")), want)
}

// SameFile reports whether two files have identical size and SHA256 digest.
func SameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil || ia.Size() != ib.Size() {
		return false
	}
	ha, err := sha256File(a)
	if err != nil {
		return false
	}
	hb, err := sha256File(b)
	return err == nil && ha == hb
}

func sha256File(p string) ([32]byte, error) {
	var zero [32]byte
	f, err := os.Open(p)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return zero, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// Move renames src to dst, falling back to copy+delete across filesystems.
// It never overwrites: an existing dst is returned as os.ErrExist.
func Move(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return os.ErrExist
	}
	return moveFile(src, dst, false)
}

// Replace moves src onto dst, overwriting an existing dst only after the copy
// succeeds.
func Replace(src, dst string) error {
	return moveFile(src, dst, true)
}

func moveFile(src, dst string, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if overwrite {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
	} else {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if overwrite {
		tmp = dst + ".epi-tmp"
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	out, err := os.OpenFile(tmp, flags, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if overwrite {
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return os.Remove(src)
}

// SearchTarget identifies a filed document to search.
type SearchTarget struct {
	Owner string // e.g. "psu100_unix/my_sudo" or "docs"
	Label string // e.g. "my_sudo.pdf"
	Path  string
}

// SearchHit is one document that matched a query, with up to 3 snippet lines.
type SearchHit struct {
	Target   SearchTarget
	Snippets []string
	Count    int
}

// Search scans all targets concurrently for case-insensitive occurrences of query.
func Search(targets []SearchTarget, query string) []SearchHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sem  = make(chan struct{}, 8)
		hits []SearchHit
	)
	for _, t := range targets {
		wg.Add(1)
		go func(t SearchTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			text, err := FullText(t.Path)
			if err != nil {
				return
			}
			var snippets []string
			count := 0
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if strings.Contains(strings.ToLower(line), q) {
					count++
					if len(snippets) < 3 {
						snippets = append(snippets, line)
					}
				}
			}
			if count > 0 {
				mu.Lock()
				hits = append(hits, SearchHit{Target: t, Snippets: snippets, Count: count})
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Count != hits[j].Count {
			return hits[i].Count > hits[j].Count
		}
		return hits[i].Target.Path < hits[j].Target.Path
	})
	return hits
}
