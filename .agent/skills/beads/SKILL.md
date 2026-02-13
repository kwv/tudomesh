---
name: beads
description: Core issue tracking and workflow management with 'bd' (beads) CLI
---

# Beads Skill (bd)

Use this skill to manage your lifecycle in the project using the `bd` CLI. This tool handles issue tracking, git worktrees, and knowledge capture.

## Role: Orchestrator (Leading/Delegating)

As an Orchestrator, you use `bd` to investigate issues, define work, and delegate to supervisors.

### 1. Investigation & Tracking
- **Search Knowledge**: `.beads/memory/recall.sh "keyword"`
- **Create Bead**: `bd create "Title" -d "Description"` (returns `{BEAD_ID}`)
- **Log Investigation**: 
  ```bash
  bd comments add {BEAD_ID} "INVESTIGATION: 
  Root cause: {file}:{line}
  Fix: {steps}"
  ```

### 2. Delegation
- **Assign**: `bd update {BEAD_ID} --assignee {tech}-supervisor`
- **Dispatch**: Use the `Task()` tool with the role and `BEAD_ID`.

### 3. Management
- **List Work**: `bd ready` (unblocked tasks)
- **Check Status**: `bd show {BEAD_ID}`
- **Close Epic**: `bd close {EPIC_ID}` (only after all children are merged)

---

## Role: Supervisor (Implementing)

As a Supervisor, you use `bd` to claim work, manage your environment, and record learnings.

### 1. Setup & Claim
- **Create Worktree**: `bd worktree create .worktrees/bd-{BEAD_ID} --branch bd-{BEAD_ID}`
- **Claim**: `bd update {BEAD_ID} --status in_progress --assignee "@me"`

### 2. Implementation
- **Log Progress**: `bd comments add {BEAD_ID} "Completed X, working on Y"`
- **Record Learning (Required)**: 
  ```bash
  bd comments add {BEAD_ID} "LEARNED: [technical insight/pattern]"
  ```

### 3. Completion
- **Mark for Review**: `bd update {BEAD_ID} --status inreview`
- **Sync**: `bd sync --full` (pushes bead metadata)

---

## Command Reference

| Command | Description |
| :--- | :--- |
| `bd onboard` | Setup local beads environment |
| `bd list` | List all beads |
| `bd ready` | Show beads with no blockers |
| `bd show ID` | Detailed view of a bead |
| `bd update ID --status S` | Update status (open, in_progress, inreview, done) |
| `bd comments ID` | View all comments/investigations |
| `bd comments add ID "..."` | Add a comment |
| `bd dep relate ID1 ID2` | Relate two beads for traceability |
| `bd sync --full` | Export to JSONL and push to `beads-sync` |
| `bd worktree create PATH` | Create a git worktree for a bead |

---

## Knowledge Capture

The `bd` system captures `INVESTIGATION:` and `LEARNED:` comments into a persistent knowledge base.

- **`INVESTIGATION:`**: Used by Orchestrators to document root causes and fix plans.
- **`LEARNED:`**: Used by Supervisors to document technical insights, conventions, or gotchas discovered during implementation.

**MANDATORY**: Every task completion must include a `LEARNED:` comment.
