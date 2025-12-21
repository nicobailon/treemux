# treemux

**Git worktrees + tmux sessions as one unit. Switch branches without losing your terminal state.**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=for-the-badge)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-macOS%20%7C%20Linux-blue?style=for-the-badge)]()

<p align="center">
  <img src="screen1.png" width="32%" />
  <img src="screen2.png" width="32%" />
  <img src="screen3.png" width="32%" />
</p>

```bash
treemux
```

## The Problem

Switching branches kills your flow:

```bash
git stash                    # hope you remember what you stashed
git checkout main            # lose your terminal state
npm install                  # dependencies changed
npm run dev                  # restart everything
# ... fix the bug ...
git checkout feature-branch
git stash pop                # pray it applies cleanly
npm install                  # restart again
# where was I?
```

Each switch costs 5-10 minutes reconstructing your environment.

## The Solution

**treemux** pairs git worktrees with tmux sessions. Each workspace is completely isolated:

| Before | After |
|--------|-------|
| One directory, constant stashing | Separate directories per branch |
| Kill dev server to switch | Dev servers keep running |
| Lose terminal history | History preserved per workspace |
| Manual session management | Automatic pairing |

```bash
# Create workspace "feature-auth" branched from main
treemux → type "feature-auth" → enter → select "main" → enter

# Switch to another workspace (instant, everything preserved)
treemux → select workspace → enter

# Delete workspace (worktree + session together)
treemux → select → ctrl-d → confirm
```

## Quick Start

### Requirements

- zsh, tmux, fzf, git

### Install

```bash
git clone https://github.com/nicobailon/treemux.git
cd treemux
./install.sh
```

### Use

```bash
treemux              # Open TUI
treemux clean        # Fix orphaned sessions/worktrees
treemux -l           # List worktrees
```

> **Tip:** Add `alias tx="treemux"` to your `.zshrc`

## Keybindings

| Key | Action |
|-----|--------|
| `enter` | Jump / Create / Manage orphan |
| `tab` | Actions menu |
| `ctrl-d` | Quick delete |
| `?` | Help |
| `esc` | Cancel |

## Features

### Unified View

- **Worktrees** with status: `●` current, green = session active, gray = no session
- **Orphaned sessions** at bottom (sessions without worktrees)
- **Preview panel** shows git status, running processes, recent commits

### Auto-Start

Run `treemux` outside tmux and it automatically creates a session and attaches.

### Orphan Management

Sessions without worktrees can be:
- **Adopted** - Create a worktree for the session
- **Jumped to** - Inspect before deciding
- **Killed** - Remove the session

## Configuration

Create `~/.config/treemux/config`:

```bash
# Default branch for new worktrees (default: auto-detect)
TREEMUX_BASE_BRANCH="main"

# Where to create worktrees: sibling (default) or subdirectory
TREEMUX_PATH_PATTERN="sibling"

# Session naming: folder (default) or branch
TREEMUX_SESSION_NAME="folder"
```

## How It Works

treemux treats worktree + session as **one workspace**:

```
┌─────────────────────────────────────────────┐
│ WORKSPACE: "feature-auth"                   │
├─────────────────────────────────────────────┤
│ ~/dev/myapp-feature-auth    (worktree)      │
│ feature-auth                (branch)        │
│ feature-auth                (tmux session)  │
│    └─ npm run dev (running)                 │
│    └─ vim src/auth.ts (open)                │
│    └─ terminal history preserved            │
└─────────────────────────────────────────────┘
```

Create together, switch together, delete together. No orphans, no manual bookkeeping.

## License

MIT
