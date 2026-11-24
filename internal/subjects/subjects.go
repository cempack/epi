// Package subjects finds subject PDFs (usually in ~/Downloads), reads their
// title, and files them under <module>/subjects with canonical names.
package subjects

import (
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
	Kind    string   // "", bootstrap, kickoff, roadmap
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

// Find scans dir for PDFs modified in the last maxAge, reads their titles in
// parallel and marks those whose filename or title contains the slug.
func Find(dir, slug string, maxAge time.Duration, limit int) []Candidate {
	entries, _ := os.ReadDir(dir)
	var cands []Candidate
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
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

// Move renames src to dst, falling back to copy+delete across filesystems.
// It never overwrites: an existing dst is returned as os.ErrExist.
func Move(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return os.ErrExist
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
