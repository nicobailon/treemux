package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/git"
	"github.com/nicobailon/treemux/internal/shell"
	"github.com/nicobailon/treemux/internal/tmux"
)

func TestWorktreePathPatterns(t *testing.T) {
	cmd := &shell.ExecCommander{}
	g := &git.Git{RepoRoot: "/home/user/repo", Cmd: cmd}
	cfg := &config.Config{PathPattern: "sibling"}
	s := NewService(g, &tmux.Tmux{Cmd: cmd}, cfg, cmd)

	if got := s.WorktreePath("feature"); got != "/home/user/repo-feature" {
		t.Fatalf("sibling pattern mismatch: %s", got)
	}

	cfg.PathPattern = "subdirectory"
	s.Config = cfg
	if got := s.WorktreePath("feature"); got != "/home/user/repo/.worktrees/feature" {
		t.Fatalf("subdirectory pattern mismatch: %s", got)
	}
}

func TestSessionNameFolder(t *testing.T) {
	dir := t.TempDir()
	cmd := &shell.ExecCommander{}
	g := &git.Git{RepoRoot: dir, Cmd: cmd}
	cfg := &config.Config{SessionName: "folder"}
	s := NewService(g, &tmux.Tmux{Cmd: cmd}, cfg, cmd)
	path := filepath.Join(dir, "wt")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := s.SessionName(path); got != "wt" {
		t.Fatalf("folder session name mismatch: %s", got)
	}
}

func TestSessionNameBranch(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := execCommand(dir, args...)
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("git", "init", "-q")
	run("git", "commit", "--allow-empty", "-m", "init")
	run("git", "checkout", "-b", "feature/test")

	cmd := &shell.ExecCommander{}
	g := &git.Git{RepoRoot: dir, Cmd: cmd}
	cfg := &config.Config{SessionName: "branch"}
	s := NewService(g, &tmux.Tmux{Cmd: cmd}, cfg, cmd)
	if got := s.SessionName(dir); got != "feature/test" {
		t.Fatalf("branch session name mismatch: %s", got)
	}
}

func execCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	return cmd
}
