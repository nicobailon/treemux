package tui

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicobailon/treemux/internal/git"
	"github.com/nicobailon/treemux/internal/shell"
	"github.com/nicobailon/treemux/internal/workspace"
)

func handleSelectRepo(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			idx := m.menu.Index()
			if idx >= 0 && idx < len(m.data.AvailableRepos) {
				repo := m.data.AvailableRepos[idx]
				var cmd shell.Commander = m.deps.Tmux.Cmd
				if cmd == nil {
					cmd = &shell.ExecCommander{}
				}
				g := &git.Git{RepoRoot: repo.Root, Cmd: cmd}
				m.pending.CreateSvc = workspace.NewService(g, m.deps.Tmux, m.deps.Cfg, cmd)
				m.nav.State = stateCreateName
				m.input.SetValue("")
				return *m, m.input.Focus()
			}
		case "esc":
			m.nav.State = stateMain
			m.pending.CreateSvc = nil
		}
	}
	return *m, cmd
}

func handleCreateName(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			name := strings.TrimSpace(m.input.Value())
			if name == "" {
				return *m, nil
			}
			m.pending.Name = name
			m.nav.NextBranchState = stateCreateBranch
			svc := m.deps.Svc
			if m.pending.CreateSvc != nil {
				svc = m.pending.CreateSvc
			}
			if svc == nil {
				m.nav.State = stateMain
				return *m, nil
			}
			return *m, branchesCmd(svc)
		case "esc":
			m.nav.State = stateMain
			m.pending.CreateSvc = nil
			return *m, nil
		}
	}
	return *m, cmd
}

func handleCreateBranch(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if sel, ok := m.menu.SelectedItem().(listItem); ok {
				branch := sel.ItemTitle
				name := m.pending.Name
				svc := m.deps.Svc
				if m.pending.CreateSvc != nil {
					svc = m.pending.CreateSvc
				}
				if svc == nil {
					m.nav.State = stateMain
					return *m, nil
				}
				m.pending.SelectAfter = filepath.Base(svc.WorktreePath(name))
				m.nav.State = stateMain
				return *m, createWorktreeCmd(svc, name, branch)
			}
		case "esc":
			m.nav.State = stateMain
			m.pending.CreateSvc = nil
		}
	}
	return *m, cmd
}

func handleOrphanBranch(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if m.deps.Svc == nil {
				m.nav.State = stateMain
				return *m, nil
			}
			if sel, ok := m.menu.SelectedItem().(listItem); ok {
				branch := sel.ItemTitle
				name := m.pending.Name
				m.pending.SelectAfter = filepath.Base(m.deps.Svc.WorktreePath(name))
				m.nav.State = stateMain
				return *m, adoptCmd(m.deps.Svc, name, branch)
			}
		case "esc":
			m.nav.State = stateMain
		}
	}
	return *m, cmd
}

func handleActionMenu(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			item, ok := m.menu.SelectedItem().(listItem)
			if !ok {
				return *m, nil
			}
			title := item.ItemTitle
			switch {
			case strings.Contains(title, "Jump"):
				if m.pending.Worktree != nil && m.deps.Svc != nil {
					sessionName := m.deps.Svc.SessionName(m.pending.Worktree.Worktree.Path)
					if !m.deps.Svc.Tmux.HasSession(sessionName) {
						_ = m.deps.Svc.Tmux.NewSession(sessionName, m.pending.Worktree.Worktree.Path)
					}
					if m.deps.RecentStore != nil && m.deps.Svc.Git != nil {
						m.deps.RecentStore.Add(m.deps.Svc.Git.RepoRoot, m.pending.Worktree.Worktree.Name, sessionName, m.pending.Worktree.Worktree.Path)
						_ = m.deps.RecentStore.Save()
					}
					m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: m.pending.Worktree.Worktree.Path}
					return *m, tea.Quit
				}
				if m.pending.Global != nil {
					sessionName := m.pending.Global.Worktree.Name
					if !m.deps.Tmux.HasSession(sessionName) {
						_ = m.deps.Tmux.NewSession(sessionName, m.pending.Global.Worktree.Path)
					}
					if m.deps.RecentStore != nil {
						m.deps.RecentStore.Add(m.pending.Global.RepoRoot, m.pending.Global.Worktree.Name, sessionName, m.pending.Global.Worktree.Path)
						_ = m.deps.RecentStore.Save()
					}
					m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: m.pending.Global.Worktree.Path}
					return *m, tea.Quit
				}
				if m.pending.Name != "" {
					m.jumpTarget = &JumpTarget{SessionName: m.pending.Name}
					return *m, tea.Quit
				}
			case strings.Contains(title, "Delete worktree"):
				if m.pending.Worktree != nil && m.deps.Svc != nil && m.deps.Svc.Git != nil {
					if m.pending.Worktree.Worktree.Path == m.deps.Svc.Git.RepoRoot {
						m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
						m.nav.State = stateMain
						return *m, toastExpireCmd()
					}
					return *m, deleteWorktreeCmd(m.deps.Svc, m.pending.Worktree.Worktree.Path)
				}
			case strings.Contains(title, "Kill session"):
				if m.pending.Worktree != nil && m.deps.Svc != nil {
					return *m, killSessionCmd(m.deps.Svc, m.pending.Worktree.SessionName)
				}
				if m.pending.Name != "" {
					if m.nav.GlobalMode {
						return *m, killSessionDirectCmd(m.deps.Tmux, m.pending.Name)
					}
					if m.deps.Svc != nil {
						return *m, killSessionCmd(m.deps.Svc, m.pending.Name)
					}
				}
			case strings.Contains(title, "Adopt"):
				if m.pending.Name != "" && m.deps.Svc != nil {
					m.nav.NextBranchState = stateOrphanBranch
					return *m, branchesCmd(m.deps.Svc)
				}
			}
			if m.nav.PrevState != 0 {
				m.nav.State = m.nav.PrevState
			} else {
				m.nav.State = stateMain
			}
			return *m, nil
		case "esc":
			if m.nav.PrevState != 0 {
				m.nav.State = m.nav.PrevState
			} else {
				m.nav.State = stateMain
			}
			return *m, nil
		}
	}
	return *m, cmd
}

func handleCommandPalette(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.commandPalette, cmd = m.commandPalette.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.nav.State = stateMain
			return *m, nil
		case "enter":
			if item, ok := m.commandPalette.SelectedItem().(CommandItem); ok {
				m.nav.State = stateMain
				if item.run != nil {
					return *m, item.run(m)
				}
			}
			m.nav.State = stateMain
			return *m, nil
		}
	}
	return *m, cmd
}
