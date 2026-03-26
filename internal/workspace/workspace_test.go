package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/git"
	"github.com/nicobailon/treemux/internal/shell"
	"github.com/nicobailon/treemux/internal/tmux"
)

type recordedCall struct {
	dir  string
	name string
	args []string
}

type recordingCommander struct {
	calls []recordedCall
}

func (r *recordingCommander) Run(name string, args ...string) ([]byte, error) {
	callArgs := append([]string(nil), args...)
	r.calls = append(r.calls, recordedCall{name: name, args: callArgs})
	return nil, nil
}

func (r *recordingCommander) RunDir(dir, name string, args ...string) ([]byte, error) {
	callArgs := append([]string(nil), args...)
	r.calls = append(r.calls, recordedCall{dir: dir, name: name, args: callArgs})
	return nil, nil
}

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

func TestCreateWorktreeUsesNameAsBranch(t *testing.T) {
	cmd := &recordingCommander{}
	g := &git.Git{RepoRoot: "/home/user/repo", Cmd: cmd}
	cfg := &config.Config{PathPattern: "sibling", SessionName: "folder"}
	s := NewService(g, &tmux.Tmux{Cmd: cmd}, cfg, cmd)

	worktreePath, err := s.CreateWorktree("feature-auth", "main")
	if err != nil {
		t.Fatalf("create worktree: %v", err)
	}

	wantPath := "/home/user/repo-feature-auth"
	if worktreePath != wantPath {
		t.Fatalf("worktree path mismatch: got %s want %s", worktreePath, wantPath)
	}
	if len(cmd.calls) != 2 {
		t.Fatalf("call count mismatch: got %d want 2", len(cmd.calls))
	}

	if cmd.calls[0].dir != "/home/user/repo" || cmd.calls[0].name != "git" {
		t.Fatalf("first call mismatch: %+v", cmd.calls[0])
	}
	wantGitArgs := []string{"worktree", "add", wantPath, "-b", "feature-auth", "main"}
	if !reflect.DeepEqual(cmd.calls[0].args, wantGitArgs) {
		t.Fatalf("git args mismatch: got %v want %v", cmd.calls[0].args, wantGitArgs)
	}

	if cmd.calls[1].name != "tmux" {
		t.Fatalf("second call command mismatch: %+v", cmd.calls[1])
	}
	wantTmuxArgs := []string{"new-session", "-d", "-s", "repo-feature-auth", "-c", wantPath}
	if !reflect.DeepEqual(cmd.calls[1].args, wantTmuxArgs) {
		t.Fatalf("tmux args mismatch: got %v want %v", cmd.calls[1].args, wantTmuxArgs)
	}
}

func TestCreateWorktreeFromBranchUsesExplicitBranch(t *testing.T) {
	cmd := &recordingCommander{}
	g := &git.Git{RepoRoot: "/home/user/repo", Cmd: cmd}
	cfg := &config.Config{PathPattern: "sibling", SessionName: "folder"}
	s := NewService(g, &tmux.Tmux{Cmd: cmd}, cfg, cmd)

	worktreePath, err := s.CreateWorktreeFromBranch("feature-auth", "ticket-123", "develop")
	if err != nil {
		t.Fatalf("create worktree from branch: %v", err)
	}

	wantPath := "/home/user/repo-feature-auth"
	if worktreePath != wantPath {
		t.Fatalf("worktree path mismatch: got %s want %s", worktreePath, wantPath)
	}
	if len(cmd.calls) != 2 {
		t.Fatalf("call count mismatch: got %d want 2", len(cmd.calls))
	}

	wantGitArgs := []string{"worktree", "add", wantPath, "-b", "ticket-123", "develop"}
	if !reflect.DeepEqual(cmd.calls[0].args, wantGitArgs) {
		t.Fatalf("git args mismatch: got %v want %v", cmd.calls[0].args, wantGitArgs)
	}

	wantTmuxArgs := []string{"new-session", "-d", "-s", "repo-feature-auth", "-c", wantPath}
	if !reflect.DeepEqual(cmd.calls[1].args, wantTmuxArgs) {
		t.Fatalf("tmux args mismatch: got %v want %v", cmd.calls[1].args, wantTmuxArgs)
	}
}

func execCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	return cmd
}
