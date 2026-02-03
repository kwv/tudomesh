#!/bin/bash
# Install git hooks for tudomesh development

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git rev-parse --show-toplevel)"
GIT_HOOKS_DIR="$REPO_ROOT/.git/hooks"

echo "Installing git hooks..."

# Install pre-commit hook
if [ -f "$GIT_HOOKS_DIR/pre-commit" ]; then
    echo "Warning: pre-commit hook already exists. Backing up to pre-commit.bak"
    cp "$GIT_HOOKS_DIR/pre-commit" "$GIT_HOOKS_DIR/pre-commit.bak"
fi

cp "$SCRIPT_DIR/pre-commit" "$GIT_HOOKS_DIR/pre-commit"
chmod +x "$GIT_HOOKS_DIR/pre-commit"

echo "Successfully installed pre-commit hook."
echo ""
echo "The hook will run 'make lint' before each commit."
echo "To bypass the hook (not recommended), use: git commit --no-verify"
