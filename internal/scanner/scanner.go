package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RepoWorktree struct {
	RepoName string
	RepoRoot string
	Worktree Worktree
}

type Worktree struct {
	Name   string
	Path   string
	Branch string
}

func ScanForRepos(searchPaths []string) []string {
	var repos []string
	seen := make(map[string]bool)

	for _, searchPath := range searchPaths {
		expanded := os.ExpandEnv(searchPath)
		if strings.HasPrefix(expanded, "~/") {
			home, _ := os.UserHomeDir()
			expanded = filepath.Join(home, expanded[2:])
		}

		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dirPath := filepath.Join(expanded, entry.Name())
			gitPath := filepath.Join(dirPath, ".git")

			if info, err := os.Stat(gitPath); err == nil {
				if info.IsDir() {
					repoRoot := findRepoRoot(dirPath)
					if repoRoot != "" && !seen[repoRoot] {
						repos = append(repos, repoRoot)
						seen[repoRoot] = true
					}
				}
			}
		}
	}

	return repos
}

func findRepoRoot(path string) string {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func GetWorktreesForRepo(repoRoot string) []Worktree {
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
			current.Name = filepath.Base(current.Path)
		} else if strings.HasPrefix(line, "branch ") {
			branch := strings.TrimPrefix(line, "branch refs/heads/")
			current.Branch = branch
		}
	}
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

func ScanAll(searchPaths []string) []RepoWorktree {
	var all []RepoWorktree

	repos := ScanForRepos(searchPaths)
	for _, repoRoot := range repos {
		repoName := filepath.Base(repoRoot)
		worktrees := GetWorktreesForRepo(repoRoot)
		for _, wt := range worktrees {
			all = append(all, RepoWorktree{
				RepoName: repoName,
				RepoRoot: repoRoot,
				Worktree: wt,
			})
		}
	}

	return all
}
