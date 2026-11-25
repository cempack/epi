package subjects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameFileAndReplace(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	_ = os.WriteFile(a, []byte("hello world"), 0o644)
	_ = os.WriteFile(b, []byte("hello world"), 0o644)
	_ = os.WriteFile(c, []byte("different"), 0o644)

	if !SameFile(a, b) {
		t.Fatalf("expected a and b to match")
	}
	if SameFile(a, c) {
		t.Fatalf("expected a and c to differ")
	}
	if err := Replace(c, b); err != nil {
		t.Fatalf("Replace failed: %v", err)
	}
	got, _ := os.ReadFile(b)
	if string(got) != "different" {
		t.Fatalf("unexpected replaced content: %q", string(got))
	}
	if _, err := os.Stat(c); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed after Replace")
	}
}

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(f, []byte("Authorized functions:\nopen, read, write\nreturn 84 on error\n"), 0o644)
	hits := Search([]SearchTarget{{Owner: "my_ls", Label: "notes.txt", Path: f}}, "return 84")
	if len(hits) != 1 || hits[0].Count != 1 {
		t.Fatalf("expected 1 hit, got %+v", hits)
	}
}
