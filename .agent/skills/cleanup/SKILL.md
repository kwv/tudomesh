---
name: cleanup
description: Cleanup project state - worktrees, merged branches, and close inreview beads
---

# Cleanup Skill (land-the-plane)

Use this skill when you need to clean up the project state after finishing a series of tasks or at the end of a session.

## What it does

1.  **Prunes stale worktrees**: Runs `git worktree prune`.
2.  **Deletes worktrees for closed beads**: List all worktrees and removes those associated with beads that are already `closed`.
3.  **Deletes merged branches**: Deletes local branches that have been merged into `main`.
4.  **Prunes remote branches**: Runs `git remote prune origin`.
5.  **Closes inreview beads**: Finds all beads with status `inreview` and sets them to `closed`.
6.  **Syncs beads**: Runs `bd sync --full` to export and push to the beads-sync branch.

## Usage

Run the provided cleanup script:

```bash
./.claude/skills/cleanup/scripts/land-the-plane.sh
```

## When to use

- After a PR has been merged and you want to clean up your local environment.
- When you have many lingering worktrees from finished tasks.
- To bulk-close beads that are pending review after you've verified they are merged.

> [!IMPORTANT]
> Ensure you have pushed all your work before running this skill, as it may delete local branches and worktrees.
