package workspace

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/git"
	"github.com/nicobailon/treemux/internal/tmux"
)

type Service struct {
	Git    *git.Git
	Tmux   *tmux.Tmux
	Config *config.Config
}

type WorktreeState struct {
	Worktree    git.Worktree
	SessionName string
	HasSession  bool
	Status      *git.StatusSummary
	Ahead       int
	Behind      int
	Commits     []git.Commit
	SessionInfo *tmux.SessionInfo
	Processes   []string
}

func NewService(g *git.Git, t *tmux.Tmux, cfg *config.Config) *Service {
	return &Service{Git: g, Tmux: t, Config: cfg}
}

func (s *Service) WorktreePath(name string) string {
	repo := s.Git.RepoRoot
	parent := filepath.Dir(repo)
	repoName := filepath.Base(repo)
	switch s.Config.PathPattern {
	case "subdirectory":
		return filepath.Join(repo, ".worktrees", name)
	default:
		return filepath.Join(parent, repoName+"-"+name)
	}
}

func (s *Service) SessionName(wtPath string) string {
	switch s.Config.SessionName {
	case "branch":
		cmd := exec.Command("git", "-C", wtPath, "branch", "--show-current")
		out, err := cmd.Output()
		if err == nil {
			branch := strings.TrimSpace(string(out))
			if branch != "" {
				return branch
			}
		}
	}
	return filepath.Base(wtPath)
}

func (s *Service) List() ([]WorktreeState, []string, error) {
	worktrees, err := s.Git.WorktreeList()
	if err != nil {
		return nil, nil, err
	}

	sessions, _ := s.Tmux.ListSessions()
	sessionSet := map[string]struct{}{}
	for _, sess := range sessions {
		sessionSet[sess.Name] = struct{}{}
	}

	var states []WorktreeState
	for _, wt := range worktrees {
		sessionName := s.SessionName(wt.Path)
		_, has := sessionSet[sessionName]
		status, _ := s.Git.Status(wt.Path)
		ahead, behind := aheadBehind(wt.Path)
		commits, _ := s.Git.Log(wt.Path, 6)
		info, _ := s.Tmux.SessionInfo(sessionName)
		procs, _ := s.Tmux.RunningProcesses(sessionName)
		states = append(states, WorktreeState{
			Worktree:    wt,
			SessionName: sessionName,
			HasSession:  has,
			Status:      status,
			Ahead:       ahead,
			Behind:      behind,
			Commits:     commits,
			SessionInfo: info,
			Processes:   procs,
		})
	}

	orphans := []string{}
	worktreeNames := map[string]struct{}{}
	for _, wt := range worktrees {
		worktreeNames[wt.Name] = struct{}{}
	}
	for name := range sessionSet {
		if _, ok := worktreeNames[name]; !ok {
			orphans = append(orphans, name)
		}
	}

	return states, orphans, nil
}

func (s *Service) WorktreesWithoutSession(states []WorktreeState) []WorktreeState {
	missing := []WorktreeState{}
	for _, st := range states {
		if !st.HasSession {
			missing = append(missing, st)
		}
	}
	return missing
}

func (s *Service) CreateWorktree(name, baseBranch string) (string, error) {
	path := s.WorktreePath(name)
	if err := s.Git.WorktreeAdd(path, name, baseBranch); err != nil {
		return "", err
	}
	sessionName := s.SessionName(path)
	_ = s.Tmux.NewSession(sessionName, path)
	return path, nil
}

func (s *Service) DeleteWorktree(path string, force bool) error {
	sessionName := s.SessionName(path)
	_ = s.Tmux.KillSession(sessionName)
	return s.Git.WorktreeRemove(path, force)
}

func (s *Service) KillSession(name string) error {
	if !s.Tmux.HasSession(name) {
		return errors.New("session not found")
	}
	return s.Tmux.KillSession(name)
}

func (s *Service) Jump(name, path string) error {
	sessionName := s.SessionName(path)
	if !s.Tmux.HasSession(sessionName) {
		if err := s.Tmux.NewSession(sessionName, path); err != nil {
			return err
		}
	}
	return s.Tmux.SwitchClient(sessionName)
}

func (s *Service) SwitchSession(name string) error {
	return s.Tmux.SwitchClient(name)
}

func (s *Service) AdoptOrphan(sessionName, baseBranch string) (string, error) {
	path := s.WorktreePath(sessionName)
	if err := s.Git.WorktreeAdd(path, sessionName, baseBranch); err != nil {
		return "", err
	}
	// retarget session to new path
	_ = exec.Command("tmux", "send-keys", "-t", sessionName, "cd '"+path+"'", "Enter").Run()
	return path, nil
}

func aheadBehind(path string) (int, int) {
	ahead := 0
	behind := 0
	cmd := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err := cmd.Run(); err != nil {
		return ahead, behind
	}
	if out, err := exec.Command("git", "-C", path, "rev-list", "--count", "@{upstream}..HEAD").Output(); err == nil {
		val, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		ahead = val
	}
	if out, err := exec.Command("git", "-C", path, "rev-list", "--count", "HEAD..@{upstream}").Output(); err == nil {
		val, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		behind = val
	}
	return ahead, behind
}
