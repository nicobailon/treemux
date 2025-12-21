# treemux Go rewrite progress log

## 2025-12-21
- Initialized progress tracking file.
- Assessed current repo contents: zsh `treemux` script version 0.1.0, README, no Go sources present.
- Reviewed architecture spec v2 to align implementation scope and parity requirements.
- Created Go module `github.com/nicobailon/treemux` and scaffolding (cmd/internal/pkg layout, Makefile, config example).
- Implemented initial config loader (viper + legacy shell parser), git/tmux/workspace/deps services, placeholder TUI entrypoint.
- Added cobra CLI with bootstrap-to-tmux logic, list/clean commands, version flag parity; ensured `go build ./cmd/treemux` passes.
- Expanded workspace service parity helpers (create/delete/jump/adopt, session naming, ahead/behind) and clean command `--kill-orphans` support; enforced dependency preflight.
- Replaced TUI placeholder with bubbletea implementation (list + preview, create flow name→branch, action/orphan menus, ctrl-d delete, help overlay); wired session-naming awareness and branch/default ordering; go mod tidy/build passing.
- Added config loader unit test (YAML path via XDG_CONFIG_HOME) and documented Go rewrite build/run usage in README.
- Implemented orphan adopt branch selection flow in TUI; added workspace path/session naming unit tests (sibling/subdirectory, folder/branch); tests now include git-backed branch naming; all tests passing.
- Hardened tmux bootstrap: clearer errors and fallback to in-shell TUI if tmux attach fails (prevents bare usage dump when outside tmux).
- Revamped TUI styling to Catppuccin-inspired panels, custom list delegate, framed preview, header/footer key hints for a more polished look.
