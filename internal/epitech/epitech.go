// Package epitech understands Epitech repository names and turns them into
// clean, predictable folder names. It does no I/O, so it is easy to test.
package epitech

import (
	"fmt"
	"regexp"
	"strings"
)

// Repo is a parsed Epitech repository name such as
// "G-PSU-100-TLS-1-1-myls-1".
type Repo struct {
	Name     string // full repo name, e.g. G-PSU-100-TLS-1-1-myls-1
	Owner    string // GitHub org, e.g. EpitechOrg-2026 (empty if unknown)
	Area     string // PSU
	Number   string // 100
	Campus   string // TLS
	Year     int    // 1 -> tek1
	Semester int    // 1
	Slug     string // myls
}

// G-PSU-100-TLS-1-1-myls-1 ; the trailing "-1" is the repo instance number.
var repoRe = regexp.MustCompile(`^[A-Z]-([A-Z]{3})-(\d{3})-([A-Z]{2,4})-(\d)-(\d)-(.+?)(?:-\d+)?$`)

// Parse accepts a bare repo name, "owner/name", an SSH URL or an HTTPS URL.
func Parse(s string) (Repo, error) {
	owner, name := splitOwner(s)
	m := repoRe.FindStringSubmatch(name)
	if m == nil {
		return Repo{}, fmt.Errorf("%q doesn't look like an Epitech repo (expected G-XXX-000-CITY-1-1-name-N)", name)
	}
	return Repo{
		Name: name, Owner: owner,
		Area: m[1], Number: m[2], Campus: m[3],
		Year: int(m[4][0] - '0'), Semester: int(m[5][0] - '0'),
		Slug: m[6],
	}, nil
}

// splitOwner strips URL noise: git@github.com:Org/Name.git -> (Org, Name).
func splitOwner(s string) (string, string) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	s = strings.TrimPrefix(s, "git@github.com:")
	if i := strings.Index(s, "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
	}
	parts := strings.Split(s, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	return "", s
}

// Code is the short module code used in folder names: "psu100".
func (r Repo) Code() string { return strings.ToLower(r.Area) + r.Number }

// ModuleID is the official code: "G-PSU-100".
func (r Repo) ModuleID() string { return "G-" + r.Area + "-" + r.Number }

// Tek is the year folder: "tek1".
func (r Repo) Tek() string { return fmt.Sprintf("tek%d", r.Year) }

// IsPool reports whether this repo belongs to the C pool (G-CPE-100).
func (r Repo) IsPool() bool { return r.Area == "CPE" && r.Number == "100" }

// Topics maps known module areas to a readable folder suffix.
// Unknown areas are asked for once; after that the existing folder is reused.
var Topics = map[string]string{
	"CPE": "c_programming",
	"PSU": "unix",
	"MUL": "graphics",
}

// ---------------------------------------------------------------- naming

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Squash lowercases and drops everything but letters and digits:
// "MY_LS" -> "myls". Two names "mean the same" when their squashes match.
func Squash(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// Snake turns any label into lowercase snake_case: "Setting Up" -> "setting_up".
func Snake(s string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), "_"), "_")
}

var poolDay = regexp.MustCompile(`^cpoolday(\d+)$`)

// SuggestName picks the proper project name for a repo slug.
//
// Repo slugs lose their separators ("myls", "settingup"), but the subject PDF
// title keeps them ("MY_LS", "SETTING UP_"). So we try every run of
// consecutive words in each title, and keep the one whose letters match the
// slug exactly. That recovers "my_ls" and "setting_up" without a lookup table.
func SuggestName(slug string, titles []string) string {
	low := strings.ToLower(slug)
	if m := poolDay.FindStringSubmatch(low); m != nil {
		return "day" + m[1]
	}
	want := Squash(slug)
	for _, t := range titles {
		words := strings.Fields(nonAlnum.ReplaceAllString(strings.ToLower(t), " "))
		for i := range words {
			for j := i + 1; j <= len(words); j++ {
				cand := strings.Join(words[i:j], "_")
				if Squash(cand) == want {
					return cand
				}
			}
		}
	}
	// Fallback heuristic: Epitech loves "my_" prefixes.
	if strings.HasPrefix(low, "my") && len(low) > 2 && !strings.HasPrefix(low, "my_") {
		return "my_" + Snake(low[2:])
	}
	return Snake(slug)
}

var snakeRe = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// IsSnake reports whether a name follows the folder convention.
func IsSnake(s string) bool { return snakeRe.MatchString(s) }

// ---------------------------------------------------------------- subjects

// SubjectKind classifies a subject document from its filename and title text.
// "" is the main subject; others become a "-kind" suffix on the filename.
func SubjectKind(filename, text string) string {
	s := strings.ToLower(filename + " " + text)
	switch {
	case strings.Contains(s, "bootstrap"):
		return "bootstrap"
	case strings.Contains(s, "kick-off") || strings.Contains(s, "kickoff"):
		return "kickoff"
	case strings.Contains(s, "roadmap") || strings.Contains(s, "feuille de route") || strings.Contains(s, "feuille_de_route"):
		return "roadmap"
	}
	return ""
}

// SubjectFile is the canonical filename: my_ls.pdf, my_ls-bootstrap.pdf.
func SubjectFile(project, kind, ext string) string {
	if kind == "" {
		return project + ext
	}
	return project + "-" + kind + ext
}
