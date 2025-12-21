#!/bin/bash

set -e

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "treemux installer"
echo "================="
echo ""

# Check dependencies
check_dep() {
  if ! command -v "$1" &>/dev/null; then
    echo "Error: $1 is required but not installed."
    exit 1
  fi
}

echo "Checking dependencies..."
check_dep zsh
check_dep tmux
check_dep fzf
check_dep git
echo "All dependencies found."
echo ""

# Create install directory if needed
mkdir -p "$INSTALL_DIR"

# Copy script
cp "$SCRIPT_DIR/treemux" "$INSTALL_DIR/treemux"
chmod +x "$INSTALL_DIR/treemux"

echo "Installed treemux to $INSTALL_DIR/treemux"
echo ""

# Check if in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  echo "Note: $INSTALL_DIR is not in your PATH."
  echo "Add this to your ~/.zshrc:"
  echo ""
  echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
  echo ""
fi

# Create default config
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/treemux"
if [[ ! -f "$CONFIG_DIR/config" ]]; then
  mkdir -p "$CONFIG_DIR"
  cat > "$CONFIG_DIR/config" << 'EOF'
# treemux configuration

# Default branch for new worktrees (default: auto-detect or "main")
# TREEMUX_BASE_BRANCH="main"

# Where to create worktrees: sibling (default) or subdirectory
# sibling:      ~/dev/myrepo-feature (next to original repo)
# subdirectory: ~/dev/myrepo/.worktrees/feature (inside repo)
# TREEMUX_PATH_PATTERN="sibling"

# Session naming: folder (default) or branch
# TREEMUX_SESSION_NAME="folder"
EOF
  echo "Created config at $CONFIG_DIR/config"
  echo ""
fi

echo "Done! Run 'treemux' in a git repo (inside tmux) to get started."
echo ""
echo "Tip: Add this alias to your ~/.zshrc:"
echo "  alias wts=\"treemux\""
