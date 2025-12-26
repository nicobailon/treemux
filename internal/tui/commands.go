package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/recent"
	"github.com/nicobailon/treemux/internal/scanner"
	"github.com/nicobailon/treemux/internal/tmux"
	"github.com/nicobailon/treemux/internal/tui/views"
	"github.com/nicobailon/treemux/internal/workspace"
)

type jumpMsg struct {
	sessionName string
	path        string
	repoRoot    string
	worktree    string
}

func loadPaneContentCmd(t *tmux.Tmux, sessionName string, lines int) tea.Cmd {
	return func() tea.Msg {
		content, err := t.CapturePane(sessionName, lines)
		return paneContentMsg{sessionName: sessionName, content: content, err: err}
	}
}

func loadDataCmd(svc *workspace.Service) tea.Cmd {
	return func() tea.Msg {
		states, orphans, err := svc.List()
		if err != nil {
			return resultMsg{action: "load", err: err}
		}
		return dataLoadedMsg{states: states, orphans: orphans}
	}
}

func loadGlobalDataCmd(cfg *config.Config, t *tmux.Tmux) tea.Cmd {
	return func() tea.Msg {
		worktrees := scanner.ScanAll(cfg.SearchPaths)
		sessions, _ := t.ListSessions()
		sessionSet := make(map[string]bool)
		for _, s := range sessions {
			sessionSet[s.Name] = true
		}
		wtNames := make(map[string]bool)
		for _, wt := range worktrees {
			wtNames[wt.Worktree.Name] = true
		}
		var orphans []string
		for name := range sessionSet {
			if !wtNames[name] {
				orphans = append(orphans, name)
			}
		}
		return globalDataLoadedMsg{worktrees: worktrees, orphans: orphans}
	}
}

func branchesCmd(svc *workspace.Service) tea.Cmd {
	return func() tea.Msg {
		branches, err := svc.Git.Branches()
		if err != nil {
			return resultMsg{action: "branches", err: err}
		}
		def := svc.Git.DefaultBranch()
		sort.Strings(branches)
		if def != "" {
			branches = append([]string{def}, views.FilterStrings(branches, def)...)
		}
		return branchesMsg{branches: branches}
	}
}

func createWorktreeCmd(svc *workspace.Service, name, branch string) tea.Cmd {
	return func() tea.Msg {
		_, err := svc.CreateWorktree(name, branch)
		return resultMsg{action: "create", err: err}
	}
}

func deleteWorktreeCmd(svc *workspace.Service, path string) tea.Cmd {
	return func() tea.Msg {
		err := svc.DeleteWorktree(path, true)
		return resultMsg{action: "delete", err: err}
	}
}

func killSessionCmd(svc *workspace.Service, name string) tea.Cmd {
	return func() tea.Msg {
		err := svc.KillSession(name)
		return resultMsg{action: "kill-session", err: err}
	}
}

func killSessionDirectCmd(t *tmux.Tmux, name string) tea.Cmd {
	return func() tea.Msg {
		err := t.KillSession(name)
		return resultMsg{action: "kill-session", err: err}
	}
}

func jumpCmd(svc *workspace.Service, name, path string, store *recent.Store) tea.Cmd {
	return func() tea.Msg {
		sessionName := svc.SessionName(path)
		if !svc.Tmux.HasSession(sessionName) {
			if err := svc.Tmux.NewSession(sessionName, path); err != nil {
				return resultMsg{action: "jump", err: err}
			}
		}
		if store != nil {
			store.Add(svc.Git.RepoRoot, name, sessionName, path)
			_ = store.Save()
		}
		return jumpMsg{sessionName: sessionName, path: path, repoRoot: svc.Git.RepoRoot, worktree: name}
	}
}

func switchRecentCmd(svc *workspace.Service, entry recent.Entry, store *recent.Store) tea.Cmd {
	return func() tea.Msg {
		if store != nil {
			store.Add(entry.RepoRoot, entry.Worktree, entry.SessionName, entry.Path)
			_ = store.Save()
		}
		return jumpMsg{sessionName: entry.SessionName, path: entry.Path, repoRoot: entry.RepoRoot, worktree: entry.Worktree}
	}
}

func switchSessionCmd(svc *workspace.Service, name string) tea.Cmd {
	return func() tea.Msg {
		return jumpMsg{sessionName: name}
	}
}

func adoptCmd(svc *workspace.Service, name, branch string) tea.Cmd {
	return func() tea.Msg {
		_, err := svc.AdoptOrphan(name, branch)
		return resultMsg{action: "adopt", err: err}
	}
}
