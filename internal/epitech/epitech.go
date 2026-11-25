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
	case strings.Contains(s, "annexe") || strings.Contains(s, "annex") || strings.Contains(s, "appendix"):
		return "annex"
	case strings.Contains(s, "bonus"):
		return "bonus"
	case strings.Contains(s, "slides") || strings.Contains(s, "presentation") || strings.Contains(s, "présentation"):
		return "slides"
	case strings.Contains(s, "example") || strings.Contains(s, "exemple"):
		return "examples"
	}
	return ""
}

// KnownKinds lists the standard subject suffixes in display order.
var KnownKinds = []string{"", "bootstrap", "kickoff", "roadmap", "annex", "bonus", "slides", "examples"}

var trailingNumRe = regexp.MustCompile(`-(?:v?\d+)$`)

// SubjectKindFromFile infers the kind of an already-filed subject given its
// project name and base filename ("my_sudo-bootstrap.pdf" -> "bootstrap",
// "my_sudo.pdf" -> "", "my_ls-bootstrap-2.pdf" -> "bootstrap").
func SubjectKindFromFile(project, filename string) string {
	base := filename
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	if base == project {
		return ""
	}
	if strings.HasPrefix(base, project+"-") {
		suf := strings.TrimPrefix(base, project+"-")
		suf = trailingNumRe.ReplaceAllString(suf, "")
		return suf
	}
	return SubjectKind(filename, "")
}

// SubjectOrder returns a sort rank so subjects always appear in a predictable,
// useful order: main first, then bootstrap, kickoff, roadmap, extras.
func SubjectOrder(kind string) int {
	for i, k := range KnownKinds {
		if k == kind {
			return i
		}
	}
	return len(KnownKinds)
}

// SubjectLabel is the human-readable badge for a subject kind.
func SubjectLabel(kind string) string {
	if kind == "" {
		return "main"
	}
	return kind
}

// SubjectFile is the canonical filename: my_ls.pdf, my_ls-bootstrap.pdf.
func SubjectFile(project, kind, ext string) string {
	if kind == "" {
		return project + ext
	}
	return project + "-" + kind + ext
}

// AuthorizedInfo holds authorized/forbidden functions parsed from a subject PDF.
type AuthorizedInfo struct {
	Allowed   []string // explicit function names/globs, e.g. "malloc", "sf*"
	Forbidden []string // explicit forbidden functions when "all libc minus..."
	Note      string   // summary note when a whole library is allowed
}

var (
	identRe     = regexp.MustCompile(`^[A-Za-z_*][A-Za-z0-9_*]*(?:\([0-9a-z]+\))?$`)
	bulletRe    = regexp.MustCompile(`^(?:[•◦▪▫–—\-*]|3)\s+`)
	manSuffixRe = regexp.MustCompile(`\([0-9a-z]+\)$`)
)

// ParseAuthorized extracts authorized/forbidden function rules from the full
// pdftotext output of an Epitech subject PDF.
func ParseAuthorized(text string) AuthorizedInfo {
	lines := strings.Split(text, "\n")
	var info AuthorizedInfo
	var notes []string
	seenAllowed := map[string]bool{}
	seenForbidden := map[string]bool{}

	addFn := func(dst *[]string, seen map[string]bool, raw string) {
		raw = strings.TrimSpace(raw)
		raw = strings.TrimRight(raw, ".,;:")
		raw = manSuffixRe.ReplaceAllString(raw, "")
		if raw == "" || !identRe.MatchString(raw) {
			return
		}
		// Reject plain English words that show up in sentences.
		switch strings.ToLower(raw) {
		case "all", "the", "only", "and", "from", "none", "any", "libc", "library",
			"functions", "function", "minus", "forbidden", "authorized", "allowed",
			"system", "calls", "following", "ones", "are", "is", "of", "in", "to",
			"for", "with", "math", "csfml", "here", "full", "list", "unspecified",
			"de", "facto", "banned":
			// Keep "system" only when explicitly listed in a comma list or forbidden list;
			// handled by caller context below.
			if strings.ToLower(raw) != "system" {
				return
			}
		}
		if !seen[raw] {
			seen[raw] = true
			*dst = append(*dst, raw)
		}
	}

	parseList := func(dst *[]string, seen map[string]bool, s string) int {
		before := len(*dst)
		for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t'
		}) {
			addFn(dst, seen, tok)
		}
		return len(*dst) - before
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		low := strings.ToLower(line)

		// Inline "Forbidden functions: ..."
		if strings.HasPrefix(low, "forbidden functions:") || strings.HasPrefix(low, "banned functions:") {
			rest := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			parseList(&info.Forbidden, seenForbidden, rest)
			// Continuation lines until blank or next heading.
			for j := i + 1; j < len(lines) && j < i+6; j++ {
				nxt := strings.TrimSpace(lines[j])
				if nxt == "" || strings.Contains(nxt, ":") || strings.HasPrefix(nxt, "∇") || strings.HasPrefix(nxt, "$>") {
					break
				}
				if parseList(&info.Forbidden, seenForbidden, nxt) == 0 {
					break
				}
			}
			continue
		}

		// Inline "Authorized functions: ..." or "Allowed functions: ..."
		if strings.HasPrefix(low, "authorized functions:") || strings.HasPrefix(low, "allowed functions:") {
			rest := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			lowRest := strings.ToLower(rest)
			if strings.Contains(lowRest, "all") && (strings.Contains(lowRest, "libc") || strings.Contains(lowRest, "minus") || strings.Contains(lowRest, "function")) {
				notes = append(notes, rest)
				continue
			}
			parseList(&info.Allowed, seenAllowed, rest)
			for j := i + 1; j < len(lines) && j < i+6; j++ {
				nxt := strings.TrimSpace(lines[j])
				if nxt == "" || strings.Contains(nxt, ":") {
					break
				}
				if parseList(&info.Allowed, seenAllowed, nxt) == 0 {
					break
				}
			}
			continue
		}

		// Section heading "Authorized Functions" / "Allowed Functions"
		if low == "authorized functions" || low == "allowed functions" {
			curLib := ""
			for j := i + 1; j < len(lines) && j < i+45; j++ {
				nxt := strings.TrimSpace(lines[j])
				lowNxt := strings.ToLower(nxt)
				if nxt == "" || nxt == "\f" {
					continue
				}
				if strings.HasPrefix(lowNxt, "any unspecified") || strings.HasPrefix(lowNxt, "usage") ||
					strings.HasPrefix(lowNxt, "examples") || strings.HasPrefix(lowNxt, "bonus") ||
					strings.HasPrefix(lowNxt, "v ") || regexp.MustCompile(`^\d+$`).MatchString(nxt) {
					if len(info.Allowed) > 0 || len(notes) > 0 {
						break
					}
					continue
				}
				if strings.HasPrefix(lowNxt, "from the ") {
					curLib = strings.TrimSpace(strings.TrimPrefix(lowNxt, "from the "))
					continue
				}
				if strings.HasPrefix(lowNxt, "all functions") {
					if strings.Contains(curLib, "csfml") {
						addFn(&info.Allowed, seenAllowed, "sf*")
						notes = append(notes, "all CSFML (sf*)")
					} else if curLib != "" {
						notes = append(notes, "all "+curLib)
					} else {
						notes = append(notes, "all functions")
					}
					continue
				}
				if bulletRe.MatchString(nxt) {
					body := bulletRe.ReplaceAllString(nxt, "")
					parseList(&info.Allowed, seenAllowed, body)
					continue
				}
				// Introductory sentence right after heading.
				if strings.Contains(lowNxt, "following") || strings.Contains(lowNxt, "full list") || strings.Contains(lowNxt, "allowed") {
					continue
				}
				if len(info.Allowed) > 0 || len(notes) > 0 {
					break
				}
			}
		}
	}
	info.Note = strings.Join(notes, " · ")
	return info
}
