package epitech

import "testing"

func TestParse(t *testing.T) {
	r, err := Parse("git@github.com:EpitechOrg-2026/G-PSU-100-TLS-1-1-myls-1.git")
	if err != nil {
		t.Fatal(err)
	}
	if r.Owner != "EpitechOrg-2026" || r.Code() != "psu100" || r.Tek() != "tek1" || r.Slug != "myls" {
		t.Fatalf("bad parse: %+v", r)
	}
	if _, err := Parse("https://github.com/x/G-MUL-100-TLS-1-1-myradar-17"); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse("dotfiles"); err == nil {
		t.Fatal("expected error for non-Epitech repo")
	}
}

// Real slugs and real PDF title lines from the subjects.
func TestSuggestName(t *testing.T) {
	cases := []struct {
		slug   string
		titles []string
		want   string
	}{
		{"myls", []string{"BOOTSTRAP_", "< MY_LS />"}, "my_ls"},
		{"settingup", []string{"BOOTSTRAP - SETTING UP_", "< ELEMENTARY PROGRAMMING />"}, "setting_up"},
		{"myhunter", []string{"BOOTSTRAP MY HUNTER_", "< INTRODUCTION TO EVENTS AND"}, "my_hunter"},
		{"miniprintf", []string{"MINI_PRINTF_", "< A SIMPLER VERSION OF PRINTF />"}, "mini_printf"},
		{"countisland", []string{"COUNT_ISLAND_", "< BACK TO RECURSION />"}, "count_island"},
		{"cpoolday07", nil, "day07"},
		{"myradar", nil, "my_radar"},    // no PDF: heuristic
		{"organized", nil, "organized"}, // plain word stays as-is
		{"Cworkshoplib", nil, "cworkshoplib"},
	}
	for _, c := range cases {
		if got := SuggestName(c.slug, c.titles); got != c.want {
			t.Errorf("SuggestName(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

func TestSubjectKind(t *testing.T) {
	cases := map[[2]string]string{
		{"Bootstrap_my_radar.pdf", "BOOTSTRAP MY RADAR"}:    "bootstrap",
		{"my_sudo.pdf", "MY_SUDO_ <Kick-off>"}:              "kickoff",
		{"my_printf_feuille_de_route.pdf", ""}:              "roadmap",
		{"my_ls.pdf", "MY_LS_ < LIST DIRECTORY CONTENT />"}: "",
		{"annexe_protocol.pdf", ""}:                         "annex",
		{"project_bonus.pdf", "BONUS FEATURES"}:             "bonus",
		{"presentation_slides.pdf", ""}:                     "slides",
		{"star-examples.txt", ""}:                           "examples",
	}
	for in, want := range cases {
		if got := SubjectKind(in[0], in[1]); got != want {
			t.Errorf("SubjectKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSubjectKindFromFileAndOrder(t *testing.T) {
	cases := []struct {
		proj, file, want string
	}{
		{"my_sudo", "my_sudo.pdf", ""},
		{"my_sudo", "my_sudo-bootstrap.pdf", "bootstrap"},
		{"my_sudo", "my_sudo-bootstrap-2.pdf", "bootstrap"},
		{"my_sudo", "my_sudo-kickoff.pdf", "kickoff"},
		{"my_printf", "my_printf-roadmap.pdf", "roadmap"},
		{"star", "star-examples.txt", "examples"},
		{"my_ls", "my_ls-custom.pdf", "custom"},
	}
	for _, c := range cases {
		if got := SubjectKindFromFile(c.proj, c.file); got != c.want {
			t.Errorf("SubjectKindFromFile(%q, %q) = %q, want %q", c.proj, c.file, got, c.want)
		}
	}
	if !(SubjectOrder("") < SubjectOrder("bootstrap") && SubjectOrder("bootstrap") < SubjectOrder("kickoff") && SubjectOrder("kickoff") < SubjectOrder("roadmap")) {
		t.Fatalf("unexpected subject order")
	}
}

func TestParseAuthorized(t *testing.T) {
	inline := "binary name: setting_up\nAuthorized functions: open, read, write, close, malloc, free, stat\n"
	got := ParseAuthorized(inline)
	if len(got.Allowed) != 7 || got.Allowed[0] != "open" || got.Allowed[6] != "stat" {
		t.Fatalf("inline parse failed: %+v", got)
	}

	bulleted := "Authorized Functions\nThe only system calls and libc function allowed are the following ones:\n3 opendir, readdir, closedir, write,\n3 malloc, free\n"
	got = ParseAuthorized(bulleted)
	if len(got.Allowed) != 6 || got.Allowed[0] != "opendir" || got.Allowed[5] != "free" {
		t.Fatalf("bulleted parse failed: %+v", got)
	}

	csfml := "Authorized functions\nHere is the full list of authorized functions.\nfrom the C library\n3 malloc\n3 free\nfrom the CSFML library\nAll functions\nAny unspecified functions are de facto banned\n"
	got = ParseAuthorized(csfml)
	if len(got.Allowed) != 3 || got.Allowed[2] != "sf*" {
		t.Fatalf("csfml parse failed: %+v", got)
	}

	sudo := "Authorized functions: All the libC minus forbidden functions.\nForbidden functions: getpw*, *fork*, system\n"
	got = ParseAuthorized(sudo)
	if len(got.Forbidden) != 3 || got.Forbidden[2] != "system" || got.Note == "" {
		t.Fatalf("forbidden parse failed: %+v", got)
	}
}
