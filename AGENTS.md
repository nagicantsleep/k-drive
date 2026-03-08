# AI Agent Instructions

## Project Context

- **Overview:** Cross-platform desktop app to connect multiple remote drives (S3-compatible first, then Google Drive/OneDrive), mount each as a local virtual drive, and manage multiple accounts/sessions securely.
- **Tech Stack:** Desktop shell: Wails (Go backend + React/TypeScript frontend); Storage engine: rclone; Windows mount: WinFsp; macOS mount: macFUSE; Local DB: SQLite; Secrets: Windows DPAPI / macOS Keychain; Packaging: Wails build pipeline.
- **Structure:**
  - `apps/desktop/` (Wails app)
    - `frontend/` (React UI: account management, drive list, mount status)
    - `backend/` (Go services: connectors, mount manager, process supervisor)
    - `backend/connectors/` (S3, Google Drive, OneDrive adapters)
    - `backend/mount/` (rclone mount orchestration, health checks, auto-remount)
    - `backend/auth/` (OAuth flows + credential handling)
    - `backend/storage/` (SQLite repositories + secure secret integration)
- **Core Flow:** Add account (OAuth or credentials) → persist account metadata + secure secrets → create remote config → start dedicated `rclone mount` process → expose as one local virtual drive per remote → monitor and auto-recover mount process.
- **Conventions:** TypeScript strict mode in frontend; Go with small service interfaces in backend; one mount process per account; no plaintext secrets (OS vault only); connector-specific config normalized via typed DTOs.
- **Commands:**
  - Install: `go mod tidy` (backend), `npm install` (frontend)
  - Run dev: `wails dev`
  - Build: `wails build`
  - Lint: `golangci-lint run` (Go), `npm run lint` (frontend)
  - Test: `go test ./...` and `npm test`

---

## 1. Git & Issue Conventions (MANDATORY)

**ALL code changes MUST be driven by a GitHub issue.** Informational requests need no issue.

- **Branching:** `feature/<issue-#>-<desc>`, `fix/<issue-#>-<desc>`, `epic/<name>`.
- **Commits:** `type(#issue): short desc` (Types: feat, fix, refactor, docs, chore).
- **Labels:** Use available labels; `epic:<name>` for epics.
- **NEVER:** Commit `.env`, `node_modules/`, `dist/`, local DBs.
- **NEVER:** Use `git add -A` or `git add .` (always stage specific files explicitly).

**Rule**: If the request leads to **any code file being modified**, an issue MUST exist before execution. No exceptions.

## 2. Issue Standards

Create issues via `gh issue create` before coding.
**Required Structure:**

```md
### Description & Requirements

[What, why, technical details, files to modify]

### Dependencies

Requires: #XX | Part of: epic:name

### Acceptance Criteria

- [ ] Specific, verifiable deliverable 1 (No vague "works correctly")
- [ ] Specific, verifiable deliverable 2 (No vague "works correctly")
- [ ] Equivalent build/test command
```

## 3. Workflow Phases

### Phase 1: Research

- `gh issue view <issue-#>`
- Read dependencies and related epics (`gh issue list --label "epic:<name>"`). Ask user if unclear.

### Phase 2: Implementation & Branching

**Epic Branching Strategy:**

1. **Epic Branch:** `main` → `epic/<name>` (Main integration point).
2. **Sub-issue Branch:** `epic/<name>` → `feature/<issue-#>-<desc>` (NEVER branch from or merge to `main`).
3. **Standalone Issue:** Branch from `main` → `feature/<issue-#>-<desc>`.

Implement changes, follow conventions, and fix all `npm run lint` errors.

### Phase 2.5: Testing (MANDATORY)

Before completing an issue, you MUST test and post a report. Do not complete if tests fail.

- **Backend:** Call API (curl/UI); check status/shape/errors.
- **Frontend:** Interact with UI; check states (loading/error/success).
- **Integration:** Run end-to-end flow.
- **Infra/Config:** Run scripts; verify config application.

**Post this Test Report on the issue (`gh issue comment <issue-#> --body "..."`):**

```md
### Test Report

- **Scope & Cases:** [...]
- **Results:** `npm run lint` (✅/❌), [Test Cmd/Flow] (✅/❌)
- **Smoke Test:**[ ] App starts/UI accessible, [ ] Core flow works, [ ] No critical errors
```

### Phase 3: Completion & Merging

**A. Epic Auto-Proceed (Sub-issues):** Do NOT ask for user confirmation between issues. Do NOT close the issue.

1. Post Implementation Summary (files changed, what/why) & Test Report.
2. Update checklist: `gh issue edit <id> --body "<updated body with [x]>"`
3. Stage explicitly, commit (`feat(#id): desc`), and push sub-issue branch.
4. Merge sub-issue into `epic/<name>`.
5. Proceed immediately to next issue in epic.

**B. Standalone Issues & Finalizing Epics:** STOP and WAIT for user confirmation.

1. Complete steps 1-4 above (merging standalone to `main`, or waiting to merge Epic to `main`).
2. Only after explicit user approval: merge epic to `main` (if applicable) and execute `gh issue close <id>`.

## 4. GH CLI Command Reference

Use `gh` CLI exclusively. No web UI or raw API calls.

- `gh issue view <id>` (append `--json body -q '.body'` to read/update checklists)
- `gh issue list --label "..."`
- `gh issue create --title "..." --label "..." --body "..."`
- `gh issue edit <id> --body "..."` (or `--add-label "..."`)
- `gh issue comment <id> --body "..."`
- `gh issue close <id>`
- `gh pr create --title "..." --body "..." --base main`

### 5. Multi-Issue & Epic Workflow

**Epic Branching Strategy (MANDATORY)**
Epics use a dedicated integration branch. **NEVER** merge sub-issues directly to `main`.

- **Hierarchy:** `main` → `epic/<name>` → `feature/<issue-#>-<desc>` (or `fix/...`)

**Step-by-Step Execution:**

**1. Setup & Planning**

- List issues: `gh issue list --label "epic:<name>"`
- Work strictly in dependency order. Reference them (`Depends on #XX`).
- Check if epic branch exists: `git branch -a | grep "epic/"`

**2. Start Epic (If branch doesn't exist)**

```bash
git checkout main && git pull origin main
git checkout -b epic/<name> && git push origin epic/<name>
```

**3. Start Sub-Issue (Always branch from Epic)**

```bash
git checkout epic/<name> && git pull origin epic/<name>
git checkout -b feature/<issue-#>-<desc>
```

**4. Complete Sub-Issue (Auto-proceed)**
_Rule: Do NOT ask for confirmation. Do NOT close the issue._

```bash
# Update issue
gh issue comment <id> --body "## Implementation Summary..."
gh issue edit <id> --body "<updated body with [x] checks>"

# Commit & Push Sub-issue
git add <files>
git commit -m "feat(#<id>): <desc>"
git push origin feature/<issue-#>-<desc>

# Merge to Epic Branch
git checkout epic/<name> && git pull origin epic/<name>
git merge feature/<issue-#>-<desc> && git push origin epic/<name>
```

**5. Complete Epic (User Approval Required)**
_Rule: STOP and WAIT for explicit user approval before executing._

```bash
git checkout main && git pull origin main
git merge epic/<name> && git push origin main
gh issue close <epic-issue-#>
```
