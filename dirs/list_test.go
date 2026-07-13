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
	mkGitDir := func(p string) string {
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mkGitFile := func(p string) string {
		if err := os.WriteFile(filepath.Join(p, ".git"), []byte("gitdir: ../.git/worktrees/"+filepath.Base(p)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	current := mkGitDir(mk("current-repo"))
	mkGitDir(home)

	// ~/code: depth and hidden behavior, plus an excluded node_modules.
	codeA := mkGitDir(mk("code", "projA"))
	codeNested := mkGitFile(mk("code", "projA", "sub", "deep")) // depth 3 under code; .git file like a worktree
	mkGitDir(mk("code", "projA", "sub", "deep", "toodeep"))     // depth 4 -> excluded
	mkGitDir(mk("code", "projA", "node_modules", "pkg"))        // excluded
	codeHidden := mkGitDir(mk("code", ".dotproj"))              // hidden included under code
	codeNonGit := mk("code", "not-a-repo")
	// ~/go
	goP := mkGitDir(mk("go", "p"))
	// $HOME top level: a normal Git dir (included), a non-Git dir (excluded), a hidden Git dir (excluded), Music (excluded)
	projHome := mkGitDir(mk("project-home"))
	nonGitHome := mk("not-a-repo-home")
	mkGitDir(mk(".config"))
	mkGitDir(mk("Music"))

	got := List(current)

	mustContain := []string{current, home, codeA, codeNested, codeHidden, goP, projHome}
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
		codeNonGit,
		nonGitHome,
	}
	for _, bad := range mustNotContain {
		if slices.Contains(got, bad) {
			t.Errorf("List should not contain %q", bad)
		}
	}

	// Order: current dir first, then home.
	if got[0] != current || got[1] != home {
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
	if err := os.Mkdir(filepath.Join(home, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := List("")
	if len(got) == 0 || got[0] != home {
		t.Errorf("with no current dir, Git home should be first; got %v", got)
	}
}

func TestListOmitsCurrentDirWhenNotGitRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	current := filepath.Join(home, "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}

	got := List(current)
	if slices.Contains(got, current) {
		t.Errorf("non-Git current dir should be excluded; got %v", got)
	}
}
