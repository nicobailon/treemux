# treemux

Git worktree + tmux session manager. One workspace, one branch, one terminal.

<p align="center">
  <img src="screen1.png" width="32%" />
  <img src="screen2.png" width="32%" />
  <img src="screen3.png" width="32%" />
</p>

## Why Worktrees + Tmux Sessions Together?

### The Context Switching Problem

Developers constantly juggle multiple streams of work: a feature in progress, an urgent hotfix, a code review, an experiment. Traditional git workflows force painful context switches:

```
git stash                    # hope you remember what you stashed
git checkout main            # lose your terminal state
npm install                  # dependencies changed
npm run dev                  # restart everything
# ... fix the bug ...
git checkout feature-branch
git stash pop                # pray it applies cleanly
npm install                  # dependencies changed again
npm run dev                  # restart again
# where was I?
```

Each switch costs 5-10 minutes and mental energy reconstructing your environment.

### The Solution: Paired Isolation

**Git worktrees** solve file isolation - each worktree is a separate directory with its own branch checked out. But files are only half the story.

**Tmux sessions** solve environment isolation - terminal history, running processes, pane layouts, environment variables. Your dev server keeps running. Your test watcher stays active.

**Together**, they create complete workspace isolation:

| Layer | Worktree provides | Tmux session provides |
|-------|-------------------|----------------------|
| Files | Separate working directory | - |
| Branch | Independent git state | - |
| Processes | - | Persistent dev servers, watchers |
| Terminal | - | Command history, scroll buffer |
| Layout | - | Pane arrangements |
| State | - | Environment variables, cwd |

### Why Keep Them Paired?

The magic happens when worktree and session are treated as **one unit**:

1. **Create together** - New worktree automatically gets a dedicated session
2. **Switch together** - Jump to a worktree = switch to its session
3. **Delete together** - Remove worktree = kill its session (no orphans)
4. **Name together** - Session named after worktree folder (predictable)

This pairing eliminates an entire class of problems:
- No orphaned sessions cluttering your tmux
- No worktrees without a "home" to work in
- No confusion about which session belongs to which code
- No manual bookkeeping

### The Mental Model

Stop thinking about branches, worktrees, and sessions separately. Think in **workspaces**:

```
+-------------------------------------------------------------+
| WORKSPACE: "feature-auth"                                   |
+-------------------------------------------------------------+
| ~/dev/myapp-feature-auth    (worktree)                      |
| feature-auth                (branch)                        |
| feature-auth                (tmux session)                  |
|    - npm run dev (running)                                  |
|    - vim src/auth.ts (open)                                 |
|    - terminal history preserved                             |
+-------------------------------------------------------------+
```

With treemux, you manage workspaces, not their components.

---

## Installation

### Requirements

- zsh
- tmux
- fzf
- git

### Install

```bash
git clone https://github.com/nicobailon/treemux.git
cd treemux
./install.sh
```

Or manually:
```bash
cp treemux ~/.local/bin/
chmod +x ~/.local/bin/treemux
```

---

## Usage

```bash
treemux              # Open interactive TUI
treemux -l           # List worktrees
treemux clean        # Find and fix orphaned sessions/worktrees
treemux -h           # Help
```

### Auto-Start Tmux

If you run `treemux` outside of tmux, it automatically:
1. Creates a tmux session named after your current folder
2. Launches treemux inside that session
3. Attaches you to the session

No more "treemux requires tmux" errors - it just works.

### Keybindings

| Key | Action |
|-----|--------|
| `enter` | Jump to selected / Create new worktree / Manage orphan |
| `tab` | Actions menu (Jump, Delete, Back) |
| `ctrl-d` | Quick delete worktree + session |
| `?` | Help |
| `esc` | Cancel |

### The Interface

The main view shows:
- **Worktrees** with status indicators:
  - `●` purple dot = current worktree (you are here)
  - Green icon = tmux session is active
  - Gray icon = no active session
- **Orphaned Sessions** (if any) - sessions without matching worktrees

The **preview panel** shows:
- Path to the worktree
- Git status (clean, modified, staged, untracked)
- Ahead/behind upstream
- Session info (windows, panes)
- Running processes in the session
- Recent commits

### Creating a Workspace

1. Run `treemux` (or your alias, e.g., `tx`)
2. Type a name for your new workspace
3. Press `enter`
4. Select the base branch
5. Press `enter`

You're now in a new tmux session, in a new worktree, on a new branch.

### Switching Workspaces

1. Run `treemux`
2. Select a workspace (current one is at top, marked with `●`)
3. Press `enter`

Instant switch. Your other workspace keeps running in the background.

### Deleting a Workspace

1. Run `treemux`
2. Select the workspace to delete
3. Press `ctrl-d` or `tab` → Delete
4. See running processes that will be terminated
5. Confirm

Both the worktree and its tmux session are removed.

### Managing Orphaned Sessions

Orphaned sessions (tmux sessions without matching worktrees) appear at the bottom of the list. Select one and press `enter` to:

- **Adopt** - Create a worktree for this session, linking them together
- **Kill** - Terminate the session (shows running processes first)
- **Back** - Return to main view

This lets you either clean up stale sessions or "rescue" them by creating a matching worktree.

---

## Configuration

Create `~/.config/treemux/config`:

```bash
# Default branch for new worktrees (default: auto-detect or "main")
TREEMUX_BASE_BRANCH="main"

# Where to create worktrees:
#   sibling:      ~/dev/myrepo-feature (next to original repo)
#   subdirectory: ~/dev/myrepo/.worktrees/feature (inside repo)
TREEMUX_PATH_PATTERN="sibling"

# Session naming: folder (default) or branch
TREEMUX_SESSION_NAME="folder"
```

---

## Recommended Aliases

Add to your `.zshrc`:

```bash
alias tx="treemux"
alias wts="treemux"
```

---

## Common Workflows

### Feature Development
```
tx → type "feature-auth" → enter → select "main" → enter
# You're in a fresh workspace branched from main
```

### Urgent Hotfix
```
tx → type "hotfix-123" → enter → select "main" → enter
# Fix the bug, push, then:
tx → select your feature workspace → enter
# Back to where you were, everything intact
```

### Code Review
```
tx → type "review-pr-456" → enter → select "main" → enter
git fetch origin pull/456/head:pr-456
git checkout pr-456
# Review, then delete the workspace when done
tx → select "review-pr-456" → ctrl-d → confirm
```

### Adopting an Orphan
```
# You have a tmux session "experiment" but deleted its worktree
tx → select "experiment" (in orphaned section) → enter
# Choose "Adopt" → select base branch → enter
# Now you have a fresh worktree linked to your existing session
```

---

## Gotchas & Limitations

### Git Limitations
- **Same branch, multiple worktrees**: Git doesn't allow the same branch checked out in multiple worktrees. Treemux will show an error if you try.

### Disk Space
- Each worktree is a full file checkout
- Each needs its own `node_modules`, build artifacts, etc.
- Great for isolation, but be mindful on large repos

### Outside Treemux
- Worktrees created via `git worktree add` won't have sessions (treemux creates one on first jump)
- Sessions killed manually leave worktrees intact (shown as "no session" indicator)
- Worktrees deleted manually leave sessions orphaned (shown in orphaned section)

### IDE Integration
- Some IDEs handle worktrees well (JetBrains, VS Code)
- Some get confused - check your IDE's worktree support

---

## Built With

- **fzf** - Powers the interactive TUI
- **tmux** - Session management
- **git worktrees** - File isolation
- **zsh** - Shell scripting

---

## License

MIT
