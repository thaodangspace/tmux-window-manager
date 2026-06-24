package dirs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// ~/code: depth and hidden behavior, plus an excluded node_modules.
	codeA := mk("code", "projA")
	codeNested := mk("code", "projA", "sub", "deep") // depth 3 under code
	mk("code", "projA", "sub", "deep", "toodeep")    // depth 4 -> excluded
	mk("code", "projA", "node_modules", "pkg")       // excluded
	codeHidden := mk("code", ".dotproj")             // hidden included under code
	// ~/go
	goP := mk("go", "p")
	// $HOME top level: a normal dir (included), a hidden dir (excluded), Music (excluded)
	projHome := mk("project-home")
	mk(".config")
	mk("Music")

	got := List("/some/current")

	mustContain := []string{"/some/current", home, codeA, codeNested, codeHidden, goP, projHome}
	for _, want := range mustContain {
		if !slices.Contains(got, want) {
			t.Errorf("List missing %q\ngot: %v", want, got)
		}
	}

	mustNotContain := []string{
		filepath.Join(home, "code", "projA", "sub", "deep", "toodeep"),
		filepath.Join(home, "code", "projA", "node_modules"),
		filepath.Join(home, "code", "projA", "node_modules", "pkg"),
		filepath.Join(home, ".config"),
		filepath.Join(home, "Music"),
	}
	for _, bad := range mustNotContain {
		if slices.Contains(got, bad) {
			t.Errorf("List should not contain %q", bad)
		}
	}

	// Order: current dir first, then home.
	if got[0] != "/some/current" || got[1] != home {
		t.Errorf("expected current then home first, got %v", got[:2])
	}

	// Dedup: no path appears twice.
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("duplicate path %q", p)
		}
		seen[p] = true
	}
}

func TestListNoCurrentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := List("")
	if len(got) == 0 || got[0] != home {
		t.Errorf("with no current dir, home should be first; got %v", got)
	}
}
