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
	}
	for in, want := range cases {
		if got := SubjectKind(in[0], in[1]); got != want {
			t.Errorf("SubjectKind(%q) = %q, want %q", in, got, want)
		}
	}
}
