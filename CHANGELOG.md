# Changelog

## 0.2.0 — 2025-12-24

### Features

**Grid View** (`ctrl+g`)
- Visual grid of all active sessions with live terminal previews
- Section headers separating SESSIONS and ORPHANED SESSIONS
- Available Worktrees section showing worktrees without sessions
- Quick jump with number keys (1-9)
- Vim-style filtering: press `/` to search, `esc` to clear
- Info sidebar showing status, branch, path, session details
- Press `enter` on available worktree to create session and jump

**UI Enhancements**
- Two-line list items with accent bar selection indicator
- Status badges (`3M 2S`) instead of dots
- `LIVE` badge for active sessions
- Preview cards with colored title bars and bordered content
- Gradient rainbow header divider
- Gradient panel separator between list and preview
- macOS-style terminal preview with traffic light dots
- Contextual accent colors per item type
- Enhanced help modal with gradient title and styled key badges

### Keybindings
- `ctrl+g` - Toggle grid view
- `1-9` - Quick jump to panel (in grid view)
- `/` - Start filtering (in grid view)

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
