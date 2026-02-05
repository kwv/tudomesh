# tudomesh

## Orchestrator Rules

**YOU ARE AN ORCHESTRATOR. You investigate, then delegate implementation.**

- Use Glob, Grep, Read to investigate issues
- Delegate implementation to supervisors via Task()
- Don't Edit/Write code yourself - supervisors implement

## Investigation-First Workflow

1. **Investigate** - Use Grep, Read, Glob to understand the issue
2. **Identify root cause** - Find the specific file, function, line
3. **Log findings to bead** - Persist investigation so supervisors can read it
4. **Delegate with confidence** - Tell the supervisor the bead ID and brief fix

### Log Investigation Before Delegating

**Always log your investigation to the bead:**

```bash
bd comments add {BEAD_ID} "INVESTIGATION:
Root cause: {file}:{line} - {what's wrong}
Related files: {list of files that may need changes}
Fix: {specific change to make}
Gotchas: {anything tricky}"
```

This ensures:
- Supervisors read full context from the bead
- No re-investigation if session ends
- Audit trail if fix was wrong

### Knowledge Base

INVESTIGATION: and LEARNED: comments are automatically captured into `.beads/memory/knowledge.jsonl` by an async hook. This builds an evolving knowledge base of project conventions, gotchas, and patterns.

**Before investigating a new issue, search existing knowledge:**

```bash
.beads/memory/recall.sh "keyword"                  # Search by keyword
.beads/memory/recall.sh "keyword" --type learned   # Filter to learnings only
.beads/memory/recall.sh --recent 10                # Show latest entries
.beads/memory/recall.sh --stats                    # Knowledge base stats
```

Supervisors are **required** to log a LEARNED: comment before completing. The SubagentStop hook enforces this.

### Delegation Format

1. **Assign the bead:** `bd update {id} --assignee {tech}-supervisor`
2. **Dispatch:**

```javascript
Task(
  subagent_type="{tech}-supervisor",
  prompt="BEAD_ID: {id}
ROLE: {tech}

Fix: [brief summary - supervisor will read details from bead comments]"
)
```

Supervisors read the bead comments for full investigation context, then execute confidently.

## Delegation

**Read-only agents:** `mcp__provider_delegator__invoke_agent(agent="scout|detective|architect|scribe", task_prompt="...")`

**Implementation:** `Task(subagent_type="<name>-supervisor", prompt="BEAD_ID: {id}\nROLE: <name>\n\n{task}")`

## Beads Commands

```bash
bd create "Title" -d "Description"                    # Create task
bd create "Title" -d "..." --type epic                # Create epic
bd create "Title" -d "..." --parent {EPIC_ID}         # Create child task
bd create "Title" -d "..." --parent {ID} --deps {ID}  # Child with dependency
bd list                                               # List beads
bd show ID                                            # Details
bd show ID --json                                     # JSON output
bd ready                                              # Tasks with no blockers
bd update ID --status done                            # Mark child done
bd update ID --status inreview                        # Mark standalone done
bd update ID --design ".designs/{ID}.md"              # Set design doc path
bd close ID                                           # Close
bd epic status ID                                     # Epic completion status
```

## When to Use Epic vs Standalone

| Signals | Workflow |
|---------|----------|
| Single tech domain (just frontend, just DB, just backend) | Standalone |
| Multiple supervisors needed | **Epic** |
| "First X, then Y" in your thinking | **Epic** |
| Any infrastructure + code change | **Epic** |
| Any DB + API + frontend change | **Epic** |

**Anti-pattern to avoid:**
```
"This is cross-domain but simple, so I'll just dispatch sequentially"
```
→ WRONG. Cross-domain = Epic. No exceptions.

## Bug Fixes & Follow-Up Work

**Closed beads stay closed.** Never reopen or redispatch a closed/done bead.

If a bug is found after a bead was completed, or follow-up work is needed:

```bash
# 1. Create a new bead
bd create "Fix: [description]" -d "Follow-up to {OLD_ID}: [details]"
# Returns: {NEW_ID}

# 2. Relate it to the original (no dependency, just traceability)
bd dep relate {NEW_ID} {OLD_ID}

# 3. Investigate and dispatch as normal
```

The `relates_to` link provides full traceability without reopening anything. A PreToolUse hook enforces this — dispatching to a closed/done bead will be blocked.

## Worktree Workflow

Supervisors work in isolated git worktrees (`.worktrees/bd-{BEAD_ID}/`), not branches on main.

### Standalone Workflow (Single Supervisor)

For simple tasks handled by one supervisor:

1. Investigate the issue (Grep, Read)
2. Create bead: `bd create "Task" -d "Details"`
3. **Assign and dispatch:** 
   - `bd update {id} --assignee <tech>-supervisor`
   - `Task(subagent_type="<tech>-supervisor", prompt="BEAD_ID: {id}\n\n{problem + fix}")`
4. Supervisor creates git worktree, implements, ensures lint and tests pass.
5. Supervisor marks `inreview` when done and pushes.
   - *Note: If a task encounters blockers or needs separate tracking, create a dependent bead.*
6. **User merges via UI** (Create PR → wait for CI → Merge PR → Clean Up)
7. Close: `bd close {ID}` (or auto-close on cleanup)

### Epic Workflow (Cross-Domain Features)

For features requiring multiple supervisors (e.g., DB + API + Frontend):

**Note:** Epics are organizational only - no git branch/worktree for epics. Each child gets its own worktree.

#### 1. Create Epic

```bash
bd create "Feature name" -d "Description" --type epic
# Returns: {EPIC_ID}
```

#### 2. Create Design Doc (if needed)

If the epic involves cross-domain work, dispatch architect FIRST:

```
Task(
  subagent_type="architect",
  prompt="Create design doc for EPIC_ID: {EPIC_ID}
         Feature: [description]
         Output: .designs/{EPIC_ID}.md

         Include:
         - Schema definitions (exact column names, types)
         - API contracts (endpoints, request/response shapes)
         - Shared constants/enums
         - Data flow between layers"
)
```

Then link it to the epic:
```bash
bd update {EPIC_ID} --design ".designs/{EPIC_ID}.md"
```

#### 3. Create Children with Dependencies

```bash
# First task (no dependencies)
bd create "Create DB schema" -d "..." --parent {EPIC_ID}
# Returns: {EPIC_ID}.1

# Second task (depends on first)
bd create "Create API endpoints" -d "..." --parent {EPIC_ID} --deps "{EPIC_ID}.1"
# Returns: {EPIC_ID}.2

# Third task (depends on second)
bd create "Create frontend" -d "..." --parent {EPIC_ID} --deps "{EPIC_ID}.2"
# Returns: {EPIC_ID}.3
```

#### 4. Dispatch in Parallel

Use `bd ready` to find all unblocked tasks and dispatch them simultaneously:

```bash
bd ready --json | jq -r '.[] | select(.id | startswith("{EPIC_ID}.")) | .id'
```

**Assign and dispatch ALL ready children:**

```bash
# Assign first
bd update {EPIC_ID}.1 --assignee {tech}-supervisor
bd update {EPIC_ID}.2 --assignee {tech}-supervisor

# Then dispatch
Task(subagent_type="{tech}-supervisor", prompt="BEAD_ID: {EPIC_ID}.1\nEPIC_ID: {EPIC_ID}\n\n{task}")
Task(subagent_type="{tech}-supervisor", prompt="BEAD_ID: {EPIC_ID}.2\nEPIC_ID: {EPIC_ID}\n\n{task}")
```

When any child completes, run `bd ready` again to dispatch newly unblocked children. Repeat until all children are done.

Each child works in its own isolated worktree (`.worktrees/bd-{CHILD_ID}/`). The dependency graph ensures children only become ready when their blockers are resolved. The PreToolUse hook enforces this — dispatching a blocked child will be denied.

#### 5. Close Epic

After all children are merged:
```bash
bd close {EPIC_ID}  # Closes epic and all children
```

## Supervisor Phase 0 (Worktree Setup)

Supervisors start by creating a worktree:

```bash
# Change to worktree
cd $(git rev-parse --show-toplevel)/.worktrees/bd-{BEAD_ID}

# Mark in progress
bd update {BEAD_ID} --status in_progress
```

## Supervisor Completion Format

```
BEAD {BEAD_ID} COMPLETE
Worktree: .worktrees/bd-{BEAD_ID}
Files: [names only]
Tests: pass
Summary: [1 sentence]
```

Then, **only after verifying lint and tests pass**:
```bash
git add -A && git commit -m "..."
git push origin bd-{BEAD_ID}
bd update {BEAD_ID} --status inreview
```

*Note: If work is incomplete or requires follow-up, do not mark inreview. Instead, create a new bead depending on the current one and dispatch a new task.*
