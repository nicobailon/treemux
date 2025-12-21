package git

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Git struct {
	RepoRoot string
}

func New() (*Git, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	return &Git{RepoRoot: root}, nil
}

func repoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("not in a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.RepoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

func (g *Git) DefaultBranch() string {
	out, err := g.run("symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "main"
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "refs/remotes/origin/")
}

func (g *Git) Branches() ([]string, error) {
	out, err := g.run("branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

func (g *Git) BranchExists(name string) bool {
	_, err := g.run("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

type Worktree struct {
	Path   string
	Name   string
	Branch string
}

func (g *Git) WorktreeList() ([]Worktree, error) {
	out, err := g.run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []Worktree
	var current Worktree
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				wts = append(wts, current)
			}
			current = Worktree{Path: strings.TrimSpace(strings.TrimPrefix(line, "worktree "))}
			current.Name = filepath.Base(current.Path)
		} else if strings.HasPrefix(line, "branch ") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		}
	}
	if current.Path != "" {
		wts = append(wts, current)
	}
	return wts, nil
}

func (g *Git) WorktreeAdd(path, branch, base string) error {
	if base == "" {
		base = g.DefaultBranch()
	}
	_, err := g.run("worktree", "add", path, "-b", branch, base)
	if err == nil {
		return nil
	}
	// if branch exists, try without -b
	if g.BranchExists(branch) {
		_, err = g.run("worktree", "add", path, branch)
	}
	return err
}

func (g *Git) WorktreeRemove(path string, force bool) error {
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	_, err := g.run(args...)
	return err
}

type StatusSummary struct {
	Staged    int
	Modified  int
	Untracked int
	Clean     bool
}

func (g *Git) Status(path string) (*StatusSummary, error) {
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	summary := &StatusSummary{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "??"):
			summary.Untracked++
		case strings.HasPrefix(line, " M") || strings.HasPrefix(line, "MM") || strings.HasPrefix(line, "AM"):
			summary.Modified++
		default:
			summary.Staged++
		}
	}
	summary.Clean = summary.Staged == 0 && summary.Modified == 0 && summary.Untracked == 0
	return summary, nil
}

type Commit struct {
	Hash string
	Msg  string
}

func (g *Git) Log(path string, n int) ([]Commit, error) {
	cmd := exec.Command("git", "log", "--oneline", "-n", strconv.Itoa(n))
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			commits = append(commits, Commit{Hash: parts[0], Msg: parts[1]})
		}
	}
	return commits, nil
}
