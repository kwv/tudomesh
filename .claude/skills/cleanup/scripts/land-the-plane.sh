#!/bin/bash

# land-the-plane: Cleanup project state
# 1. Prune stale worktrees
# 2. Delete local branches merged to main
# 3. Prune remote branches
# 4. Close beads in 'inreview' status

set -e

echo "🚀 Starting land-the-plane cleanup..."

# Ensure we are in the root of the main worktree
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

# 1. Prune git worktrees
echo "🧹 Pruning worktrees..."
git worktree prune

# 2. List worktrees and check for completion
git worktree list --porcelain | grep "^worktree" | while read -r line; do
    path=$(echo "$line" | cut -d' ' -f2)
    if [[ "$path" == "$repo_root" ]]; then continue; fi
    if [[ "$path" == *"beads-sync"* ]]; then continue; fi
    
    # Try to extract bead ID from path
    bead_id=$(basename "$path" | sed 's/bd-tudomesh-//')
    
    # Check if bead is closed
    if bd show "$bead_id" --json >/dev/null 2>&1; then
        status=$(bd show "$bead_id" --json | jq -r '.[0].status')
        if [[ "$status" == "closed" ]]; then
            echo "🗑️ Removing worktree for closed bead $bead_id: $path"
            git worktree remove "$path" --force
        fi
    else
        echo "❓ Worktree $path has no associated bead or bead-id extraction failed. Skipping."
    fi
done

# 3. Delete merged local branches
echo "🌿 Cleaning up merged local branches..."
# Use origin/main as reference for merged branches
git branch --merged origin/main | grep -v "\*" | grep -v "main" | grep -v "beads-sync" | xargs -r git branch -d || true

# 4. Prune remote branches (preserve beads-sync)
echo "🌐 Pruning remote branches..."
git fetch origin --prune && git remote set-head origin --auto || true
# Re-push beads-sync if it was pruned from remote
if ! git ls-remote --heads origin beads-sync | grep -q beads-sync; then
    echo "⚠️  Restoring pruned remote beads-sync..."
    git push origin beads-sync || true
fi

# 5. Close inreview beads
echo "✅ Closing 'inreview' beads..."
inreview_beads=$(bd list --status inreview --json | jq -r '.[].id')

if [[ -z "$inreview_beads" ]]; then
    echo "No beads in 'inreview' status."
else
    for id in $inreview_beads; do
        echo "Closing bead $id..."
        bd update "$id" --status closed
    done
fi

# 6. Final Sync
echo "🔄 Final bd sync..."
bd sync

echo "✨ Cleanup complete!"
