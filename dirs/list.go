// Package dirs lists Git repository directory candidates for the "new session"
// picker, natively replacing the script's `fd` invocations so the only external
// runtime dependencies remain tmux and fzf.
package dirs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// List returns Git repository directory candidates, in the same order and with
// the same scope as the script's list_directories: the current dir, $HOME, a
// deep walk of ~/code and ~/go, then the top-level children of $HOME. A
// candidate is emitted only when it has a direct .git entry. Duplicates are
// removed, keeping first occurrence (the awk '!seen[$0]++').
func List(currentDir string) []string {
	home, _ := os.UserHomeDir()

	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] || !isGitRepo(p) {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	if currentDir != "" {
		add(currentDir)
	}
	add(home)

	// ~/code and ~/go: directories up to 3 levels deep, hidden included
	// (the script's fd calls passed --hidden here).
	deepExclude := set(".git", "node_modules", "Library", ".Trash")
	for _, sub := range []string{"code", "go"} {
		root := filepath.Join(home, sub)
		if isDir(root) {
			for _, d := range walkDirs(root, 3, deepExclude, true) {
				add(d)
			}
		}
	}

	// $HOME top-level (depth 1), excluding noisy/system dirs and the two we
	// already walked deeply. The script's fd here did NOT pass --hidden, so
	// dotfile dirs (~/.config, ~/.ssh, …) are skipped.
	homeExclude := set(".git", "Library", ".Trash", "Applications", "Public",
		"OrbStack", "Movies", "Music", "Pictures", "code", "go")
	for _, d := range walkDirs(home, 1, homeExclude, false) {
		add(d)
	}

	return out
}

// walkDirs returns directories under root from depth 1 to maxDepth (inclusive),
// excluding any whose basename is in exclude (and not descending into them).
// The root itself is not emitted, matching fd. When includeHidden is false,
// dot-directories are skipped.
//
// Note: unlike fd, this does not honor .gitignore — a deliberate simplification
// (replicating gitignore semantics natively isn't worth the complexity here).
// The explicit excludes cover the dirs that matter (.git, node_modules, …).
func walkDirs(root string, maxDepth int, exclude map[string]bool, includeHidden bool) []string {
	var dirs []string
	sep := string(os.PathSeparator)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if path == root || !d.IsDir() {
			return nil
		}
		if exclude[d.Name()] || (!includeHidden && strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		depth := strings.Count(strings.TrimPrefix(path, root+sep), sep) + 1
		if depth > maxDepth {
			return filepath.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isGitRepo(p string) bool {
	_, err := os.Stat(filepath.Join(p, ".git"))
	return err == nil
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}
