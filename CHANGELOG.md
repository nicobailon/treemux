# Changelog

## 0.1.0 — 2025-12-20

Initial release of treemux.

### Features
- Interactive TUI powered by fzf with Catppuccin color scheme
- Create worktree + tmux session together in one action
- Jump between workspaces instantly (session already running)
- Delete worktree + session together (no orphans)
- Preview panel showing git status, running processes, recent commits
- Orphan management: adopt, jump to, or kill sessions without worktrees
- Auto-start tmux when run outside tmux
- `treemux clean` command to find and fix orphaned sessions/worktrees
- Contextual action hints in preview panel
- Centered tmux popup dialogs for confirmations

### Keybindings
- `enter` - Jump to selected / Create new worktree / Manage orphan
- `tab` - Actions menu (Jump, Delete, Kill session only)
- `ctrl-d` - Quick delete worktree + session
- `?` - Help
- `esc` - Cancel

### Configuration
- `TREEMUX_BASE_BRANCH` - Default branch for new worktrees
- `TREEMUX_PATH_PATTERN` - Where to create worktrees (sibling or subdirectory)
- `TREEMUX_SESSION_NAME` - Session naming strategy (folder or branch)
